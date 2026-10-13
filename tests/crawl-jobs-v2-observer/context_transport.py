"""Pinned, noninteractive SSH exchange with bounded I/O, lifetime and cleanup."""
import copy
import ipaddress
import os
from pathlib import Path
import selectors
import signal
import stat
import subprocess
import tempfile

from contracts import LIMITS, Invalid, canonical, decode, require, runtime_architecture, sha, validate_preparation
import context_files as files
from context_provision import deployment_digest, transport_digest, validate_deployment, write_new
from runtime_context import ContextSource

SSH = "/usr/bin/ssh"
CLIENT_FILES = {"deployment.json": 4096, "context-source.key": 32, "id_ed25519": 16384}


def credentials(directory):
    fd = files.directory(directory, os.geteuid(), 0o700)
    try:
        return {name: files.read_at(fd, name, os.geteuid(), (0o600,), limit) for name, limit in CLIENT_FILES.items()}
    finally:
        os.close(fd)


def ssh_argv(deployment, identity, known_hosts, seconds):
    for path in (identity, known_hosts):
        require(Path(path).is_absolute() and not any(char in str(path) for char in ("\0", "\n", "\r", "%", "$")), "CONTEXT_SSH_PATH")
    known = '"' + str(known_hosts).replace("\\", "\\\\").replace('"', '\\"') + '"'
    options = {"BatchMode": "yes", "StrictHostKeyChecking": "yes", "UserKnownHostsFile": known,
        "GlobalKnownHostsFile": "/dev/null", "HostKeyAlias": files.HOST_ALIAS, "HostKeyAlgorithms": "ssh-ed25519",
        "PubkeyAcceptedAlgorithms": "ssh-ed25519", "IdentitiesOnly": "yes", "IdentityAgent": "none",
        "PreferredAuthentications": "publickey", "PasswordAuthentication": "no", "KbdInteractiveAuthentication": "no",
        "NumberOfPasswordPrompts": "0", "HostbasedAuthentication": "no", "GSSAPIAuthentication": "no",
        "ForwardAgent": "no", "ForwardX11": "no", "ClearAllForwardings": "yes", "RequestTTY": "no",
        "PermitLocalCommand": "no", "ProxyCommand": "none", "ProxyJump": "none", "CanonicalizeHostname": "no",
        "ControlMaster": "no", "ControlPath": "none", "ControlPersist": "no", "UpdateHostKeys": "no",
        "VerifyHostKeyDNS": "no", "AddKeysToAgent": "no", "PKCS11Provider": "none", "EscapeChar": "none",
        "ConnectionAttempts": "1", "ConnectTimeout": str(max(1, min(10, int(seconds + 0.999)))),
        "ServerAliveInterval": "1", "ServerAliveCountMax": "1", "LogLevel": "ERROR"}
    argv = [SSH, "-F", "/dev/null", "-T", "-4" if ipaddress.ip_address(deployment["host"]).version == 4 else "-6"]
    for name, value in options.items(): argv += ["-o", name + "=" + value]
    return [*argv, "-p", str(deployment["port"]), "-i", str(identity), "-l", files.SERVICE_USER,
        deployment["host"], files.REMOTE_COMMAND]


def verify_client(expected):
    fd = os.open(SSH, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, "rb") as stream:
        before = os.fstat(stream.fileno())
        require(stat.S_ISREG(before.st_mode) and before.st_uid == 0 and not before.st_mode & 0o022 and
            before.st_mode & 0o111 and 0 < before.st_size <= 32 * 1024 * 1024, "CONTEXT_SSH_BINARY")
        raw = stream.read(32 * 1024 * 1024 + 1)
        require(len(raw) == before.st_size and files.identity(before) == files.identity(os.fstat(stream.fileno())) and
            files.identity(before) == files.identity(os.stat(SSH, follow_symlinks=False)) and sha(raw) == expected, "CONTEXT_SSH_BINARY")


def exchange_process(argv, raw, deadline, *, spawn=subprocess.Popen):
    deadline.check()
    require(type(raw) is bytes and 0 < len(raw) <= LIMITS["receipt_bytes"] and raw.endswith(b"\n"), "CONTEXT_INPUT_BOUND")
    # Acquire the selector before starting a child, and guard spawn/setup too.
    mux = selectors.DefaultSelector()
    process, reaped, setup_complete = None, False, False
    try:
        deadline.check()
        process = spawn(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            env={"PATH": "/usr/bin:/bin", "LC_ALL": "C", "SSH_ASKPASS_REQUIRE": "never"},
            start_new_session=True, close_fds=True)
        output, offset = bytearray(), 0
        for pipe, event, label in ((process.stdin, selectors.EVENT_WRITE, "in"),
                (process.stdout, selectors.EVENT_READ, "out"), (process.stderr, selectors.EVENT_READ, "err")):
            os.set_blocking(pipe.fileno(), False)
            mux.register(pipe, event, label)
        setup_complete = True
        while mux.get_map():
            deadline.check()
            for event, _ in mux.select(min(deadline.remaining(), 0.05)):
                deadline.check()
                if event.data == "in":
                    try: count = os.write(event.fd, raw[offset:])
                    except (BlockingIOError, InterruptedError): continue
                    require(count > 0, "CONTEXT_INPUT_EOF")
                    offset += count
                    if offset == len(raw):
                        mux.unregister(event.fileobj); process.stdin.close()
                else:
                    try: chunk = os.read(event.fd, 4097)
                    except (BlockingIOError, InterruptedError): continue
                    if not chunk:
                        mux.unregister(event.fileobj)
                    else:
                        require(event.data == "out", "CONTEXT_SSH_STDERR")
                        output.extend(chunk)
                        require(len(output) <= 4 * LIMITS["receipt_bytes"], "CONTEXT_OUTPUT_BOUND")
        require(offset == len(raw) and output, "CONTEXT_EXCHANGE_INCOMPLETE")
        code = process.wait(timeout=deadline.remaining())
        reaped = True
        deadline.check()
        require(code == 0, "CONTEXT_SSH_FAILED")
        return bytes(output)
    finally:
        try:
            if process is not None and not reaped:
                # Do not reap a leader while descendants may still hold its pipes.
                # An empty map before setup is not observed EOF. A drained map
                # after setup permits reaping, but is not remote/group absence proof.
                if setup_complete and not mux.get_map():
                    try: process.wait(timeout=0); reaped = True
                    except subprocess.TimeoutExpired: pass
                if not reaped:
                    try:
                        try: os.killpg(process.pid, signal.SIGKILL)
                        except ProcessLookupError: pass
                    finally:
                        process.wait(timeout=LIMITS["cleanup_ms"] / 1000)
        finally:
            mux.close()
            if process is not None:
                for pipe in (process.stdin, process.stdout, process.stderr): pipe.close()


class SSHContextTransport:
    def __init__(self, bundle):
        self.bundle = Path(bundle)
        require(self.bundle.is_absolute(), "CONTEXT_BUNDLE_PATH")
        self._credentials = credentials(self.bundle)
        self._deployment = validate_deployment(decode(self._credentials["deployment.json"], 4096))

    @property
    def deployment(self):
        return copy.deepcopy(self._deployment)

    def __call__(self, raw, deadline):
        try:
            deadline.check()
            runtime_architecture(self._deployment["architecture"])
            require(credentials(self.bundle) == self._credentials, "CONTEXT_CREDENTIALS_CHANGED")
            verify_client(self.deployment["ssh_client_sha256"])
            # SSH opens immutable per-exchange copies, not paths that can be swapped
            # after admission. Temporary credential bytes never enter diagnostics.
            with tempfile.TemporaryDirectory(prefix="obs1-context-ssh-") as folder:
                identity, known = Path(folder) / "identity", Path(folder) / "known_hosts"
                write_new(identity, self._credentials["id_ed25519"])
                write_new(known, (files.HOST_ALIAS + " " + self.deployment["ssh_host_key"] + "\n").encode())
                argv = ssh_argv(self.deployment, identity, known, deadline.remaining())
                result = exchange_process(argv, raw, deadline)
                verify_client(self.deployment["ssh_client_sha256"])
                require(credentials(self.bundle) == self._credentials, "CONTEXT_CREDENTIALS_CHANGED")
                deadline.check()
                return result
        except Exception:
            raise Invalid("CONTEXT_SSH_EXCHANGE_REJECTED") from None


class _ProvisionedContextSource(ContextSource):
    def __init__(self, anchor, key, exchange, bindings, *, authorize=None):
        super().__init__(anchor, key, exchange, authorize=authorize)
        self._provisioning_bindings = dict(bindings)

    def acquire(self, case_id, invocation_sha256, preparation, daemon_sha256, deadline, budget):
        preparation = copy.deepcopy(validate_preparation(preparation))
        require(all(preparation["artifacts"][name] == value for name, value in self._provisioning_bindings.items()),
            "CONTEXT_PROVISIONING_BINDING")
        return super().acquire(case_id, invocation_sha256, preparation, daemon_sha256, deadline, budget)


def provisioned_source(bundle, preparation, *, authorize=None):
    """Load only operator-provisioned inputs; no discovery, TOFU or key scanning."""
    validate_preparation(preparation)
    transport = SSHContextTransport(bundle)
    value = transport.deployment
    runtime_architecture(value["architecture"])
    require(preparation["architecture"] == value["architecture"], "CONTEXT_ARCHITECTURE")
    bindings = {"runtime_context_deployment": deployment_digest(value), "runtime_context_transport": transport_digest(),
        "runtime_context_source": value["anchor"]["source_sha256"]}
    require(all(preparation["artifacts"][name] == digest for name, digest in bindings.items()), "CONTEXT_PROVISIONING_BINDING")
    source = _ProvisionedContextSource(value["anchor"], transport._credentials["context-source.key"], transport, bindings, authorize=authorize)
    source.provisioning_receipt = {"deployment_sha256": bindings["runtime_context_deployment"], "transport_source_sha256": bindings["runtime_context_transport"],
        "architecture": value["architecture"], "platform_sha256": preparation["platform_sha256"],
        "anchor_sha256": sha(canonical(value["anchor"])), "ssh_client_sha256": value["ssh_client_sha256"],
        "host_installation_verified": False, "execution_authorized": False, "runtime_evidence": False}
    return source
