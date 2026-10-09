"""Closed C0 resource blueprints, metadata admission and owned cleanup policy.

No command execution at import. Runtime image/process proof and execution
authorization belong to the outer controller, not to these metadata predicates.
"""
import copy
import datetime
import json
import re

from contracts import LIMITS, TRIALS, Invalid, canonical, digest, exact, integer, require, sha
from controller import role_spec
from profiles import profile

ACTORS = ("target", "oracle", "observer")
VOLUMES = ("control", "witness")
KEY = "io.mifolyo.obs1."
TMPFS_OPTIONS = {"type": "tmpfs", "device": "tmpfs",
    "o": "uid=999,gid=999,mode=0700,size=1048576,nodev,nosuid,noexec"}
COMMANDS = {"target": (["/target"], []), "observer": (["/observer"], []),
    "oracle": (["python3"], ["-I", "-B", "/oracle/worker.py"])}


def hex_id(value):
    return digest(value)


def timestamp(value):
    require(type(value) is str and re.fullmatch(r"20[0-9]{2}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,9})?Z", value) is not None, "CREATION_TIME")
    try:
        datetime.datetime.fromisoformat(value[:-1] + "+00:00")
    except ValueError:
        raise Invalid("CREATION_TIME") from None
    return value


def scope(case_id, invocation_id, images, environment_sha256, daemon_sha256):
    require(type(case_id) is str and case_id in TRIALS, "CASE")
    require(type(invocation_id) is str and re.fullmatch(r"[0-9a-f]{32}", invocation_id) is not None and invocation_id != "0" * 32, "INVOCATION")
    exact(images, ACTORS)
    exact(environment_sha256, ACTORS)
    digest(daemon_sha256)
    for role in ACTORS:
        require(type(images[role]) is str and images[role].startswith("sha256:"), "IMAGE")
        digest(images[role][7:])
        digest(environment_sha256[role])
        if role != "oracle":
            require(environment_sha256[role] == sha(canonical([])), "NATIVE_ENVIRONMENT")
    require(len(set(images.values())) == len(ACTORS), "IMAGE_ROLES")
    return {"version": 1, "case_id": case_id, "invocation_id": invocation_id,
        "images": dict(images), "environment_sha256": dict(environment_sha256),
        "daemon_sha256": daemon_sha256,
        "profiles": {role: sha(canonical(profile(role))) for role in ACTORS},
        "namespace_policy": {"target": "private", "oracle": "private", "observer": "owned_target_only"},
        "execution_authorized": False, "runtime_process_admission": "pending"}


def validate_scope(value):
    exact(value, ("version", "case_id", "invocation_id", "images", "environment_sha256", "daemon_sha256", "profiles", "namespace_policy", "execution_authorized", "runtime_process_admission"))
    require(canonical(value) == canonical(scope(value["case_id"], value["invocation_id"], value["images"], value["environment_sha256"], value["daemon_sha256"])), "RESOURCE_SCOPE")
    return copy.deepcopy(value)


def labels(value, role):
    require(role in (*ACTORS, *VOLUMES), "ROLE")
    return {KEY + "invocation": value["invocation_id"], KEY + "case": value["case_id"],
        KEY + "scope": sha(canonical(value)), KEY + "role": role}


def name(value, role):
    require(role in (*ACTORS, *VOLUMES), "ROLE")
    return "obs1-" + value["invocation_id"] + "-" + role


def volume_spec(value, role):
    validate_scope(value)
    require(role in VOLUMES, "VOLUME_ROLE")
    return {"name": name(value, role), "role": role, "labels": labels(value, role),
        "driver": "local", "scope": "local", "options": dict(TMPFS_OPTIONS)}


def container_spec(value, role, target_id=None):
    validate_scope(value)
    require(role in ACTORS, "ACTOR_ROLE")
    if role == "observer":
        hex_id(target_id)
    else:
        require(target_id is None, "UNEXPECTED_PID_TARGET")
    base = role_spec(role, value["images"][role], value["profiles"][role])
    entry, command = COMMANDS[role]
    mounts = [] if role == "observer" else [
        {"name": name(value, "control"), "destination": "/control", "read_only": role == "oracle"},
        {"name": name(value, "witness"), "destination": "/witness", "read_only": role == "oracle"}]
    return {"role": role, "name": name(value, role), "image": base["image"], "labels": labels(value, role),
        "entrypoint": entry, "command": command, "environment_sha256": value["environment_sha256"][role],
        "profile_sha256": value["profiles"][role], "memory": base["memory"], "pids_limit": base["pids_limit"],
        "pid_mode": "container:" + target_id if role == "observer" else "", "mounts": mounts}


def admit_volume(observed, expected):
    require(type(observed) is dict, "VOLUME_INSPECTION")
    require(observed.get("Name") == expected["name"] and observed.get("Labels") == expected["labels"], "VOLUME_OWNER")
    require(observed.get("Driver") == "local" and observed.get("Scope") == "local" and observed.get("Options") == TMPFS_OPTIONS, "VOLUME_DRIVER")
    timestamp(observed.get("CreatedAt"))
    path = observed.get("Mountpoint")
    require(type(path) is str and path.startswith("/") and len(path) <= 1024 and not any(char in path for char in "\x00\n\r"), "VOLUME_MOUNTPOINT")
    return {"kind": "volume", "role": expected["role"], "name": expected["name"],
        "created": observed["CreatedAt"], "mountpoint": path, "spec_sha256": sha(canonical(expected))}


def _profile_options(values, role):
    require(type(values) is list and len(values) == 2 and all(type(value) is str and len(value) <= 32768 for value in values), "SECURITY_OPTIONS")
    nnp = [value for value in values if value in ("no-new-privileges", "no-new-privileges:true")]
    seccomp = [value[8:] for value in values if value.startswith("seccomp=")]
    require(len(nnp) == len(seccomp) == 1, "SECURITY_OPTIONS")

    def pairs(items):
        result = {}
        for key, value in items:
            require(key not in result, "PROFILE_DUPLICATE")
            result[key] = value
        return result

    try:
        parsed = json.loads(seccomp[0], object_pairs_hook=pairs)
        require(canonical(parsed) == canonical(profile(role)), "PROFILE_CONTENT")
    except (ValueError, TypeError, UnicodeError, RecursionError):
        raise Invalid("PROFILE_CONTENT") from None


def container_identity(observed, expected, bound=None):
    require(type(observed) is dict and type(observed.get("Config")) is dict, "CONTAINER_INSPECTION")
    hex_id(observed.get("Id"))
    timestamp(observed.get("Created"))
    require(observed.get("Name") == "/" + expected["name"] and observed.get("Image") == expected["image"] and
        observed["Config"].get("Labels") == expected["labels"], "CONTAINER_OWNER")
    ref = {"kind": "container", "role": expected["role"], "name": expected["name"], "id": observed["Id"],
        "created": observed["Created"], "spec_sha256": sha(canonical(expected))}
    if bound is not None:
        require(canonical(ref) == canonical(bound), "CONTAINER_REPLACED")
    return ref


def empty_optional(value, expected_type):
    return value is None or type(value) is expected_type and len(value) == 0


def disabled_healthcheck(value):
    if value is None:
        return
    require(type(value) is dict and set(value) <= {"Test", "Interval", "Timeout", "StartPeriod", "StartInterval", "Retries"} and value.get("Test") == ["NONE"], "HEALTHCHECK")
    for key in set(value) - {"Test"}:
        integer(value[key], 0, 0)


def admit_container(observed, expected, volumes, *, phase="created", bound=None):
    require(type(observed) is dict and type(observed.get("Config")) is dict and type(observed.get("HostConfig")) is dict and type(observed.get("State")) is dict, "CONTAINER_INSPECTION")
    cfg, host, state = observed["Config"], observed["HostConfig"], observed["State"]
    ref = container_identity(observed, expected, bound)
    require(cfg.get("Labels") == expected["labels"] and cfg.get("Image") == expected["image"] and cfg.get("User") == "999:999", "CONTAINER_OWNER")
    cmd = [] if cfg.get("Cmd") is None else cfg["Cmd"]
    require(cfg.get("Entrypoint") == expected["entrypoint"] and type(cmd) is list and cmd == expected["command"] and cfg.get("WorkingDir") == "/", "CONTAINER_COMMAND")
    env = [] if cfg.get("Env") is None else cfg["Env"]
    require(type(env) is list and len(env) <= 8 and all(type(item) is str and len(item) <= 2048 and "=" in item and "\x00" not in item for item in env), "ENVIRONMENT")
    require(sha(canonical(env)) == expected["environment_sha256"], "ENVIRONMENT")
    require(cfg.get("Tty") is False and cfg.get("OpenStdin") is True and cfg.get("AttachStdin") is True and cfg.get("AttachStdout") is True and cfg.get("AttachStderr") is True, "CONTAINER_STREAMS")
    require(empty_optional(cfg.get("ExposedPorts"), dict) and empty_optional(cfg.get("Volumes"), dict), "IMAGE_DEFAULTS")
    disabled_healthcheck(cfg.get("Healthcheck"))
    require(state.get("Health") is None, "HEALTHCHECK_STATE")
    require(host.get("NetworkMode") == "none" and host.get("ReadonlyRootfs") is True and host.get("Privileged") is False, "ISOLATION")
    require(host.get("PidMode") == expected["pid_mode"] and host.get("IpcMode") == "private" and host.get("CgroupnsMode") == "private", "NAMESPACE")
    require(host.get("UsernsMode") == "" and host.get("UTSMode") == "" and (host.get("Init") is None or host["Init"] is False), "NAMESPACE")
    require(host.get("CapDrop") == ["ALL"] and empty_optional(host.get("CapAdd"), list), "CAPABILITIES")
    for key, number in (("Memory", expected["memory"]), ("MemorySwap", expected["memory"]), ("NanoCpus", 1000000000), ("PidsLimit", expected["pids_limit"])):
        integer(host.get(key), number, number)
    exact(host.get("RestartPolicy"), ("Name", "MaximumRetryCount"))
    integer(host["RestartPolicy"]["MaximumRetryCount"], 0, 0)
    require(host["RestartPolicy"]["Name"] == "no" and host.get("AutoRemove") is False, "RESTART_POLICY")
    require(host.get("LogConfig") == {"Type": "none", "Config": {}}, "LOGGING")
    require(type(host.get("Ulimits")) is list and len(host["Ulimits"]) == 1, "CORE_LIMIT")
    core = host["Ulimits"][0]
    exact(core, ("Name", "Hard", "Soft"))
    require(core["Name"] == "core", "CORE_LIMIT")
    integer(core["Hard"], 0, 0)
    integer(core["Soft"], 0, 0)
    for key in ("Binds", "VolumesFrom", "Devices", "DeviceRequests", "Links", "Dns", "DnsSearch", "DnsOptions", "ExtraHosts", "GroupAdd"):
        require(empty_optional(host.get(key), list), "EXTRA_HOST_AUTHORITY")
    for key in ("PortBindings", "Tmpfs"):
        require(empty_optional(host.get(key), dict), "EXTRA_HOST_AUTHORITY")
    require(host.get("PublishAllPorts") is False and host.get("Runtime") in ("", "runc"), "HOST_AUTHORITY")
    _profile_options(host.get("SecurityOpt"), expected["role"])
    declared_mounts = [] if host.get("Mounts") is None else host["Mounts"]
    require(type(declared_mounts) is list and len(declared_mounts) == len(expected["mounts"]), "MOUNT_OPTIONS")
    declared = {row.get("Target"): row for row in declared_mounts if type(row) is dict}
    require(len(declared) == len(declared_mounts), "MOUNT_OPTIONS")
    for mount in expected["mounts"]:
        row = declared.get(mount["destination"], {})
        require(set(row) <= {"Type", "Source", "Target", "ReadOnly", "Consistency", "VolumeOptions", "BindOptions", "TmpfsOptions", "ClusterOptions", "ImageOptions"}, "MOUNT_OPTIONS")
        require(row.get("Type") == "volume" and row.get("Source") == mount["name"] and
            row.get("ReadOnly", False) is mount["read_only"] and row.get("Consistency", "") == "", "MOUNT_OPTIONS")
        require(all(row.get(key) is None for key in ("BindOptions", "TmpfsOptions", "ClusterOptions", "ImageOptions")), "MOUNT_OPTIONS")
        options = row.get("VolumeOptions")
        require(type(options) is dict and set(options) <= {"NoCopy", "Labels", "DriverConfig", "Subpath"} and
            options.get("NoCopy") is True and empty_optional(options.get("Labels"), dict) and
            options.get("DriverConfig") is None and options.get("Subpath", "") == "", "MOUNT_OPTIONS")
    mounts = observed.get("Mounts")
    require(type(mounts) is list and len(mounts) == len(expected["mounts"]), "MOUNTS")
    actual = {row.get("Destination"): row for row in mounts if type(row) is dict}
    require(len(actual) == len(mounts), "MOUNTS")
    for mount in expected["mounts"]:
        row = actual.get(mount["destination"], {})
        volume_ref = next((item for item in volumes.values() if type(item) is dict and item.get("name") == mount["name"]), None)
        require(volume_ref is not None and row.get("Type") == "volume" and row.get("Name") == mount["name"] and row.get("Driver") == "local", "MOUNTS")
        require(row.get("Source") == volume_ref["mountpoint"] and row.get("RW") is (not mount["read_only"]), "MOUNT_AUTHORITY")
    require(state.get("Paused") is False and state.get("Restarting") is False and state.get("Dead") is False, "CONTAINER_STATE")
    integer(state.get("Pid"), 0)
    integer(state.get("ExitCode"), 0, 255)
    require(type(state.get("Running")) is bool and type(state.get("OOMKilled")) is bool, "CONTAINER_STATE")
    if phase == "created":
        require(state.get("Status") == "created" and state["Pid"] == 0 and state["Running"] is False and state["OOMKilled"] is False, "NOT_STOPPED")
    elif phase == "running":
        require(state.get("Status") == "running" and state["Pid"] > 0 and state["Running"] is True and state["OOMKilled"] is False, "NOT_RUNNING")
    else:
        require(phase == "cleanup", "PHASE")
    return ref


class Registry:
    """Journal a candidate before mutation; bind returned identity before start."""
    def __init__(self, value, publish):
        self.scope = validate_scope(value)
        require(callable(publish), "JOURNAL_REQUIRED")
        self.publish, self.entries, self.generation, self.failed = publish, {}, 0, False
        self._seal()

    def _seal(self):
        try:
            self.publish(self.snapshot())
        except BaseException:
            self.failed = True
            raise

    def snapshot(self):
        return {"version": 1, "scope": copy.deepcopy(self.scope), "generation": self.generation, "entries": copy.deepcopy(self.entries)}

    def candidate(self, role):
        require(not self.failed and role in (*VOLUMES, *ACTORS) and role not in self.entries, "RESOURCE_DUPLICATE")
        if role in ACTORS:
            require(all(self.entries.get(item, {}).get("reference") for item in VOLUMES), "VOLUMES_NOT_BOUND")
        if role == "observer":
            require(self.entries.get("target", {}).get("reference"), "TARGET_NOT_BOUND")
        self.entries[role] = {"reference": None}
        self.generation += 1
        self._seal()

    def spec(self, role):
        if role in VOLUMES:
            return volume_spec(self.scope, role)
        target = self.entries.get("target", {}).get("reference")
        return container_spec(self.scope, role, target["id"] if role == "observer" and target else None)

    def bind(self, role, observed):
        require(not self.failed and role in self.entries and self.entries[role]["reference"] is None, "RESOURCE_ALREADY_BOUND")
        spec = self.spec(role)
        if role in VOLUMES:
            ref = admit_volume(observed, spec)
        else:
            volumes = {name: self.entries[name]["reference"] for name in VOLUMES}
            ref = admit_container(observed, spec, volumes)
            require(all(item["reference"] is None or item["reference"].get("id") != ref["id"] for item in self.entries.values()), "REUSED_CONTAINER_ID")
        self.entries[role]["reference"] = ref
        self.generation += 1
        self._seal()
        return copy.deepcopy(ref)


def validate_snapshot(value):
    exact(value, ("version", "scope", "generation", "entries"))
    integer(value["version"], 1, 1)
    integer(value["generation"], 0, 10)
    validate_scope(value["scope"])
    require(type(value["entries"]) is dict and set(value["entries"]) <= set((*VOLUMES, *ACTORS)), "RESOURCE_INVENTORY")
    bound = 0
    ids = set()
    for role, entry in value["entries"].items():
        exact(entry, ("reference",))
        if role in ACTORS:
            require(all(value["entries"].get(key, {}).get("reference") for key in VOLUMES), "VOLUMES_NOT_BOUND")
        if role == "observer":
            require(value["entries"].get("target", {}).get("reference"), "TARGET_NOT_BOUND")
        ref = entry["reference"]
        if ref is None:
            continue
        bound += 1
        if role in VOLUMES:
            exact(ref, ("kind", "role", "name", "created", "mountpoint", "spec_sha256"))
            require(ref["kind"] == "volume" and type(ref["mountpoint"]) is str and ref["mountpoint"].startswith("/") and len(ref["mountpoint"]) <= 1024, "VOLUME_REFERENCE")
            expected = volume_spec(value["scope"], role)
        else:
            exact(ref, ("kind", "role", "name", "id", "created", "spec_sha256"))
            require(ref["kind"] == "container" and all(value["entries"].get(key, {}).get("reference") for key in VOLUMES), "CONTAINER_REFERENCE")
            hex_id(ref["id"])
            require(ref["id"] not in ids, "REUSED_CONTAINER_ID")
            ids.add(ref["id"])
            target = value["entries"].get("target", {}).get("reference")
            expected = container_spec(value["scope"], role, target["id"] if role == "observer" and target else None)
        require(ref["role"] == role and ref["name"] == expected["name"] and ref["spec_sha256"] == sha(canonical(expected)), "RESOURCE_BINDING")
        timestamp(ref["created"])
    require(value["generation"] == len(value["entries"]) + bound, "RESOURCE_GENERATION")
    require(len(canonical(value)) <= 32768, "RESOURCE_SNAPSHOT_BOUND")
    return copy.deepcopy(value)


def cleanup(snapshot, backend, deadline):
    """Continue independently safe cleanup; inspect errors are never absence."""
    require(getattr(backend, "evidence_kind", None) in ("simulated", "docker_metadata_only"), "BACKEND_EVIDENCE_KIND")
    saved = validate_snapshot(snapshot)
    registry = object.__new__(Registry)
    registry.scope, registry.entries = saved["scope"], saved["entries"]
    results = []
    volumes = {role: saved["entries"].get(role, {}).get("reference") for role in VOLUMES}
    for role in ("observer", "oracle", "target", "witness", "control"):
        if role not in saved["entries"]:
            continue
        entry, spec = saved["entries"][role], registry.spec(role)
        kind = "volume" if role in VOLUMES else "container"
        ref = entry["reference"]
        # A lost create reply may complete at the daemon after an absence check.
        # Dispose of currently observed owned resources, but never certify a
        # candidate-only operation settled merely because its name is absent.
        result = {"role": role, "kind": kind, "name": spec["name"], "absent": False,
            "creation_settled": ref is not None, "result": "unproven"}
        try:
            deadline.check()
            selector = ref["id"] if kind == "container" and ref else spec["name"]
            observed = backend.inspect(kind, selector, deadline)
            named = backend.inspect(kind, spec["name"], deadline)
            if observed is None and named is None:
                result.update(absent=True, result="already_absent")
            else:
                require(observed is not None and named is not None, "RESOURCE_REPLACED")
                if kind == "container":
                    require(observed["Id"] == named["Id"], "RESOURCE_REPLACED")
                    current = container_identity(observed, spec, ref)
                    require(type(observed.get("State")) is dict and type(observed["State"].get("Running")) is bool, "CONTAINER_STATE")
                    if observed["State"]["Running"]:
                        backend.kill(current, deadline)
                    backend.wait(current, deadline)
                    stopped = backend.inspect("container", current["id"], deadline)
                    container_identity(stopped, spec, current)
                    require(type(stopped.get("State")) is dict, "CONTAINER_STATE")
                    integer(stopped["State"].get("Pid"), 0, 0)
                    require(stopped["State"]["Running"] is False and stopped["State"]["Pid"] == 0, "QUIESCENCE")
                    backend.remove(current, deadline)
                    require(backend.inspect("container", current["id"], deadline) is None, "CONTAINER_REMAINS")
                else:
                    current = admit_volume(observed, spec)
                    require(ref is None or canonical(current) == canonical(ref), "VOLUME_REPLACED")
                    require(backend.attachments(spec["name"], deadline) == [], "FOREIGN_ATTACHMENT")
                    backend.remove(current, deadline)
                require(backend.inspect(kind, spec["name"], deadline) is None, "RESOURCE_REMAINS")
                result.update(absent=True, result="removed_and_absent")
        except Exception:
            # Raw Docker/transport/metadata values are deliberately not retained.
            result["result"] = "cleanup_not_proven"
        results.append(result)
    return {"scope_sha256": sha(canonical(saved["scope"])), "resources": results, "evidence_kind": backend.evidence_kind,
        "complete": len(results) == len(saved["entries"]) and all(row["absent"] and row["creation_settled"] for row in results),
        "execution_authorized": False, "runtime_process_admission": "not_established"}
