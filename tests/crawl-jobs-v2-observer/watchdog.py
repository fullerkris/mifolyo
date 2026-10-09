"""Independent owned-cleanup watchdog, bounded private IPC and retained journal.

No actor execution CLI. The parent must separately authorize resource creation
and acknowledge every journalled candidate/identity before progressing.
"""
import copy
import multiprocessing
import os
from pathlib import Path
import re
import selectors
import signal
import stat
import time

from contracts import LIMITS, Invalid, canonical, decode, digest, exact, integer, require, sha
from owned_resources import ACTORS, VOLUMES, cleanup, name, validate_scope, validate_snapshot
from streams import Deadline

CONTROL_FRAME = 32768
CONTROL_BYTES = 65536
JOURNAL_BYTES = 131072


def file_identity(info):
    return sha(canonical({"device": info.st_dev, "inode": info.st_ino}))


def read_journal(path, expected_identity):
    digest(expected_identity)
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        info = os.fstat(fd)
        require(stat.S_ISREG(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1 and
            info.st_uid == os.getuid() and 0 < info.st_size <= JOURNAL_BYTES and file_identity(info) == expected_identity, "JOURNAL_IDENTITY")
        raw = os.pread(fd, JOURNAL_BYTES + 1, 0)
        require(len(raw) == info.st_size and raw.endswith(b"\n"), "JOURNAL_BOUND")
        prefix, rows = b"", []
        for line in raw.splitlines(keepends=True):
            row = decode(line, CONTROL_FRAME + 1024)
            exact(row, ("sequence", "previous", "kind", "payload"))
            integer(row["sequence"], len(rows), len(rows))
            require(row["previous"] == (sha(prefix) if prefix else None) and row["kind"] in ("ready", "armed", "triggered", "cleanup"), "JOURNAL_CHAIN")
            prefix += line
            rows.append(row)
        require(len(rows) <= 16 and rows[0]["kind"] == "ready", "JOURNAL_EVENTS")
        after = os.fstat(fd)
        named = Path(path).stat(follow_symlinks=False)
        require(file_identity(after) == file_identity(named) == expected_identity and after.st_size == named.st_size == len(raw) and
            named.st_nlink == after.st_nlink == 1, "JOURNAL_CHANGED")
        return {"sha256": sha(raw), "identity_sha256": expected_identity, "rows": rows}
    finally:
        os.close(fd)


class ControlChannel:
    def __init__(self, connection):
        self.connection = connection
        self.fd = connection.fileno()
        os.set_blocking(self.fd, False)
        self.partial = bytearray()
        self.traffic, self.received, self.sent = 0, 0, 0

    def _wait(self, mode, deadline):
        with selectors.DefaultSelector() as selector:
            selector.register(self.fd, mode)
            selector.select(min(deadline.remaining(), 0.05))

    def send(self, value, deadline):
        raw = canonical(value)
        require(len(raw) <= CONTROL_FRAME and self.traffic + len(raw) <= CONTROL_BYTES and self.sent < 16, "CONTROL_BOUND")
        offset = 0
        while offset < len(raw):
            deadline.check()
            try:
                count = os.write(self.fd, raw[offset:])
                require(count > 0, "CONTROL_CLOSED")
                offset += count
            except BlockingIOError:
                self._wait(selectors.EVENT_WRITE, deadline)
        self.traffic += len(raw)
        self.sent += 1

    def receive(self, deadline):
        require(self.received < 16, "CONTROL_BOUND")
        while True:
            deadline.check()
            end = self.partial.find(b"\n")
            if end >= 0:
                require(end + 1 <= CONTROL_FRAME, "CONTROL_FRAME")
                raw = bytes(self.partial[:end + 1])
                del self.partial[:end + 1]
                self.received += 1
                return decode(raw, CONTROL_FRAME)
            require(len(self.partial) <= CONTROL_FRAME, "CONTROL_FRAME")
            try:
                raw = os.read(self.fd, 4096)
                if not raw:
                    require(not self.partial, "CONTROL_TRUNCATED")
                    raise EOFError()
                require(self.traffic + len(raw) <= CONTROL_BYTES, "CONTROL_BOUND")
                self.traffic += len(raw)
                self.partial.extend(raw)
            except BlockingIOError:
                self._wait(selectors.EVENT_READ, deadline)

    def close(self):
        self.connection.close()


class Journal:
    def __init__(self, directory, invocation_id):
        require(type(invocation_id) is str and re.fullmatch(r"[0-9a-f]{32}", invocation_id) is not None, "JOURNAL_NAME")
        directory = Path(directory)
        require(directory.is_absolute() and not directory.is_symlink(), "JOURNAL_DIRECTORY")
        info = directory.stat()
        require(stat.S_ISDIR(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o700 and info.st_uid == os.getuid(), "JOURNAL_DIRECTORY")
        self.path = directory / (invocation_id + ".watchdog.jsonl")
        self.fd = os.open(self.path, os.O_RDWR | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_NONBLOCK, 0o600)
        info = os.fstat(self.fd)
        self.identity, self.raw, self.count = (info.st_dev, info.st_ino), b"", 0
        self.identity_sha256 = file_identity(info)
        directory_fd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            os.fsync(directory_fd)
        finally:
            os.close(directory_fd)

    def append(self, kind, payload):
        require(kind in ("ready", "armed", "triggered", "cleanup") and self.count < 16, "JOURNAL_EVENT")
        info = os.fstat(self.fd)
        path_info = self.path.stat(follow_symlinks=False)
        require(stat.S_ISREG(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1 and
            (info.st_dev, info.st_ino) == (path_info.st_dev, path_info.st_ino) == self.identity and info.st_size == len(self.raw), "JOURNAL_CHANGED")
        require(os.pread(self.fd, len(self.raw) + 1, 0) == self.raw, "JOURNAL_CHANGED")
        line = canonical({"sequence": self.count, "previous": sha(self.raw) if self.raw else None, "kind": kind, "payload": payload})
        require(len(self.raw) + len(line) <= JOURNAL_BYTES, "JOURNAL_BOUND")
        offset = 0
        while offset < len(line):
            written = os.pwrite(self.fd, line[offset:], len(self.raw) + offset)
            require(written > 0, "JOURNAL_WRITE")
            offset += written
        os.fsync(self.fd)
        self.raw += line
        self.count += 1
        require(os.pread(self.fd, len(self.raw) + 1, 0) == self.raw, "JOURNAL_APPEND")
        after = self.path.stat(follow_symlinks=False)
        require((after.st_dev, after.st_ino) == self.identity and after.st_nlink == 1, "JOURNAL_REPLACED")

    def close(self):
        os.close(self.fd)


class WatchState:
    def __init__(self, value):
        self.scope = validate_scope(value)
        self.current = {"version": 1, "scope": copy.deepcopy(self.scope), "generation": 0, "entries": {}}
        self.started = False

    def accept(self, value):
        new = validate_snapshot(value)
        require(canonical(new["scope"]) == canonical(self.scope), "WATCH_SCOPE")
        if not self.started:
            require(new["generation"] == 0 and new["entries"] == {}, "WATCH_INITIAL")
        else:
            require(new["generation"] == self.current["generation"] + 1, "WATCH_GENERATION")
            old_entries, new_entries = self.current["entries"], new["entries"]
            require(set(old_entries) <= set(new_entries), "WATCH_RESOURCE_LOSS")
            added = set(new_entries) - set(old_entries)
            changes = []
            for role, old in old_entries.items():
                if canonical(old) != canonical(new_entries[role]):
                    require(old["reference"] is None and new_entries[role]["reference"] is not None, "WATCH_REBIND")
                    changes.append(role)
            require((len(added) == 1 and not changes and new_entries[next(iter(added))]["reference"] is None)
                or (not added and len(changes) == 1), "WATCH_TRANSITION")
        self.current, self.started = new, True
        return sha(canonical(new))


def validate_cleanup(value, snapshot):
    exact(value, ("scope_sha256", "resources", "evidence_kind", "complete", "execution_authorized", "runtime_process_admission"))
    require(value["scope_sha256"] == sha(canonical(snapshot["scope"])) and value["execution_authorized"] is False and
        value["runtime_process_admission"] == "not_established", "CLEANUP_SCOPE")
    require(value["evidence_kind"] in ("simulated", "docker_metadata_only"), "CLEANUP_EVIDENCE_KIND")
    expected_roles = [role for role in ("observer", "oracle", "target", "witness", "control") if role in snapshot["entries"]]
    require(type(value["resources"]) is list and len(value["resources"]) == len(expected_roles), "CLEANUP_INVENTORY")
    for role, row in zip(expected_roles, value["resources"]):
        exact(row, ("role", "kind", "name", "absent", "creation_settled", "result"))
        require(row["role"] == role and row["kind"] == ("volume" if role in VOLUMES else "container") and
            row["name"] == name(snapshot["scope"], role) and type(row["absent"]) is bool, "CLEANUP_BINDING")
        require(row["result"] in ("already_absent", "removed_and_absent", "cleanup_not_proven") and
            row["absent"] == (row["result"] != "cleanup_not_proven"), "CLEANUP_RESULT")
        require(type(row["creation_settled"]) is bool and row["creation_settled"] == (snapshot["entries"][role]["reference"] is not None), "CREATION_SETTLEMENT")
    require(type(value["complete"]) is bool and value["complete"] == all(row["absent"] and row["creation_settled"] for row in value["resources"]), "CLEANUP_COMPLETE")
    return value


def _default_backend(value, profile_files, cleanup_authorizer=None):
    from docker_backend import DockerBackend
    expected = sha(canonical(value))
    def delegated(action, scope_hash):
        return (action == "cleanup" and scope_hash == expected and callable(cleanup_authorizer)
            and cleanup_authorizer(action, scope_hash) is True)
    return DockerBackend(value, profile_files, authorize=delegated if callable(cleanup_authorizer) else None)


def _worker(connection, value, profile_files, directory, end, parent_uid, backend_factory, cleanup_authorizer):
    channel = None
    journal = None
    try:
        os.setsid()
        require(os.getsid(0) == os.getpid() and os.getuid() == parent_uid, "WATCHDOG_PROCESS")
        channel = ControlChannel(connection)
        state = WatchState(value)
        require(callable(cleanup_authorizer) and cleanup_authorizer("cleanup", sha(canonical(value))) is True, "CLEANUP_NOT_DELEGATED")
        journal = Journal(directory, value["invocation_id"])
        backend = backend_factory(value, profile_files, cleanup_authorizer)
        ready = {"kind": "ready", "scope_sha256": sha(canonical(value)), "pid": os.getpid(), "session": os.getsid(0), "uid": os.getuid(),
            "journal_identity_sha256": journal.identity_sha256}
        journal.append("ready", ready)
        deadline = Deadline(1)
        deadline.end = end
        channel.send(ready, deadline)
        signal.signal(signal.SIGINT, signal.SIG_IGN)
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
        reason, intact = "requested", True
        while True:
            step = "receive"
            try:
                message = channel.receive(deadline)
                exact(message, ("kind", "value"))
                if message["kind"] == "cleanup":
                    require(message["value"] == sha(canonical(value)), "WATCH_SCOPE")
                    break
                require(message["kind"] == "snapshot", "WATCH_MESSAGE")
                digest = state.accept(message["value"])
                step = "journal"
                journal.append("armed", state.current)
                step = "acknowledge"
                channel.send({"kind": "armed", "generation": state.current["generation"], "snapshot_sha256": digest}, deadline)
            except EOFError:
                reason = "owner_eof"
                break
            except Exception:
                reason = "deadline" if time.monotonic() >= end else "protocol_or_journal_error"
                if step == "journal":
                    intact = False
                break
        try:
            journal.append("triggered", {"reason": reason})
        except Exception:
            intact = False
        result = cleanup(state.current, backend, Deadline(LIMITS["cleanup_ms"] / 1000))
        validate_cleanup(result, state.current)
        final = {"kind": "cleanup", "reason": reason, "journal_intact": intact, "result": result}
        try:
            journal.append("cleanup", final)
        except Exception:
            final["journal_intact"] = False
        final["journal_sha256"] = sha(journal.raw) if final["journal_intact"] else None
        final["journal_identity_sha256"] = journal.identity_sha256
        try:
            channel.send(final, Deadline(1))
        except Exception:
            pass  # Parent loss does not remove the independently retained journal.
    except BaseException:
        if channel is not None:
            try:
                channel.send({"kind": "failure", "code": "WATCHDOG_FAILED"}, Deadline(0.1))
            except Exception:
                pass
    finally:
        if journal is not None:
            journal.close()
        connection.close()


class WatchdogProcess:
    """Separate-session host supervisor; native actors receive neither IPC endpoint."""
    def __init__(self, value, profile_files, evidence_dir, *, cleanup_authorizer=None, lifetime_seconds=12, backend_factory=_default_backend):
        self.scope = validate_scope(value)
        require(callable(cleanup_authorizer) and cleanup_authorizer("cleanup", sha(canonical(self.scope))) is True, "CLEANUP_NOT_DELEGATED")
        require(type(lifetime_seconds) in (int, float) and 0 < lifetime_seconds <= 12, "WATCHDOG_LIFETIME")
        require(callable(backend_factory), "WATCHDOG_BACKEND")
        self.state, self.closed = WatchState(value), False
        self.acknowledged = []
        self.journal_path = Path(evidence_dir) / (self.scope["invocation_id"] + ".watchdog.jsonl")
        self.deadline = Deadline(lifetime_seconds)
        context = multiprocessing.get_context("spawn")
        parent, child = context.Pipe(duplex=True)
        self.channel = ControlChannel(parent)
        self._process = context.Process(target=_worker, args=(child, self.scope, profile_files, str(evidence_dir),
            self.deadline.end, os.getuid(), backend_factory, cleanup_authorizer), daemon=False)
        try:
            self._process.start()
            child.close()
            ready = self.channel.receive(self.deadline)
            exact(ready, ("kind", "scope_sha256", "pid", "session", "uid", "journal_identity_sha256"))
            require(ready["kind"] == "ready" and ready["scope_sha256"] == sha(canonical(self.scope)) and
                type(ready["pid"]) is int and ready["pid"] == self._process.pid and type(ready["session"]) is int and ready["session"] == ready["pid"] and
                type(ready["uid"]) is int and ready["uid"] == os.getuid() and os.getsid(self._process.pid) == self._process.pid, "WATCHDOG_READY")
            self.journal_identity_sha256 = ready["journal_identity_sha256"]
            observed = read_journal(self.journal_path, self.journal_identity_sha256)
            require(observed["rows"] == [{"sequence": 0, "previous": None, "kind": "ready", "payload": ready}], "WATCHDOG_READY_JOURNAL")
            self.ready = copy.deepcopy(ready)
        except BaseException:
            child.close()
            self.channel.close()
            if self._process.pid is not None:
                self._process.kill()
                self._process.join(2)
            raise

    def publish(self, snapshot):
        require(not self.closed, "WATCHDOG_CLOSED")
        candidate = copy.deepcopy(self.state)
        digest = candidate.accept(snapshot)
        self.channel.send({"kind": "snapshot", "value": snapshot}, self.deadline)
        reply = self.channel.receive(self.deadline)
        exact(reply, ("kind", "generation", "snapshot_sha256"))
        require(reply == {"kind": "armed", "generation": snapshot["generation"], "snapshot_sha256": digest}, "WATCHDOG_ACK")
        self.state = candidate
        self.acknowledged.append(copy.deepcopy(snapshot))

    def close(self):
        require(not self.closed, "WATCHDOG_CLOSED")
        self.closed = True
        deadline = Deadline(LIMITS["cleanup_ms"] / 1000 + 1)
        try:
            try:
                self.channel.send({"kind": "cleanup", "value": sha(canonical(self.scope))}, deadline)
            except (BrokenPipeError, ConnectionResetError):
                pass  # An independently triggered final reply may already be queued.
            reply = self.channel.receive(deadline)
            self._validate_result(reply)
            self._process.join(deadline.remaining())
            require(not self._process.is_alive() and self._process.exitcode == 0, "WATCHDOG_EXIT")
            return reply
        finally:
            self.channel.close()
            self._process.join(max(0, deadline.end - time.monotonic()))

    def abandon(self):
        """Close the owner endpoint; independent child observes EOF and cleans up."""
        require(not self.closed, "WATCHDOG_CLOSED")
        self.closed = True
        self.channel.close()
        self._process.join(LIMITS["cleanup_ms"] / 1000 + 2)
        require(not self._process.is_alive(), "WATCHDOG_EXIT")

    def wait_for_cleanup(self):
        """Receive an independently triggered cleanup without sending more work."""
        require(not self.closed, "WATCHDOG_CLOSED")
        self.closed = True
        deadline = Deadline(18)
        try:
            reply = self.channel.receive(deadline)
            self._validate_result(reply)
            self._process.join(deadline.remaining())
            require(not self._process.is_alive() and self._process.exitcode == 0, "WATCHDOG_EXIT")
            return reply
        finally:
            self.channel.close()
            self._process.join(max(0, deadline.end - time.monotonic()))

    def _validate_result(self, reply):
        exact(reply, ("kind", "reason", "journal_intact", "result", "journal_sha256", "journal_identity_sha256"))
        require(reply["kind"] == "cleanup" and reply["reason"] in ("requested", "owner_eof", "deadline", "protocol_or_journal_error") and
            type(reply["journal_intact"]) is bool and reply["journal_identity_sha256"] == self.journal_identity_sha256, "WATCHDOG_RESULT")
        validate_cleanup(reply["result"], self.state.current)
        require(reply["journal_intact"] is True, "WATCHDOG_RETENTION_UNPROVEN")
        digest(reply["journal_sha256"])
        observed = read_journal(self.journal_path, self.journal_identity_sha256)
        require(observed["sha256"] == reply["journal_sha256"], "WATCHDOG_FINAL_JOURNAL")
        rows = observed["rows"]
        require([row["kind"] for row in rows] == ["ready", *(["armed"] * len(self.acknowledged)), "triggered", "cleanup"], "WATCHDOG_ACKNOWLEDGED_PREFIX")
        require(canonical(rows[0]["payload"]) == canonical(self.ready), "WATCHDOG_READY_JOURNAL")
        require([canonical(row["payload"]) for row in rows[1:1 + len(self.acknowledged)]] ==
            [canonical(snapshot) for snapshot in self.acknowledged], "WATCHDOG_ACKNOWLEDGED_PREFIX")
        require(rows[-2]["payload"] == {"reason": reply["reason"]}, "WATCHDOG_TRIGGER")
        expected = {key: value for key, value in reply.items() if key not in ("journal_sha256", "journal_identity_sha256")}
        require(canonical(rows[-1]["payload"]) == canonical(expected), "WATCHDOG_FINAL_JOURNAL")
