#!/usr/bin/env python3
"""Opt-in local Docker controller for one approved, closed disposable M4 case.

No pull/build, remote Docker host, retained project, arbitrary command, or restart
policy. The executor receives neither the Docker socket nor a host bind mount.
"""
from __future__ import annotations

import argparse
from contextlib import contextmanager
import json
import os
from pathlib import Path
import re
import secrets
import selectors
import shutil
import signal
import stat
import subprocess
import sys
import time

import harness as h
import runtime_case as case
import claim_executor as claim_worker
import negative_executor as negative_worker
import negative_specs as ns
import admission

LABEL = "io.mifolyo.cj2.fixture"
ENTRY = "/app/tests/crawl-jobs-v2-redis/executor.py"
OUTPUT_LIMIT = 2 * 1024 * 1024


class CommandError(Exception):
    pass


class StageFailure(CommandError):
    def __init__(self, receipt):
        self.receipt = receipt
        super().__init__("EXECUTOR_STAGE_FAILED")


ADMISSION_FIELDS = {
    "INSPECTION_SHAPE": {"inspection", "Config", "Config.Labels", "HostConfig", "Mounts", "State"},
    "CONTAINER_IDENTITY": {"Image", "Config.User", "Config.Labels.fixture", "Config.Labels.case"},
    "ISOLATION": {"HostConfig.NetworkMode", "HostConfig.ReadonlyRootfs", "HostConfig.Privileged",
                  "HostConfig.Memory", "HostConfig.MemorySwap", "HostConfig.PidsLimit", "HostConfig.NanoCpus",
                  "HostConfig.RestartPolicy", "HostConfig.PortBindings", "HostConfig.PublishAllPorts",
                  "HostConfig.CapDrop", "HostConfig.CapAdd", "HostConfig.SecurityOpt", "HostConfig.Binds",
                  "HostConfig.Devices", "HostConfig.PidMode", "HostConfig.IpcMode", "HostConfig.Tmpfs",
                  "HostConfig.Dns", "HostConfig.DnsSearch", "HostConfig.DnsOptions", "HostConfig.ExtraHosts"},
    "CONTAINER_COMMAND": {"Config.Entrypoint", "Config.Cmd"},
    "CONTAINER_ENV": {"Config.Env"},
    "MOUNTS": {"Mounts.inventory", "Mounts.types", "Mounts.tmpfs"},
    "CONTAINER_STATE": {"State.Running"},
}


class ContainerAdmissionError(h.InvalidArtifact):
    """Only closed check names; never observed/expected values or raw inspect."""
    def __init__(self, code, checks):
        if (type(code) is not str or code not in ADMISSION_FIELDS or type(checks) not in (list, tuple)
                or not checks or not all(type(name) is str for name in checks)
                or len(checks) != len(set(checks)) or not set(checks) <= ADMISSION_FIELDS[code]):
            raise ValueError("INVALID_ADMISSION_DIAGNOSTIC")
        self.code, self.checks = code, tuple(checks)
        super().__init__(code)


def admission_checks(code, checks):
    failed = [name for name, passed in checks.items() if not passed]
    if failed:
        raise ContainerAdmissionError(code, failed)


def failure_details(error):
    if isinstance(error, ContainerAdmissionError):
        try:
            checked = ContainerAdmissionError(error.code, error.checks)
        except (ValueError, AttributeError):
            return {"code": "UNEXPECTED_FAILURE"}
        return {"code": checked.code, "checks": list(checked.checks)}
    if isinstance(error, KeyboardInterrupt):
        return {"code": "INTERRUPTED"}
    if isinstance(error, CommandError):
        allowed = {"COMMAND_TIMEOUT", "COMMAND_OUTPUT_LIMIT", "DOCKER_COMMAND_FAILED",
                    "INSPECTION_FAILED", "LIFECYCLE_DEADLINE", "EXECUTOR_STAGE_FAILED"}
        code = error.args[0] if len(error.args) == 1 and type(error.args[0]) is str and error.args[0] in allowed else "COMMAND_FAILED"
        return {"code": code}
    return {"code": "VALIDATION_FAILED" if isinstance(error, h.InvalidArtifact) else "UNEXPECTED_FAILURE"}


def command(argv, data=b"", timeout=30):
    """Drain both pipes with a combined hard bound; never echo stderr/input."""
    h.require(len(data) <= OUTPUT_LIMIT and timeout > 0, "COMMAND_BOUND")
    env = {k: v for k, v in os.environ.items() if k not in
           ("DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_TLS", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH")}
    process = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                               env=env, start_new_session=True)
    output = {"out": bytearray(), "err": bytearray()}
    deadline, offset = time.monotonic() + timeout, 0
    try:
        with selectors.DefaultSelector() as mux:
            for stream, name in ((process.stdout, "out"), (process.stderr, "err")):
                os.set_blocking(stream.fileno(), False)
                mux.register(stream, selectors.EVENT_READ, name)
            if data:
                os.set_blocking(process.stdin.fileno(), False)
                mux.register(process.stdin, selectors.EVENT_WRITE, "in")
            else:
                process.stdin.close()
            while mux.get_map():
                left = deadline - time.monotonic()
                if left <= 0:
                    raise CommandError("COMMAND_TIMEOUT")
                for event, _ in mux.select(min(left, 0.25)):
                    if event.data == "in":
                        count = os.write(event.fd, data[offset:offset + 16384])
                        offset += count
                        if offset == len(data):
                            mux.unregister(event.fileobj)
                            event.fileobj.close()
                    else:
                        chunk = os.read(event.fd, 16384)
                        if not chunk:
                            mux.unregister(event.fileobj)
                        else:
                            output[event.data].extend(chunk)
                            if sum(map(len, output.values())) > OUTPUT_LIMIT:
                                raise CommandError("COMMAND_OUTPUT_LIMIT")
            code = process.wait(timeout=max(0.01, deadline - time.monotonic()))
            return code, bytes(output["out"]), bytes(output["err"])
    except BaseException:
        # Do not reap/poll the leader first: it can have exited while children
        # still own the output pipes. Abort the owned session before final wait.
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        process.wait(timeout=5)
        raise
    finally:
        for stream in (process.stdin, process.stdout, process.stderr):
            stream.close()


def scope_paths():
    return ["docs/crawl-jobs-v2.md", "contracts/crawl-jobs-v2/digest-vectors.json",
            "scripts/generate-crawl-jobs-v2-bundle.py",
            "services/spider/internal/database/crawljobsv2/script_bundle_generated.go",
            *["tests/crawl-jobs-v2-redis/" + name for name in case.FILES],
            *["services/spider/internal/database/crawljobsv2/lua/" + op.lower() + ".lua"
              for op in h.source_identity()[1]]]


def verify_revision(expected):
    prefix = ["git", "-C", str(h.ROOT)]
    code, raw, _ = command([*prefix, "rev-parse", "HEAD"])
    h.require(code == 0 and raw.decode().strip() == expected, "REVIEWED_COMMIT")
    paths = scope_paths()
    h.require(command([*prefix, "ls-files", "--error-unmatch", "--", *paths])[0] == 0, "UNTRACKED_INPUT")
    h.require(command([*prefix, "diff", "--quiet", "HEAD", "--", *paths])[0] == 0, "DIRTY_INPUT")


def image_admission(value, expected, architecture, harness=False):
    h.require(type(value) is dict and value.get("Id") == expected and value.get("Os") == "linux" and
              value.get("Architecture") == architecture, "IMAGE_IDENTITY")
    h.require(type(value.get("RootFS", {}).get("Layers")) is list and value["RootFS"]["Layers"], "IMAGE_LAYERS")
    h.require(all(type(layer) is str and layer.startswith("sha256:") and h.nonzero(layer[7:])
                  for layer in value["RootFS"]["Layers"]), "IMAGE_LAYERS")
    config = value.get("Config", {})
    h.require(not config.get("OnBuild"), "ONBUILD")
    h.require(not any("=" in entry and entry.split("=", 1)[0].lower().endswith("proxy") and
                       entry.split("=", 1)[1] for entry in config.get("Env", [])), "IMAGE_PROXY")
    environment_sha = admission.environment_digest(config.get("Env", []))
    h.require(set(config.get("Volumes") or {}) <= (set() if harness else {"/data"}), "IMAGE_VOLUMES")
    return {"id": expected, "os": "linux", "architecture": architecture,
             "environment_sha256": environment_sha,
             "layer_digest": h.digest(h.canonical({"layers": value["RootFS"]["Layers"]}))}


def container_spec(name, role, fixture_id, image, volumes, case_id=case.CASE, environment_sha256=None):
    case.sources(case_id)
    init = role == "init"
    mounts = [(volumes["control"], "/run/cj2", role in ("executor", "revocation"))]
    if role in ("init", "redis"):
        mounts.append((volumes["data"], "/data", False))
    entrypoint, command = admission.command_for(role)
    environment_sha256 = admission.environment_digest([]) if environment_sha256 is None else environment_sha256
    h.require(h.nonzero(environment_sha256) and len(set(volumes.values())) == 2, "CONTAINER_SPEC")
    return {"name": name, "role": role, "fixture_id": fixture_id, "image": image, "case": case_id,
             "entrypoint": entrypoint, "command": command, "environment_sha256": environment_sha256,
            "uid": "0:0" if init else "65534:65534", "cap_add": ["CHOWN"] if init else [],
            "memory": 134217728 if init else (553648128 if role == "redis" else 268435456),
            "mounts": mounts}


def allowed_cap_add(actual, expected):
    # Docker canonicalizes --cap-add CHOWN to CAP_CHOWN in inspect. Admit only
    # these two spellings of the sole init capability; not arbitrary CAP_* names.
    if expected == []:
        return actual is None or type(actual) is list and actual == []
    return (expected == ["CHOWN"] and type(actual) is list and len(actual) == 1
            and actual[0] in ("CHOWN", "CAP_CHOWN"))


def verify_container(value, spec, running):
    admission_checks("INSPECTION_SHAPE", {"inspection": type(value) is dict})
    config, host, mounts, state = (value.get("Config"), value.get("HostConfig"), value.get("Mounts"), value.get("State"))
    admission_checks("INSPECTION_SHAPE", {"Config": type(config) is dict, "HostConfig": type(host) is dict,
                                        "Mounts": type(mounts) is list, "State": type(state) is dict})
    admission_checks("INSPECTION_SHAPE", {"Config.Labels": type(config.get("Labels")) is dict,
                                        "Mounts": all(type(row) is dict for row in mounts)})
    admission_checks("CONTAINER_IDENTITY", {
        "Image": value.get("Image") == spec["image"], "Config.User": config.get("User") == spec["uid"],
        "Config.Labels.fixture": config["Labels"].get(LABEL) == spec["fixture_id"],
        "Config.Labels.case": config["Labels"].get("io.mifolyo.cj2.case") == spec.get("case", case.CASE),
    })
    admission_checks("CONTAINER_COMMAND", {"Config.Entrypoint": config.get("Entrypoint") == spec["entrypoint"],
                                           "Config.Cmd": config.get("Cmd") == spec["command"]})
    try:
        environment_sha = admission.environment_digest(config.get("Env"))
    except h.InvalidArtifact:
        environment_sha = None
    admission_checks("CONTAINER_ENV", {"Config.Env": environment_sha == spec["environment_sha256"]})
    admission_checks("ISOLATION", {
        "HostConfig.NetworkMode": host.get("NetworkMode") == "none",
        "HostConfig.ReadonlyRootfs": host.get("ReadonlyRootfs") is True,
        "HostConfig.Privileged": host.get("Privileged") is False,
        "HostConfig.Memory": host.get("Memory") == spec["memory"],
        "HostConfig.MemorySwap": host.get("MemorySwap") == spec["memory"],
        "HostConfig.PidsLimit": host.get("PidsLimit") == 64,
        "HostConfig.NanoCpus": host.get("NanoCpus") == 1000000000,
        "HostConfig.RestartPolicy": type(host.get("RestartPolicy")) is dict and host["RestartPolicy"].get("Name") == "no",
        "HostConfig.PortBindings": not host.get("PortBindings"),
        "HostConfig.PublishAllPorts": not host.get("PublishAllPorts"),
        "HostConfig.CapDrop": host.get("CapDrop") == ["ALL"],
        "HostConfig.CapAdd": allowed_cap_add(host.get("CapAdd"), spec["cap_add"]),
        "HostConfig.SecurityOpt": host.get("SecurityOpt") == ["no-new-privileges:true"],
        "HostConfig.Binds": not host.get("Binds"), "HostConfig.Devices": not host.get("Devices"),
        "HostConfig.PidMode": host.get("PidMode", "") == "", "HostConfig.IpcMode": host.get("IpcMode") == "private",
        "HostConfig.Tmpfs": host.get("Tmpfs") == {"/tmp": "rw,noexec,nosuid,size=16777216"},
        **{"HostConfig." + field: host.get(field) is None or type(host.get(field)) is list and not host[field]
           for field in ("Dns", "DnsSearch", "DnsOptions", "ExtraHosts")},
    })
    volumes = [row for row in mounts if row.get("Type") == "volume"]
    tmpfs = [row for row in mounts if row.get("Type") == "tmpfs"]
    actual = {(row.get("Name"), row.get("Destination"), not row.get("RW")) for row in volumes}
    admission_checks("MOUNTS", {
        "Mounts.inventory": len(volumes) == len(spec["mounts"]) and actual == set(spec["mounts"]),
        "Mounts.types": len(volumes) + len(tmpfs) == len(mounts),
        "Mounts.tmpfs": len(tmpfs) <= 1 and all(row.get("Destination") == "/tmp" and row.get("RW") is True for row in tmpfs),
    })
    admission_checks("CONTAINER_STATE", {"State.Running": state.get("Running") is running})


class Docker:
    evidence_kind = "real_redis"

    def __init__(self):
        binary = shutil.which("docker")
        h.require(binary is not None, "DOCKER_MISSING")
        self.prefix = [binary, "--host", "unix:///var/run/docker.sock"]
        self.deadline = float("inf")
        self.approval_expires_at_ms = None
        self.case_id = case.CASE

    def timeout(self, requested=30):
        remaining = min(requested, self.deadline - time.monotonic())
        if self.approval_expires_at_ms is not None:
            remaining = min(remaining, self.approval_expires_at_ms / 1000 - time.time())
        if remaining <= 0:
            raise CommandError("LIFECYCLE_DEADLINE")
        return remaining

    def call(self, *args, data=b"", timeout=30):
        code, out, _ = command([*self.prefix, *args], data, self.timeout(timeout))
        if code:
            raise CommandError("DOCKER_COMMAND_FAILED")
        return out

    def inspect(self, kind, name):
        code, out, error = command([*self.prefix, kind, "inspect", name], timeout=self.timeout())
        if code:
            # A dead daemon/permission failure is never proof of absence.
            text = error.decode("utf-8", "replace").lower()
            if name.lower() in text and ("no such " + kind in text or (kind == "volume" and "no such volume" in text)):
                return None
            raise CommandError("INSPECTION_FAILED")
        rows = json.loads(out)
        h.require(type(rows) is list and len(rows) == 1 and type(rows[0]) is dict, "INSPECTION_SHAPE")
        return rows[0]

    def volume(self, name, fixture_id):
        self.call("volume", "create", "--label", LABEL + "=" + fixture_id,
                   "--label", "io.mifolyo.cj2.case=" + self.case_id, name)

    def attachments(self, volume):
        raw = self.call("container", "ls", "--all", "--filter", "volume=" + volume, "--format", "{{.Names}}")
        names = raw.decode("ascii").splitlines()
        h.require(len(names) <= 64 and len(set(names)) == len(names) and all(
            re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}", name) for name in names), "VOLUME_SHARED")
        return names

    def create(self, spec):
        args = ["container", "create", "--name", spec["name"], "--pull", "never", "--network", "none",
                "--restart", "no", "--read-only", "--user", spec["uid"], "--cap-drop", "ALL",
                "--security-opt", "no-new-privileges:true", "--memory", str(spec["memory"]),
                "--memory-swap", str(spec["memory"]), "--pids-limit", "64", "--cpus", "1", "--ipc", "private",
                "--label", LABEL + "=" + spec["fixture_id"], "--label", "io.mifolyo.cj2.case=" + spec.get("case", case.CASE),
                "--tmpfs", "/tmp:rw,noexec,nosuid,size=16777216", "--log-driver", "none"]
        for cap in spec["cap_add"]:
            args += ["--cap-add", cap]
        for name, target, readonly in spec["mounts"]:
            args += ["--mount", f"type=volume,source={name},target={target},volume-nocopy" + (",readonly" if readonly else "")]
        if spec["role"] == "redis":
            args += ["--entrypoint", "redis-server", spec["image"], "/run/cj2/redis.conf"]
        else:
            args += ["--entrypoint", "python3", spec["image"], "-B", ENTRY, "hold"]
        self.call(*args)

    def start(self, name):
        self.call("container", "start", name)

    def kill(self, name):
        self.call("container", "kill", "--signal", "KILL", name)
        state = self.inspect("container", name)
        h.require(state is not None and not state["State"]["Running"], "KILL_NOT_OBSERVED")

    def stage(self, name, stage, request, timeout):
        milliseconds = int(self.timeout(timeout) * 1000)
        h.require(1 <= milliseconds <= 30000, "STAGE_TIMEOUT")
        code, out, _ = command([*self.prefix, "container", "exec", "--interactive", name, "python3", "-B", ENTRY, stage, str(milliseconds)],
                               data=h.canonical(request), timeout=self.timeout(timeout))
        result = h.decode(out)
        selected = case.case_for_plan(request["plan"])
        h.require(set(result) == {"stage", "status", "recipe_sha256", "isolation", "result"} and
                  result["stage"] == stage and result["status"] in ("PASS", "FAIL") and
                  result["recipe_sha256"] == case.recipe_sha256(selected), "STAGE_RESULT")
        if result["status"] == "FAIL":
            h.require(code != 0, "STAGE_FAILURE")
            if selected in ns.CASES:
                h.require(stage in ("resume", "measure"), "STAGE_FAILURE")
                negative_worker.validate_stage_result(stage, result["result"], request, result["isolation"], successful=False)
            else:
                h.require(selected == case.CLAIM_CASE and stage == "measure", "STAGE_FAILURE")
                claim_worker.validate_measurement(result["result"], False)
            raise StageFailure(result)
        h.require(code == 0, "STAGE_EXIT")
        return result

    def quiesce(self, name, fixture_id):
        """Stop the actual container, wait, and remove it before a teardown helper.

        A fresh, differently named helper prevents delayed exec requests from
        reaching a restarted container with the old identity.
        """
        current = self.inspect("container", name)
        h.require(current is not None and owned(current, "container", fixture_id, getattr(self, "case_id", case.CASE)), "WORKER_OWNER")
        if current.get("State", {}).get("Running") is True:
            self.call("container", "kill", "--signal", "KILL", name)
        self.call("container", "wait", name)
        stopped = self.inspect("container", name)
        h.require(stopped is not None and owned(stopped, "container", fixture_id, getattr(self, "case_id", case.CASE)) and
                  stopped.get("State", {}).get("Running") is False and
                  type(stopped["State"].get("Pid")) is int and stopped["State"]["Pid"] == 0,
                  "WORKER_NOT_STOPPED")
        self.remove("container", name)
        h.require(self.inspect("container", name) is None, "WORKER_REMAINS")
        return {"container": name, "stopped": True, "pid": 0, "removed": True}

    def remove(self, kind, name):
        if kind == "container":
            self.call("container", "rm", "--force", name)
        else:
            self.call("volume", "rm", name)


def owned(value, kind, fixture_id, case_id=case.CASE):
    labels = (value.get("Config", {}) if kind == "container" else value).get("Labels", {}) or {}
    return labels.get(LABEL) == fixture_id and labels.get("io.mifolyo.cj2.case") == case_id


def verify_volume_attachments(backend, volumes, specs):
    allowed = {value["name"]: {volume for volume, _, _ in value["mounts"]} for value in specs.values()}
    attached = {volume: backend.attachments(volume) for volume in volumes.values()}
    admission.volume_attachments(volumes, attached, allowed)
    by_name = {value["name"]: value for value in specs.values()}
    for name in sorted({name for names in attached.values() for name in names}):
        observed = backend.inspect("container", name)
        h.require(type(observed) is dict and type(observed.get("State", {}).get("Running")) is bool, "VOLUME_SHARED")
        verify_container(observed, by_name[name], observed["State"]["Running"])


def cleanup(backend, resources, fixture_id, *, on_removed=None):
    results = []
    # Stop/remove workers before Redis even when exec cancellation was ambiguous.
    order = sorted(reversed(resources), key=lambda item:
                   2 if item[0] == "volume" else (1 if item[1].endswith("-redis") else 0))
    for kind, name in order:
        row = {"kind": kind, "name": name, "removed": False}
        try:
            current = backend.inspect(kind, name)
            if current is not None:
                h.require(owned(current, kind, fixture_id, getattr(backend, "case_id", case.CASE)), "RESOURCE_OWNER_MISMATCH")
                if kind == "volume":
                    h.require(backend.attachments(name) == [], "VOLUME_SHARED")
                backend.remove(kind, name)
            h.require(backend.inspect(kind, name) is None, "RESOURCE_REMAINS")
            row["removed"] = True
        except (Exception, KeyboardInterrupt) as error:
            row["error"] = "CLEANUP_NOT_PROVEN"
            row["failure_details"] = failure_details(error)
        results.append(row)
        if row["removed"] and on_removed:
            try:
                on_removed(kind, name)
            except (Exception, KeyboardInterrupt) as error:
                row["journal_failure"] = failure_details(error)
    return results


def verify_revocation(result, targets):
    case.validate_revocation(result, targets)


@contextmanager
def cleanup_signals():
    # The first interrupt enters cleanup. Later interrupts must not skip it.
    old = {sig: signal.signal(sig, signal.SIG_IGN) for sig in (signal.SIGINT, signal.SIGTERM)}
    try:
        yield
    finally:
        for sig, handler in old.items():
            signal.signal(sig, handler)


def execute(plan, approval, backend, *, revision_check=verify_revision, journal=None, action_journal=None):
    case.validate_approval(plan, approval, int(time.time() * 1000))
    revision_check(approval["commit"])
    case.validate_approval(plan, approval, int(time.time() * 1000))
    fixture_id = secrets.token_hex(16)
    selected = case.case_for_plan(plan)
    backend.case_id = selected
    prefix = "cj2-m4-" + fixture_id
    volumes = {role: prefix + "-" + role for role in ("data", "control")}
    names = {role: prefix + "-" + role for role in ("init", "redis", "executor", "revocation")}
    credentials = {role: secrets.token_hex(32) for role in case.roles(selected)}
    material = ({"owner_a": secrets.token_hex(16), "owner_b": secrets.token_hex(16),
                 "token_a": secrets.token_hex(32), "token_b": secrets.token_hex(32), "wrong_token": secrets.token_hex(32)}
                if case.uses_material(selected) else {})
    binding = case.fixture(plan, fixture_id, material) if selected != case.CASE else None
    private_values = [*credentials.values(), *material.values()]
    if binding:
        worker_binding = case.worker_fixture(binding)
        if worker_binding:
            private_values += [worker_binding["identities"][label]["reservation_id"] for label in ("a", "b")]
        if selected in ns.CASES:
            private_values.append(binding["admin_nonce"])
        private_values += [case.claim.URL, case.claim.ROBOTS]
    resources, specs = [], {}
    report = {"case": selected, "fixture_id": fixture_id, "evidence_kind": backend.evidence_kind,
              "plan_sha256": h.digest(h.canonical(plan)), "approval_sha256": h.digest(h.canonical(approval)),
              "recipe_sha256": case.recipe_sha256(selected), "m4_accepted": False, "case_passed": False,
              "images": {}, "container_admission": {}, "stages": {}, "cleanup": [],
              "worker_quiescence": "not_proven", "revocation": "not_proven", "actions": []}
    intent = {"case": selected, "fixture_id": fixture_id, "status": "INCOMPLETE",
              "plan_sha256": report["plan_sha256"], "approval_sha256": report["approval_sha256"],
              "recipe_sha256": report["recipe_sha256"], "m4_accepted": False,
              "resource_names": {"volumes": volumes, "containers": names}}
    # Publish exact names before any Docker mutation. An uncatchable controller
    # kill leaves an INCOMPLETE intent, never a success or an unidentifiable run.
    h.require(backend.evidence_kind != "real_redis" or (callable(journal) and callable(action_journal)), "JOURNAL_REQUIRED")
    if journal:
        journal(intent)
    case.validate_approval(plan, approval, int(time.time() * 1000))
    report["intent_sha256"] = h.digest(h.canonical(intent))
    wall_now, mono_now = time.time(), time.monotonic()
    deadline = min(mono_now + approval["max_seconds"],
                   mono_now + approval["expires_at_ms"] / 1000 - wall_now)
    h.require(deadline > mono_now, "EXECUTION_DEADLINE")
    backend.deadline = deadline
    backend.approval_expires_at_ms = approval["expires_at_ms"]
    phase = "images"
    redis_started = False

    def action(name, subject, cleanup_mode=False):
        try:
            h.require(len(report["actions"]) < 128, "ACTION_BOUND")
            row = {"sequence": len(report["actions"]), "action": name, "subject": subject,
                   "at_ms": int(time.time() * 1000)}
            report["actions"].append(row)
            if action_journal:
                action_journal(fixture_id, row)
        except (Exception, KeyboardInterrupt) as error:
            if not cleanup_mode:
                raise
            # Preserve observed cleanup facts and keep attempting teardown.
            # Incomplete journaling still invalidates the final case evidence.
            report["journal_failure"] = failure_details(error)

    def remaining():
        left = min(30, deadline - time.monotonic(), (approval["expires_at_ms"] / 1000) - time.time())
        h.require(left > 0, "EXECUTION_DEADLINE")
        return left

    def stage(name, selected_roles, previous=None, cleanup_mode=False):
        request = {"plan": plan, "recipe_sha256": report["recipe_sha256"], "fixture_id": fixture_id,
                    "credentials": {role: credentials[role] for role in selected_roles}, "previous": previous or {}}
        if material:
            request["claim_material"] = material
        target = names["init"] if name == "init" else names["revocation" if name == "revoke" else "executor"]
        try:
            result = backend.stage(target, name, request, 30 if cleanup_mode else remaining())
        except StageFailure as failure:
            raw = h.canonical(failure.receipt)
            h.require(not any(value.encode() in raw for value in private_values), "PRIVATE_STAGE_OUTPUT")
            if selected in ns.CASES:
                negative_worker.validate_stage_result(name, failure.receipt["result"], request, failure.receipt["isolation"], successful=False)
            else:
                claim_worker.validate_measurement(failure.receipt["result"], False)
                h.require(failure.receipt["result"]["fixture_sha256"] == request["previous"]["fixture_summary"]["fixture_sha256"],
                           "CLAIM_EVIDENCE_BINDING")
            report["stages"][name] = failure.receipt
            raise
        raw = h.canonical(result)
        h.require(len(raw) <= OUTPUT_LIMIT, "STAGE_OUTPUT_BOUND")
        h.require(not any(value.encode() in raw for value in private_values), "PRIVATE_STAGE_OUTPUT")
        if selected == case.CLAIM_CASE:
            claim_worker.validate_stage_result(name, result["result"], request)
        elif selected in ns.CASES:
            negative_worker.validate_stage_result(name, result["result"], request, result["isolation"])
        if name != "ready":
            report["stages"][name] = result
        action("stage_completed", name, cleanup_mode)
        return result["result"]

    def create(role, cleanup_mode=False):
        def check():
            if cleanup_mode:
                h.require(time.monotonic() < backend.deadline, "CLEANUP_DEADLINE")
            else:
                remaining()
        check()
        name = names[role]
        h.require(backend.inspect("container", name) is None, "RESOURCE_EXISTS")
        image_role = "redis" if role == "redis" else "harness"
        spec = container_spec(name, role, fixture_id, plan["inputs"][image_role + "_image"], volumes, selected,
                              report["images"][image_role]["environment_sha256"])
        specs[role] = spec
        resources.append(("container", name))  # Before create: lost reply may hide a successful create.
        check()
        backend.create(spec)
        action("container_created", role, cleanup_mode)
        verify_container(backend.inspect("container", name), spec, False)
        verify_volume_attachments(backend, volumes, specs)
        check()
        backend.start(name)
        observed = backend.inspect("container", name)
        verify_container(observed, spec, True)
        verify_volume_attachments(backend, volumes, specs)
        report["container_admission"][role] = {"spec": spec, "verified": True,
            "inspection_sha256": h.digest(h.canonical(observed))}
        action("container_started_and_verified", role, cleanup_mode)

    def ready():
        remaining()
        verify_container(backend.inspect("container", names["redis"]), specs["redis"], True)
        stage("ready", ("setup",))

    try:
        for role in ("redis", "harness"):
            image = plan["inputs"][role + "_image"]
            value = backend.inspect("image", image)
            h.require(value is not None, "IMAGE_NOT_LOCAL")
            report["images"][role] = image_admission(value, image, approval["architecture"], role == "harness")
        for name in volumes.values():
            phase = "volumes"
            remaining()
            h.require(backend.inspect("volume", name) is None, "VOLUME_EXISTS")
            resources.append(("volume", name))
            remaining()
            backend.volume(name, fixture_id)
            h.require(owned(backend.inspect("volume", name), "volume", fixture_id, selected), "VOLUME_OWNER")
            action("volume_created_and_verified", name)
        phase = "init"
        create("init")
        initialized = stage("init", case.roles(selected))
        h.require(initialized.get("empty_volumes_verified") is True and
                  initialized.get("config_sha256") == plan["redis_config"]["sha256"] and
                  initialized.get("acl_file_sha256") == h.digest(case.acl_file(credentials, selected, binding, plan)), "INITIALIZATION_EVIDENCE")
        backend.remove("container", names["init"])
        h.require(backend.inspect("container", names["init"]) is None, "INIT_REMAINS")
        del specs["init"]
        action("container_removed_and_verified", "init")
        phase = "start"
        create("executor")
        # Mark before starting: a lost start reply cannot skip revocation attempts.
        redis_started = True
        create("redis")
        ready()
        phase = "probe"
        probe = stage("probe", ("setup",))
        remaining()
        backend.kill(names["redis"])
        action("container_killed_and_verified", "redis")
        remaining()
        backend.start(names["redis"])
        action("container_restarted", "redis")
        ready()
        phase = "resume"
        resumed = stage("resume", case.stage_roles(selected, "resume"), probe)
        verify_revocation(resumed.get("early_revocation"), case.early_roles(selected))
        phase = "measure"
        stage("measure", case.measure_roles(selected), resumed)
        remaining()
        report["case_passed"] = True
    except (Exception, KeyboardInterrupt) as error:
        report["failure_phase"] = phase
        report["failure_details"] = failure_details(error)
    finally:
        with cleanup_signals():
            backend.deadline = time.monotonic() + 60
            backend.approval_expires_at_ms = None  # Expiry never disables cleanup.
            if redis_started:
                try:
                    report["worker_quiescence"] = backend.quiesce(names["executor"], fixture_id)
                    proof = report["worker_quiescence"]
                    h.require(type(proof) is dict and proof.get("container") == names["executor"] and
                              proof.get("stopped") is True and type(proof.get("pid")) is int and
                               proof["pid"] == 0 and proof.get("removed") is True, "WORKER_NOT_QUIESCED")
                    action("worker_quiesced_and_removed", "executor", True)
                    specs.pop("executor", None)
                    create("revocation", cleanup_mode=True)
                    result = stage("revoke", case.roles(selected), cleanup_mode=True)
                    verify_revocation(result.get("revocation"), case.roles(selected))
                    report["revocation"] = "verified"
                    action("credentials_revoked_and_verified", "all-roles", True)
                except (Exception, KeyboardInterrupt) as error:
                    report["revocation"] = "not_proven"
                    report["revocation_failure"] = failure_details(error)
            else:
                report["revocation"] = "server_never_started"
            report["cleanup"] = cleanup(backend, resources, fixture_id,
                on_removed=lambda kind, name: action(kind + "_removed_and_verified", name, True))
            credentials.clear()
            material.clear()
    complete = (report["case_passed"] and report["revocation"] == "verified" and
                all(row["removed"] and "journal_failure" not in row for row in report["cleanup"]) and
                "journal_failure" not in report)
    report["verdict"] = "PASS" if complete else "FAIL"
    report["case_evidence_valid"] = complete and backend.evidence_kind == "real_redis"
    return report


def write_report(directory, report, *, intent=False):
    h.require(directory.is_absolute() and directory.is_dir() and not directory.is_symlink(), "EVIDENCE_DIRECTORY")
    h.require(type(report.get("fixture_id")) is str and re.fullmatch(r"[0-9a-f]{32}", report["fixture_id"]), "EVIDENCE_NAME")
    raw = h.canonical(report)
    h.require(len(raw) <= OUTPUT_LIMIT, "EVIDENCE_SIZE")
    path = directory / (report["fixture_id"] + (".intent.json" if intent else ".json"))
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "wb") as stream:
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())
    directory_fd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(directory_fd)
    finally:
        os.close(directory_fd)
    return path


def write_action(directory, fixture_id, event):
    """Durable, bounded controller-action receipts, separate from private state."""
    h.require(directory.is_absolute() and directory.is_dir() and not directory.is_symlink(), "EVIDENCE_DIRECTORY")
    h.require(type(fixture_id) is str and re.fullmatch(r"[0-9a-f]{32}", fixture_id), "EVIDENCE_NAME")
    h.exact(event, {"sequence", "action", "subject", "at_ms"})
    h.require(type(event["sequence"]) is int and 0 <= event["sequence"] < 128 and
              type(event["at_ms"]) is int and 0 < event["at_ms"] <= h.MAX_EXACT and
              event["action"] in {"stage_completed", "container_created", "container_started_and_verified",
                                  "volume_created_and_verified", "container_removed_and_verified",
                                  "container_killed_and_verified", "container_restarted", "volume_removed_and_verified",
                                  "worker_quiesced_and_removed", "credentials_revoked_and_verified"} and
              type(event["subject"]) is str and re.fullmatch(r"[a-z0-9-]{1,96}", event["subject"]), "ACTION_FIELDS")
    raw = h.canonical(event)
    flags = os.O_WRONLY | os.O_APPEND | os.O_NOFOLLOW | os.O_NONBLOCK
    if event["sequence"] == 0:
        flags |= os.O_CREAT | os.O_EXCL
    fd = os.open(directory / (fixture_id + ".actions.jsonl"), flags, 0o600)
    with os.fdopen(fd, "wb") as stream:
        info = os.fstat(stream.fileno())
        h.require(stat.S_ISREG(info.st_mode) and info.st_size + len(raw) <= OUTPUT_LIMIT, "ACTION_FILE_BOUND")
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())
    if event["sequence"] == 0:
        directory_fd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(directory_fd)
        finally:
            os.close(directory_fd)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    recipe_parser = sub.add_parser("recipe", help="Offline case/ACL/source manifest; starts nothing")
    recipe_parser.add_argument("--case", choices=tuple(case.CASES), default=case.CASE)
    run = sub.add_parser("run", help="Requires a separately reviewed, hash-bound execution approval")
    run.add_argument("--plan", type=Path, required=True)
    run.add_argument("--approval", type=Path, required=True)
    run.add_argument("--expected-approval-sha256", required=True)
    run.add_argument("--evidence-dir", type=Path, required=True)
    args = parser.parse_args()
    try:
        if args.command == "recipe":
            sys.stdout.buffer.write(h.canonical({"recipe": case.recipe(args.case), "sha256": case.recipe_sha256(args.case), "execution_authorized": False}))
            return 0
        approval_bytes = h.read_artifact(args.approval)
        h.require(h.nonzero(args.expected_approval_sha256) and h.digest(approval_bytes) == args.expected_approval_sha256, "APPROVAL_HASH")
        h.require(args.evidence_dir.is_absolute() and args.evidence_dir.is_dir() and not args.evidence_dir.is_symlink(), "EVIDENCE_DIRECTORY")
        def interrupted(*_):
            raise KeyboardInterrupt()
        old = signal.signal(signal.SIGTERM, interrupted)
        try:
            report = execute(h.decode(h.read_artifact(args.plan)), h.decode(approval_bytes), Docker(),
                              journal=lambda value: write_report(args.evidence_dir, value, intent=True),
                              action_journal=lambda fixture, event: write_action(args.evidence_dir, fixture, event))
            path = write_report(args.evidence_dir, report)
        finally:
            signal.signal(signal.SIGTERM, old)
        print("M4 case " + report["verdict"] + "; evidence: " + str(path))
        return 0 if report["case_evidence_valid"] else 1
    except (Exception, KeyboardInterrupt):
        print("M4 execution refused or failed; no acceptance claimed", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
