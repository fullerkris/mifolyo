"""Read-only native-host discovery. A candidate is never runtime admission.

Only GET /info and GET /version at the fixed local Docker socket are allowed.
Collector metadata is inspected without reading credentials or issuing anchors.
"""
import argparse
import ctypes
import json
import os
import re
import stat

from contracts import Invalid, canonical, digest, exact, integer, platform, platform_digest, require, sha
from engine_metadata import ENGINE_SOCKET, MAX_BODY, read_engine_metadata
from linux_observation import LinuxReader
from runtime_admission import CONTEXT_HASHES, NAMESPACES
from streams import Deadline

INFO_FIELDS = {"id": "ID", "os": "OSType", "architecture": "Architecture", "version": "ServerVersion",
    "kernel": "KernelVersion", "cgroup_version": "CgroupVersion", "cgroup_driver": "CgroupDriver",
    "security_options": "SecurityOptions", "memory": "MemoryLimit", "swap": "SwapLimit",
    "pids": "PidsLimit", "cpu_period": "CpuCfsPeriod", "cpu_quota": "CpuCfsQuota"}
VERSION_FIELDS = {"os": "Os", "architecture": "Arch", "version": "Version", "kernel": "KernelVersion"}
PENDING = ("daemon_kernel_namespace_pairing", "collector_installation_and_pinned_ssh_admission",
    "runtime_image_and_actor_admission", "trial_dispatch_and_whole_trial_confinement",
    "native_breakpoint_timing_and_failure_calibration", "independent_cleanup_and_evidence_review")
COLLECTOR_PATHS = {"anchor": ("/etc/obs1/context-source.json", "file"),
    "code": ("/opt/obs1/context-source", "directory")}


def metadata(raw, fields, *, engine_response=False):
    require(type(raw) is bytes and 0 < len(raw) <= (MAX_BODY if engine_response else 16384), "PREFLIGHT_METADATA_BOUND")
    def pairs(rows):
        result = {}
        for key, value in rows:
            require(key not in result, "PREFLIGHT_DUPLICATE")
            result[key] = value
        return result
    def forbidden(_):
        raise Invalid("PREFLIGHT_NUMBER")
    try:
        value = json.loads(raw.decode("utf-8"), object_pairs_hook=pairs, parse_float=forbidden, parse_constant=forbidden)
    except (ValueError, UnicodeError, RecursionError):
        raise Invalid("PREFLIGHT_JSON") from None
    if engine_response:
        require(type(value) is dict and all(source in value for source in fields.values()), "PREFLIGHT_ENGINE_FIELDS")
        # The bounded API body may include proxy configuration; discard every
        # unselected field without logging, hashing or exporting that body.
        value = {key: value[source] for key, source in fields.items()}
        require(len(canonical(value)) <= 16384, "PREFLIGHT_METADATA_BOUND")
    exact(value, fields)
    return value


class HostProbe:
    def __init__(self, architecture):
        self.reader = LinuxReader(architecture)

    def native(self):
        uname = os.uname()
        return {"os": uname.sysname, "architecture": uname.machine, "pointer_bytes": ctypes.sizeof(ctypes.c_void_p)}

    def kernel(self):
        return self.reader.kernel()

    def interpreter(self):
        return self.reader.executable()

    def controllers(self):
        names = self.reader.read("/sys/fs/cgroup/cgroup.controllers", 1024).split()
        require(all(re.fullmatch(rb"[a-z_]+", name) for name in names) and len(names) == len(set(names)), "PREFLIGHT_CONTROLLERS")
        return {name: name.encode() in names for name in ("cpu", "memory", "pids")}

    def endpoint(self):
        try:
            socket = stat.S_ISSOCK(os.stat(ENGINE_SOCKET).st_mode)
        except FileNotFoundError:
            socket = False
        return {"socket_present": socket}

    def docker(self, operation, deadline):
        require(operation in ("info", "version"), "PREFLIGHT_OPERATION")
        fields = INFO_FIELDS if operation == "info" else VERSION_FIELDS
        raw = read_engine_metadata(operation, deadline)
        deadline.check()
        return metadata(raw, fields, engine_response=True)

    def collector(self):
        observed = {}
        for key, (path, kind) in COLLECTOR_PATHS.items():
            try:
                info = os.lstat(path)
                expected = stat.S_ISREG(info.st_mode) if kind == "file" else stat.S_ISDIR(info.st_mode)
                observed[key] = "PRESENT_UNVERIFIED" if expected else "UNEXPECTED_TYPE"
            except FileNotFoundError:
                observed[key] = "MISSING"
        return observed


def checked_kernel(value, architecture):
    exact(value, ("architecture", *CONTEXT_HASHES, "namespaces", "ns_device", "resolution_ns", "yama_scope"))
    require(value["architecture"] == architecture, "PREFLIGHT_ARCHITECTURE")
    for key in CONTEXT_HASHES:
        digest(value[key])
    exact(value["namespaces"], NAMESPACES)
    for number in (*value["namespaces"].values(), value["ns_device"]):
        integer(number, 1)
    integer(value["resolution_ns"], 1, 1000000)
    integer(value["yama_scope"], 0, 3)
    return value


def checked_docker(info, version, architecture):
    exact(info, INFO_FIELDS)
    exact(version, VERSION_FIELDS)
    selected = platform(architecture)
    require(info["os"] == version["os"] == "linux" and
        info["architecture"] in (architecture, selected["image_architecture"]) and
        version["architecture"] == selected["image_architecture"], "PREFLIGHT_DOCKER_PLATFORM")
    require(type(info["id"]) is str and re.fullmatch(r"[A-Za-z0-9:-]{1,256}", info["id"]) is not None, "PREFLIGHT_DAEMON_ID")
    require(type(info["version"]) is str and re.fullmatch(r"[0-9][A-Za-z0-9.+-]{0,63}", info["version"]) is not None and
        info["version"] == version["version"], "PREFLIGHT_DOCKER_VERSION")
    require(type(info["kernel"]) is str and 0 < len(info["kernel"].encode()) <= 256 and
        info["kernel"] == version["kernel"], "PREFLIGHT_DOCKER_KERNEL")
    require(type(info["cgroup_version"]) is str and info["cgroup_version"] in ("1", "2") and
        info["cgroup_driver"] in ("systemd", "cgroupfs", "none"), "PREFLIGHT_DOCKER_CGROUP")
    limits = {key: info[key] for key in ("memory", "swap", "pids", "cpu_period", "cpu_quota")}
    require(all(type(value) is bool for value in limits.values()), "PREFLIGHT_DOCKER_LIMITS")
    options = info["security_options"]
    require(type(options) is list and len(options) <= 16 and all(type(item) is str and
        re.fullmatch(r"name=(?:[a-z]+(?:,profile=[A-Za-z0-9_.-]+)?|no-new-privileges)", item) is not None for item in options), "PREFLIGHT_DOCKER_SECURITY")
    names = [option.split(",", 1)[0] for option in options]
    require(len(names) == len(set(names)), "PREFLIGHT_DOCKER_SECURITY")
    return {"daemon_sha256": sha(info["id"].encode()), "os": "linux", "architecture": selected["image_architecture"],
        "version": info["version"], "kernel_release_sha256": sha(info["kernel"].encode()),
        "cgroup_version": info["cgroup_version"], "cgroup_driver": info["cgroup_driver"], "limits": limits,
        "security_options_sha256": sha(canonical(options)), "seccomp": "name=seccomp,profile=builtin" in options,
        "cgroupns": "name=cgroupns" in options, "rootless": "name=rootless" in names, "userns": "name=userns" in names}


def inspect_host(architecture, *, probe=None, deadline=None):
    """Retain blockers and pending obligations; never generate an execution packet."""
    platform(architecture)
    simulated = probe is not None or deadline is not None
    probe = HostProbe(architecture) if probe is None else probe
    deadline = Deadline(10) if deadline is None else deadline
    observed, blockers = {}, []
    def collect(name, action):
        try:
            deadline.check()
            value = action()
            deadline.check()
            # Snapshot nested fixture/producer objects before a subsequent read.
            observed[name] = json.loads(canonical(value))
            return observed[name]
        except (OSError, ValueError, TypeError, RecursionError):
            blockers.append(name.upper() + "_UNAVAILABLE")
            observed[name] = None
            return None

    native = collect("native", probe.native)
    if native != {"os": "Linux", "architecture": architecture, "pointer_bytes": 8}:
        # Unrecognized OS/architecture strings are not exported as diagnostics.
        observed["native"] = None
        blockers.append("NATIVE_PLATFORM_MISMATCH")
    kernel = collect("kernel", lambda: checked_kernel(probe.kernel(), architecture))
    if kernel is not None and kernel["yama_scope"] != 0:
        blockers.append("YAMA_RESTRICTS_C0_SIBLING_PTRACE")
    collect("interpreter_sha256", lambda: digest(probe.interpreter()))
    def controllers():
        value = probe.controllers()
        exact(value, ("cpu", "memory", "pids"))
        require(all(type(item) is bool for item in value.values()), "PREFLIGHT_CONTROLLERS")
        return value
    control = collect("cgroup_controllers", controllers)
    if control is not None and not all(control.values()):
        blockers.append("CGROUP_CONTROLLERS_MISSING")
    def endpoint():
        value = probe.endpoint()
        exact(value, ("socket_present",))
        require(all(type(item) is bool for item in value.values()), "PREFLIGHT_ENDPOINT")
        return value
    local = collect("docker_endpoint", endpoint)
    if local is not None and not all(local.values()):
        blockers.append("LOCAL_DOCKER_ENDPOINT_MISSING")
    if local is not None and all(local.values()) and observed["native"] is not None:
        def docker():
            before = probe.docker("info", deadline)
            frozen = canonical(before)
            version = probe.docker("version", deadline)
            result = checked_docker(before, version, architecture)
            require(frozen == canonical(before) == canonical(probe.docker("info", deadline)), "PREFLIGHT_DAEMON_CHANGED")
            return result
        engine = collect("docker", docker)
        if engine is not None:
            if kernel is not None and engine["kernel_release_sha256"] != kernel["kernel_release_sha256"]:
                blockers.append("DOCKER_HOST_KERNEL_MISMATCH")
            if engine["cgroup_version"] != "2" or engine["cgroup_driver"] == "none" or not all(engine["limits"].values()):
                blockers.append("DOCKER_RESOURCE_LIMITS_UNSUPPORTED")
            if not engine["seccomp"] or not engine["cgroupns"]:
                blockers.append("DOCKER_CONFINEMENT_UNSUPPORTED")
            if engine["rootless"] or engine["userns"]:
                blockers.append("DOCKER_NAMESPACE_MAPPING_UNSUPPORTED")
    else:
        observed["docker"] = None
    def collector():
        value = probe.collector()
        exact(value, COLLECTOR_PATHS)
        require(all(item in ("PRESENT_UNVERIFIED", "MISSING", "UNEXPECTED_TYPE") for item in value.values()), "PREFLIGHT_COLLECTOR")
        return value
    installation = collect("collector", collector)
    if installation is not None and any(item != "PRESENT_UNVERIFIED" for item in installation.values()):
        blockers.append("COLLECTOR_NOT_PROVISIONED")
    if kernel is not None:
        after = collect("kernel_after", lambda: checked_kernel(probe.kernel(), architecture))
        if after != kernel:
            blockers.append("HOST_OBSERVATION_CHANGED")
    result = {"version": 1, "kind": "c0_native_host_preflight", "architecture": architecture,
        "platform_sha256": platform_digest(architecture), "backend": "docker-local", "read_only": True,
        "evidence_kind": "simulated" if simulated else "host_metadata_only", "observations": observed,
        "blockers": sorted(set(blockers)), "pending": list(PENDING),
        "status": "BLOCKED" if blockers else "HOST_CANDIDATE", "host_admitted": False,
        "runtime_evidence": False, "execution_authorized": False, "native_trials_run": 0}
    require(len(canonical(result)) <= 8192, "PREFLIGHT_REPORT_BOUND")
    return result


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--architecture", required=True, choices=("aarch64", "x86_64"))
    args = parser.parse_args(argv)
    result = inspect_host(args.architecture)
    print(canonical(result).decode(), end="")
    return 0 if result["status"] == "HOST_CANDIDATE" else 1


if __name__ == "__main__":
    raise SystemExit(main())
