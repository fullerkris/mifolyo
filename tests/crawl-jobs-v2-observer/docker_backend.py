"""Local-only closed Docker adapter; mutations require an outer authorizer.

There is no run CLI or approval issuer here. Construction and metadata reads do
not authorize mutation. Native/runtime admission remains a separate next layer.
"""
import json
import os
from pathlib import Path
import selectors
import signal
import stat
import subprocess
import time

from contracts import LIMITS, Invalid, canonical, digest, exact, integer, require, sha, validate_preparation
from owned_resources import ACTORS, VOLUMES, TMPFS_OPTIONS, Registry, admit_container, admit_volume, container_spec, container_identity, disabled_healthcheck, empty_optional, hex_id, labels, name, validate_scope, validate_snapshot, volume_spec
from profiles import profile
from streams import ProcessStream
import runtime_admission
from owned_resources import timestamp

MAX_COMMAND_BYTES = 262144


def clean_environment():
    return {key: value for key, value in os.environ.items() if key not in
        ("DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_TLS", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH")}


def command(argv, deadline):
    """Drain both pipes; no shell and no unbounded subprocess.run capture."""
    deadline.check()
    output = {"out": bytearray(), "err": bytearray()}
    process = subprocess.Popen(argv, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        env=clean_environment(), start_new_session=True, close_fds=True)
    reaped = False
    try:
        with selectors.DefaultSelector() as mux:
            for stream, key in ((process.stdout, "out"), (process.stderr, "err")):
                os.set_blocking(stream.fileno(), False)
                mux.register(stream, selectors.EVENT_READ, key)
            while mux.get_map():
                for event, _ in mux.select(min(deadline.remaining(), 0.05)):
                    chunk = os.read(event.fd, 4096)
                    if not chunk:
                        mux.unregister(event.fileobj)
                        continue
                    require(sum(map(len, output.values())) + len(chunk) <= MAX_COMMAND_BYTES, "COMMAND_OUTPUT_BOUND")
                    output[event.data].extend(chunk)
        code = process.wait(timeout=deadline.remaining())
        reaped = True
        return code, bytes(output["out"]), bytes(output["err"])
    except BaseException:
        if not reaped:
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            process.wait(timeout=5)
        raise
    finally:
        process.stdout.close()
        process.stderr.close()


class DockerBackend:
    evidence_kind = "docker_metadata_only"

    def __init__(self, value, profile_files, *, authorize=None, runner=command, stream_factory=ProcessStream):
        self.scope = validate_scope(value)
        self.scope_sha256 = sha(canonical(self.scope))
        exact(profile_files, ACTORS)
        self.profile_files = {role: Path(path) for role, path in profile_files.items()}
        self.authorize, self.runner, self.stream_factory = authorize, runner, stream_factory
        self.evidence_kind = "docker_metadata_only" if runner is command and stream_factory is ProcessStream else "simulated"
        self.prefix = ["docker", "--host", "unix:///var/run/docker.sock"]
        self._actor_streams, self._native_admissions = {}, {}
        self._runtime_starts = {}

    def _authorize(self, action):
        require(callable(self.authorize) and self.authorize(action, self.scope_sha256) is True, "EXECUTION_NOT_APPROVED")

    def _daemon(self, deadline):
        raw = self._success(["info", "--format", "{{.ID}}"], deadline).strip()
        require(0 < len(raw) <= 256 and sha(raw) == self.scope["daemon_sha256"], "DAEMON_IDENTITY")

    def _call(self, args, deadline):
        deadline.check()
        try:
            code, out, err = self.runner([*self.prefix, *args], deadline)
        except Exception:
            raise Invalid("DOCKER_TRANSPORT") from None
        require(type(code) is int and type(out) is bytes and type(err) is bytes and len(out) + len(err) <= MAX_COMMAND_BYTES, "DOCKER_OUTPUT")
        return code, out, err

    def _success(self, args, deadline):
        code, out, _ = self._call(args, deadline)
        require(code == 0, "DOCKER_FAILED")
        return out

    def _selector(self, kind, selector):
        require(kind in ("container", "volume", "image") and type(selector) is str, "DOCKER_SELECTOR")
        if kind == "image":
            require(selector in self.scope["images"].values(), "IMAGE_SELECTOR")
        elif kind == "volume":
            require(selector in {name(self.scope, role) for role in VOLUMES}, "VOLUME_SELECTOR")
        elif selector not in {name(self.scope, role) for role in ACTORS}:
            hex_id(selector)

    def inspect(self, kind, selector, deadline):
        self._selector(kind, selector)
        self._daemon(deadline)
        code, out, err = self._call([kind, "inspect", selector], deadline)
        self._daemon(deadline)
        if code:
            missing = {f"Error: No such {kind}: {selector}", f"Error response from daemon: No such {kind}: {selector}",
                f"Error: No such object: {selector}", f"Error response from daemon: No such object: {selector}"}
            if kind == "volume":
                missing.add(f"Error response from daemon: get {selector}: no such volume")
            if code == 1 and out.strip() in (b"", b"[]") and err.strip() in {value.encode() for value in missing}:
                return None
            raise Invalid("INSPECTION_FAILED")
        try:
            values = json.loads(out)
            require(type(values) is list and len(values) == 1 and type(values[0]) is dict, "INSPECTION_SHAPE")
            return values[0]
        except (ValueError, UnicodeError, RecursionError):
            raise Invalid("INSPECTION_SHAPE") from None

    def verify_platform(self, deadline):
        self._daemon(deadline)
        raw = self._success(["version", "--format", "{{json .Server}}"], deadline)
        try:
            value = json.loads(raw)
        except (ValueError, UnicodeError, RecursionError):
            raise Invalid("PLATFORM") from None
        require(type(value) is dict and value.get("Os") == "linux" and value.get("Arch") == "arm64", "PLATFORM")
        return {"os": "linux", "architecture": "arm64", "metadata_sha256": sha(canonical(value)), "runtime_feasibility_proven": False}

    def attachments(self, volume, deadline):
        self._selector("volume", volume)
        self._daemon(deadline)
        raw = self._success(["container", "ls", "--all", "--no-trunc", "--filter", "volume=" + volume, "--format", "{{.ID}}"], deadline)
        self._daemon(deadline)
        try:
            ids = raw.decode("ascii").splitlines()
        except UnicodeError:
            raise Invalid("ATTACHMENTS") from None
        require(len(ids) <= 64 and len(ids) == len(set(ids)), "ATTACHMENTS")
        for value in ids:
            hex_id(value)
        return sorted(ids)

    def _profile_file(self, role):
        path = self.profile_files[role]
        require(path.is_absolute(), "PROFILE_FILE")
        try:
            fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
            with os.fdopen(fd, "rb") as stream:
                info = os.fstat(stream.fileno())
                require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and 0 < info.st_size <= 32768, "PROFILE_FILE")
                raw = stream.read(32769)
        except OSError:
            raise Invalid("PROFILE_FILE") from None
        require(raw == canonical(profile(role)) and sha(raw) == self.scope["profiles"][role], "PROFILE_FILE_CONTENT")
        return str(path)

    def create_volume(self, expected, deadline):
        require(canonical(expected) == canonical(volume_spec(self.scope, expected.get("role"))), "VOLUME_SPEC")
        self._authorize("create_volume")
        self._daemon(deadline)
        args = ["volume", "create", "--driver", "local"]
        for key, value in sorted(TMPFS_OPTIONS.items()):
            args += ["--opt", key + "=" + value]
        for key, value in sorted(expected["labels"].items()):
            args += ["--label", key + "=" + value]
        self._authorize("create_volume")
        raw = self._success([*args, expected["name"]], deadline)
        require(raw.strip() == expected["name"].encode(), "VOLUME_CREATE_REPLY")

    def create_container(self, expected, deadline):
        role = expected.get("role")
        target_id = expected.get("pid_mode", "").removeprefix("container:") if role == "observer" else None
        require(canonical(expected) == canonical(container_spec(self.scope, role, target_id)), "CONTAINER_SPEC")
        self._authorize("create_container")
        self._daemon(deadline)
        if role == "observer":
            target = self.inspect("container", target_id, deadline)
            container_identity(target, container_spec(self.scope, "target"))
        image = self.inspect("image", expected["image"], deadline)
        require(type(image) is dict and image.get("Id") == expected["image"] and image.get("Os") == "linux" and image.get("Architecture") == "arm64", "IMAGE_IDENTITY")
        cfg = image.get("Config")
        require(type(cfg) is dict and empty_optional(cfg.get("OnBuild"), list) and empty_optional(cfg.get("Volumes"), dict), "IMAGE_DEFAULTS")
        disabled_healthcheck(cfg.get("Healthcheck"))
        env = [] if cfg.get("Env") is None else cfg["Env"]
        require(type(env) is list and sha(canonical(env)) == expected["environment_sha256"], "IMAGE_ENVIRONMENT")
        args = ["container", "create", "--name", expected["name"], "--pull", "never", "--network", "none",
            "--read-only", "--user", "999:999", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true",
            "--security-opt", "seccomp=" + self._profile_file(role), "--memory", str(expected["memory"]),
            "--memory-swap", str(expected["memory"]), "--cpus", "1", "--pids-limit", str(expected["pids_limit"]),
            "--ulimit", "core=0:0", "--restart", "no", "--init=false", "--ipc", "private", "--cgroupns", "private",
            "--log-driver", "none", "--no-healthcheck", "--workdir", "/", "--interactive", "--attach", "stdin", "--attach", "stdout", "--attach", "stderr"]
        if role == "observer":
            args += ["--pid", expected["pid_mode"]]
        for key, value in sorted(expected["labels"].items()):
            args += ["--label", key + "=" + value]
        for mount in expected["mounts"]:
            value = "type=volume,source=" + mount["name"] + ",target=" + mount["destination"] + ",volume-nocopy"
            args += ["--mount", value + (",readonly" if mount["read_only"] else "")]
        args += ["--entrypoint", expected["entrypoint"][0], expected["image"], *expected["command"]]
        self._authorize("create_container")
        raw = self._success(args, deadline)
        try:
            result = raw.decode("ascii").strip()
        except UnicodeError:
            raise Invalid("CONTAINER_CREATE_REPLY") from None
        hex_id(result)
        return result

    def open_actor(self, ref, expected, snapshot, budget, deadline):
        self._authorize("start")
        self._daemon(deadline)
        saved = validate_snapshot(snapshot)
        require(canonical(saved["scope"]) == canonical(self.scope) and set(saved["entries"]) == set((*VOLUMES, *ACTORS)) and
            all(row["reference"] is not None for row in saved["entries"].values()), "ACTIVATION_INVENTORY")
        role = expected.get("role")
        require(role in ACTORS and canonical(saved["entries"][role]["reference"]) == canonical(ref), "ACTIVATION_BINDING")
        registry = object.__new__(Registry)
        registry.scope, registry.entries = self.scope, saved["entries"]
        verify_attachments(registry, self, deadline)
        volumes = {role: saved["entries"][role]["reference"] for role in VOLUMES}
        target_id = expected.get("pid_mode", "").removeprefix("container:") if role == "observer" else None
        require(canonical(expected) == canonical(container_spec(self.scope, role, target_id)), "CONTAINER_SPEC")
        expected = registry.spec(role)
        ref = dict(saved["entries"][role]["reference"])
        for volume in VOLUMES:
            spec = volume_spec(self.scope, volume)
            actual = admit_volume(self.inspect("volume", spec["name"], deadline), spec)
            require(canonical(actual) == canonical(volumes.get(volume)), "VOLUME_REPLACED")
        if role == "observer":
            target = self.inspect("container", target_id, deadline)
            admit_container(target, container_spec(self.scope, "target"), volumes, phase="running")
        observed = self.inspect("container", ref["id"], deadline)
        admit_container(observed, expected, volumes, bound=ref)
        binding = {"case_id": self.scope["case_id"], "invocation_sha256": sha(canonical(self.scope)),
            "container_id": ref["id"], "image": expected["image"], "role": expected["role"]}
        require(ref["id"] not in self._actor_streams, "ACTOR_ALREADY_OPENED")
        self._authorize("start")
        holder = []
        def dispatch_guard():
            # No Docker calls here: this guard also runs immediately before the
            # write, after authorizer latency and the sender's nonblocking pump.
            peer_role = "observer" if role == "target" else "target"
            pre_dispatch = holder and holder[0].phase in ("READY", "ADMISSION_READY")
            try:
                deadline.check()
                if pre_dispatch:
                    admitted = self._native_admissions.get(role)
                    require(admitted is not None and admitted["stream"] is holder[0], "NATIVE_ADMISSION_REQUIRED")
                    peer = self._native_admissions.get(peer_role)
                    expected_phase = "ARMED" if role == "target" else "READY"
                    require(peer is not None, "NATIVE_OBSERVER_NOT_ARMED" if role == "target" else "NATIVE_TARGET_NOT_READY")
                    peer["stream"].require_quiet_live(expected_phase)
                    require(deadline.clock() < admitted["expires_at"], "NATIVE_ADMISSION_EXPIRED")
                    require(peer["stream"].deadline.clock() < peer["expires_at"], "NATIVE_ADMISSION_EXPIRED")
                deadline.check()
            except BaseException:
                if pre_dispatch:
                    self._native_admissions.pop(role, None)
                    self._native_admissions.pop(peer_role, None)
                raise

        def before_input():
            self._authorize("actor_input")
            if holder and not holder[0].sent and role in ("target", "observer"):
                current = None
                while current is None:
                    deadline.check()
                    current = self._runtime_binding(ref, expected, volumes, deadline, allow_created=True)
                    if current is None:
                        time.sleep(min(deadline.remaining(), 0.01))
                if ref["id"] in self._runtime_starts:
                    require(current == self._runtime_starts[ref["id"]], "NATIVE_ADMISSION_RESTART")
                else:
                    self._runtime_starts[ref["id"]] = current
            deadline.check()
            self._authorize("actor_input")
            dispatch_guard()
        stream = self.stream_factory([*self.prefix, "container", "start", "--attach", "--interactive", ref["id"]],
            binding, budget, deadline, env=clean_environment(), before_input=before_input, before_write=dispatch_guard)
        holder.append(stream)
        self._actor_streams[ref["id"]] = stream
        return stream

    def _runtime_binding(self, ref, expected, volumes, deadline, *, allow_created=False):
        value = self.inspect("container", ref["id"], deadline)
        phase = "created" if allow_created and type(value) is dict and type(value.get("State")) is dict and value["State"].get("Status") == "created" else "running"
        admit_container(value, expected, volumes, phase=phase, bound=ref)
        if phase == "created":
            return None
        integer(value.get("RestartCount"), 0, 0)
        started = timestamp(value["State"].get("StartedAt"))
        return {"reference": dict(ref), "host_pid": value["State"]["Pid"], "started_at": started, "restart_count": 0}

    def collect_native_admission(self, stream, ref, snapshot, preparation, context, deadline):
        """Read four bounded actor frames and join them to live owned metadata.

        The expected kernel/clock context is an independently trusted controller
        input, never inferred from the actor being admitted. Oracle admission,
        ptrace/slot capability proof and full trial evidence remain separate.
        """
        role = ref.get("role") if type(ref) is dict else None
        require(role in ("target", "observer"), "NATIVE_ADMISSION_ROLE")
        hex_id(ref.get("id"))
        require(self._actor_streams.get(ref.get("id")) is stream and stream is not None, "NATIVE_ADMISSION_STREAM")
        try:
            self._authorize("runtime_admission")
            require(role not in self._native_admissions, "NATIVE_ADMISSION_DUPLICATE")
            validate_preparation(preparation)
            runtime_admission.validate_context(context)
            saved = validate_snapshot(snapshot)
            require(canonical(saved["scope"]) == canonical(self.scope) and set(saved["entries"]) == set((*VOLUMES, *ACTORS)) and
                all(row["reference"] is not None for row in saved["entries"].values()), "ACTIVATION_INVENTORY")
            require(canonical(saved["entries"][role]["reference"]) == canonical(ref), "NATIVE_ADMISSION_BINDING")
            require(stream.sent and stream.phase is None and not stream.closed and stream.admission_frames == [], "NATIVE_ADMISSION_PHASE")
            require(stream.deadline is deadline and deadline.remaining() <= LIMITS["admission_ms"] / 1000, "NATIVE_ADMISSION_DEADLINE")
            binding = {"case_id": self.scope["case_id"], "invocation_sha256": self.scope_sha256,
                "container_id": ref["id"], "image": self.scope["images"][role], "role": role}
            require(stream.binding == binding, "NATIVE_ADMISSION_BINDING")
            for actor in ("target", "observer"):
                require(preparation["artifacts"][actor + "_profile"] == self.scope["profiles"][actor], "NATIVE_ADMISSION_PROFILE")
            registry = object.__new__(Registry)
            registry.scope, registry.entries = self.scope, saved["entries"]
            volumes = {item: saved["entries"][item]["reference"] for item in VOLUMES}
            def observed(actor):
                reference = saved["entries"][actor]["reference"]
                return self._runtime_binding(reference, registry.spec(actor), volumes, deadline)
            verify_attachments(registry, self, deadline)
            self.verify_platform(deadline)
            before = observed(role)
            require(self._runtime_starts.get(ref["id"]) == before, "NATIVE_ADMISSION_RESTART")
            target = None
            if role == "observer":
                target = self._native_admissions.get("target")
                require(target is not None and target["stream"].phase == "READY" and not target["stream"].closed, "NATIVE_ADMISSION_TARGET_REQUIRED")
                target["stream"].deadline.check()
                target["stream"].require_admission_ready()
                require(observed("target") == target["metadata"], "NATIVE_ADMISSION_TARGET_RESTART")
            for phase in runtime_admission.PHASES:
                stream.receive(phase)
            stream.receive("READY" if role == "target" else "ADMISSION_READY")
            stream.require_admission_ready()
            validated = runtime_admission.validate(stream.admission_frames, binding, preparation, context,
                target=target["validated"] if target else None)
            require(observed(role) == before, "NATIVE_ADMISSION_RESTART")
            if target is not None:
                target["stream"].deadline.check()
                target["stream"].require_admission_ready()
                require(observed("target") == target["metadata"], "NATIVE_ADMISSION_TARGET_RESTART")
            verify_attachments(registry, self, deadline)
            deadline.check()
            self._authorize("runtime_admission")
            # Metadata/attachment checks and the authorizer can take time or
            # observe a now-dead transport. Admission is decided after them.
            stream.require_admission_ready()
            if target is not None:
                target["stream"].require_admission_ready()
                target["stream"].deadline.check()
            deadline.check()
            self._native_admissions[role] = {"stream": stream, "validated": validated, "metadata": before, "expires_at": deadline.end}
            result = runtime_admission.summary(validated)
            result["metadata_sha256"] = sha(canonical(before))
            result["evidence_kind"] = "docker_bound_native_frames" if self.evidence_kind == "docker_metadata_only" else "simulated"
            require(len(canonical(result)) <= LIMITS["receipt_bytes"], "NATIVE_ADMISSION_RECEIPT_BOUND")
            return result
        except BaseException as error:
            self._native_admissions.pop(role, None)
            try:
                stream.close()
            except Exception:
                pass  # Remote cleanup is still an independent watchdog obligation.
            if not isinstance(error, Exception):
                raise
            raise Invalid("NATIVE_ADMISSION_REJECTED") from None

    def _owned(self, ref, deadline):
        require(type(ref) is dict and ref.get("kind") in ("container", "volume"), "RESOURCE_REFERENCE")
        if ref["kind"] == "container":
            exact(ref, ("kind", "role", "name", "id", "created", "spec_sha256"))
            require(ref["role"] in ACTORS and ref["name"] == name(self.scope, ref["role"]), "RESOURCE_REFERENCE")
            hex_id(ref["id"])
            observed = self.inspect("container", ref["id"], deadline)
            require(type(observed) is dict and observed.get("Id") == ref["id"] and observed.get("Created") == ref["created"] and
                observed.get("Name") == "/" + ref["name"] and observed.get("Image") == self.scope["images"][ref["role"]] and
                type(observed.get("Config")) is dict and observed["Config"].get("Labels") == labels(self.scope, ref["role"]), "CONTAINER_OWNER")
            return observed
        exact(ref, ("kind", "role", "name", "created", "mountpoint", "spec_sha256"))
        require(ref["role"] in VOLUMES, "RESOURCE_REFERENCE")
        spec = volume_spec(self.scope, ref["role"])
        actual = admit_volume(self.inspect("volume", spec["name"], deadline), spec)
        require(canonical(actual) == canonical(ref), "VOLUME_OWNER")
        return actual

    def kill(self, ref, deadline):
        require(ref.get("kind") == "container", "KILL_KIND")
        self._authorize("cleanup")
        self._daemon(deadline)
        self._owned(ref, deadline)
        self._success(["container", "kill", "--signal", "KILL", ref["id"]], deadline)

    def wait(self, ref, deadline):
        require(ref.get("kind") == "container", "WAIT_KIND")
        self._authorize("cleanup")
        self._daemon(deadline)
        self._owned(ref, deadline)
        raw = self._success(["container", "wait", ref["id"]], deadline)
        require(raw.strip().isdigit() and 0 <= int(raw.strip()) <= 255, "WAIT_REPLY")

    def remove(self, ref, deadline):
        self._authorize("cleanup")
        self._daemon(deadline)
        observed = self._owned(ref, deadline)
        if ref["kind"] == "container":
            require(type(observed.get("State")) is dict and observed["State"].get("Running") is False and
                type(observed["State"].get("Pid")) is int and observed["State"]["Pid"] == 0, "QUIESCENCE")
            selector = ref["id"]
        else:
            require(self.attachments(ref["name"], deadline) == [], "FOREIGN_ATTACHMENT")
            selector = ref["name"]
        self._success([ref["kind"], "rm", selector], deadline)


def verify_attachments(registry, backend, deadline):
    for volume in VOLUMES:
        ref = registry.entries.get(volume, {}).get("reference")
        if ref is None:
            continue
        current = admit_volume(backend.inspect("volume", ref["name"], deadline), registry.spec(volume))
        require(canonical(current) == canonical(ref), "VOLUME_REPLACED")
        expected = []
        for role in ACTORS:
            actor = registry.entries.get(role, {}).get("reference")
            if actor and any(mount["name"] == ref["name"] for mount in registry.spec(role)["mounts"]):
                expected.append(actor["id"])
        require(backend.attachments(ref["name"], deadline) == sorted(expected), "FOREIGN_ATTACHMENT")


def prepare_resources(registry, backend, deadline):
    """Metadata-only creation policy; a journal/watchdog acknowledgement precedes mutation."""
    require(not registry.failed and not registry.scope["case_id"].startswith("N."), "PREPARATION_STATE")
    try:
        backend.verify_platform(deadline)
        for role in (*VOLUMES, *ACTORS):
            deadline.check()
            kind = "volume" if role in VOLUMES else "container"
            require(backend.inspect(kind, name(registry.scope, role), deadline) is None, "RESOURCE_EXISTS")
            registry.candidate(role)
            spec = registry.spec(role)
            if kind == "volume":
                backend.create_volume(spec, deadline)
                observed = backend.inspect(kind, spec["name"], deadline)
            else:
                container_id = backend.create_container(spec, deadline)
                observed = backend.inspect(kind, container_id, deadline)
                require(type(observed) is dict and observed.get("Id") == container_id, "CREATION_ID")
            registry.bind(role, observed)
            verify_attachments(registry, backend, deadline)
        return {"metadata_admission": "PASS", "containers_started": 0, "runtime_process_admission": "pending", "execution_authorized": False}
    except BaseException:
        registry.failed = True
        raise
