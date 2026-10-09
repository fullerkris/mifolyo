"""Bounded native admission exchange; validation never grants execution authority.

The caller supplies independently trusted kernel/clock expectations and admitted
container/stream bindings. Native frames alone are not an authority or an oracle.
"""
import copy

from contracts import DOMAIN, LIMITS, TRIALS, canonical, clock_ns, decode, digest, exact, integer, require, sha, validate_preparation

PHASES = ("IDENTITY", "CONFINEMENT", "RESOURCES", "CLOCK")
NAMESPACES = ("pid_ns", "user_ns", "time_ns", "cgroup_ns", "mnt_ns", "net_ns", "ipc_ns")
COMMON = ("case_id", "invocation_sha256", "phase", "role", "version")
FIELDS = {
    "IDENTITY": ("boot_sha256", "executable_sha256", "gid_map_sha256", "pid", "start_ticks", "tid", "uid_map_sha256", "ns_device", *NAMESPACES),
    "CONFINEMENT": ("caps", "core_hard", "core_soft", "dumpable", "gids", "groups", "nnp", "seccomp", "seccomp_filters", "tasks", "threads", "tracer_pid", "uids"),
    "RESOURCES": ("cgroup_sha256", "cpu_period", "cpu_quota", "memory_current", "memory_max", "memory_peak", "pids_current", "pids_max", "processes", "swap_max"),
    "CLOCK": ("kernel_release_sha256", "lsm_sha256", "monotonic_ns", "resolution_ns", "time_offsets_sha256", "yama_scope")}
CONTEXT_HASHES = ("kernel_boot_sha256", "kernel_release_sha256", "uid_map_sha256", "gid_map_sha256", "lsm_sha256", "time_offsets_sha256")
HOST_NAMESPACES = ("host_pid_ns", "host_mnt_ns", "host_net_ns", "host_ipc_ns", "host_cgroup_ns")


def _array(value, count, low, high):
    require(type(value) is list and len(value) == count, "ADMISSION_ARRAY")
    for item in value:
        integer(item, low, high)


def native_frame(raw, phase):
    require(phase in PHASES, "ADMISSION_PHASE")
    value = decode(raw, LIMITS["receipt_bytes"])
    exact(value, (*COMMON, *FIELDS[phase]))
    integer(value["version"], 1, 1)
    require(value["phase"] == phase and value["role"] in ("target", "observer"), "ADMISSION_PHASE")
    require(type(value["case_id"]) is str and value["case_id"] in TRIALS and not value["case_id"].startswith("N."), "ADMISSION_CASE")
    digest(value["invocation_sha256"])
    if phase == "IDENTITY":
        for name in ("boot_sha256", "executable_sha256", "uid_map_sha256", "gid_map_sha256"):
            digest(value[name])
        for name in (*NAMESPACES, "start_ticks", "pid", "tid"):
            integer(value[name], 1)
        integer(value["ns_device"], 1)
        integer(value["pid"], 1 if value["role"] == "target" else 2, 1 if value["role"] == "target" else 4194304)
        require(value["tid"] == value["pid"], "ADMISSION_MAIN_THREAD")
    elif phase == "CONFINEMENT":
        _array(value["caps"], 5, 0, 0)
        _array(value["uids"], 4, 999, 999)
        _array(value["gids"], 4, 999, 999)
        require(type(value["groups"]) is list and len(value["groups"]) <= 1, "ADMISSION_GROUPS")
        for item in value["groups"]:
            integer(item, 999, 999)
        for name in ("core_soft", "core_hard", "tracer_pid"):
            integer(value[name], 0, 0)
        for name in ("dumpable", "nnp", "threads"):
            integer(value[name], 1, 1)
        integer(value["seccomp"], 2, 2)
        integer(value["seccomp_filters"], 1, 16)
        _array(value["tasks"], 1, 1, 4194304)
    elif phase == "RESOURCES":
        digest(value["cgroup_sha256"])
        require(value["cgroup_sha256"] == sha(b"0::/\n"), "ADMISSION_CGROUP_ROOT")
        integer(value["memory_max"], LIMITS[value["role"] + "_memory"], LIMITS[value["role"] + "_memory"])
        integer(value["swap_max"], 0, 0)
        integer(value["memory_current"], 1, value["memory_max"])
        integer(value["memory_peak"], value["memory_current"], value["memory_max"])
        integer(value["pids_max"], LIMITS[value["role"] + "_pids"], LIMITS[value["role"] + "_pids"])
        integer(value["pids_current"], 1, 1)
        integer(value["cpu_period"], 1000, 1000000)
        integer(value["cpu_quota"], value["cpu_period"], value["cpu_period"])
        _array(value["processes"], 1, 1, 4194304)
    else:
        for name in ("kernel_release_sha256", "lsm_sha256", "time_offsets_sha256"):
            digest(value[name])
        clock_ns(value["monotonic_ns"], 1)
        integer(value["resolution_ns"], 1, 1000000)
        integer(value["yama_scope"], 0, 3)
    return value


def validate_context(value):
    exact(value, (*CONTEXT_HASHES, *HOST_NAMESPACES, "user_ns", "time_ns", "ns_device", "resolution_ns", "yama_scope", "seccomp_filters", "not_before_ns", "not_after_ns"))
    for name in CONTEXT_HASHES:
        digest(value[name])
    for name in (*HOST_NAMESPACES, "user_ns", "time_ns", "ns_device"):
        integer(value[name], 1)
    integer(value["resolution_ns"], 1, 1000000)
    integer(value["yama_scope"], 0, 3)
    exact(value["seccomp_filters"], ("target", "observer"))
    integer(value["seccomp_filters"]["target"], 1, 15)
    integer(value["seccomp_filters"]["observer"], value["seccomp_filters"]["target"] + 1, value["seccomp_filters"]["target"] + 1)
    begin = clock_ns(value["not_before_ns"], 1)
    end = clock_ns(value["not_after_ns"], begin + 1)
    require(end - begin <= LIMITS["admission_ms"] * 1000000, "ADMISSION_WINDOW")
    return copy.deepcopy(value)


def validate(raw_frames, binding, preparation, context, *, target=None):
    """Closed offline validator. Context and channel provenance stay caller obligations."""
    validate_preparation(preparation)
    context = validate_context(context)
    exact(binding, ("case_id", "invocation_sha256", "container_id", "image", "role"))
    require(binding["role"] in ("target", "observer"), "ADMISSION_ROLE")
    digest(binding["container_id"])
    digest(binding["invocation_sha256"])
    require(type(binding["image"]) is str and binding["image"].startswith("sha256:"), "ADMISSION_IMAGE")
    digest(binding["image"][7:])
    require(type(raw_frames) is list and len(raw_frames) == 4, "ADMISSION_FRAMES")
    frames = [native_frame(raw, phase) for raw, phase in zip(raw_frames, PHASES)]
    for frame in frames:
        require(all(frame[key] == binding[key] for key in ("case_id", "invocation_sha256", "role")), "ADMISSION_BINDING")
    identity, security, resources, clock = frames
    role = binding["role"]
    require(identity["executable_sha256"] == preparation["artifacts"][role + "_elf"], "ADMISSION_EXECUTABLE")
    for name, expected in (("boot_sha256", "kernel_boot_sha256"), ("uid_map_sha256", "uid_map_sha256"), ("gid_map_sha256", "gid_map_sha256")):
        require(identity[name] == context[expected], "ADMISSION_KERNEL_IDENTITY")
    require(identity["user_ns"] == context["user_ns"] and identity["time_ns"] == context["time_ns"], "ADMISSION_NAMESPACE")
    require(identity["ns_device"] == context["ns_device"], "ADMISSION_NAMESPACE")
    for name in HOST_NAMESPACES:
        require(identity[name.removeprefix("host_")] != context[name], "ADMISSION_HOST_NAMESPACE")
    require(security["tasks"] == resources["processes"] == [identity["pid"]], "ADMISSION_TASK_INVENTORY")
    require(security["seccomp_filters"] == context["seccomp_filters"][role], "ADMISSION_FILTER_COUNT")
    for name in ("kernel_release_sha256", "lsm_sha256", "time_offsets_sha256", "resolution_ns", "yama_scope"):
        require(clock[name] == context[name], "ADMISSION_KERNEL_CONTEXT")
    require(context["not_before_ns"] <= clock["monotonic_ns"] <= context["not_after_ns"], "ADMISSION_STALE")
    if role == "target":
        require(target is None, "ADMISSION_UNEXPECTED_PEER")
    else:
        require(type(target) is dict, "ADMISSION_TARGET_REQUIRED")
        exact(target, ("binding", "frames", "context_sha256", "preparation_sha256", "identity_sha256", "observation_sha256", "execution_authorized", "runtime_evidence"))
        # Revalidate the peer's original frames, rather than accepting digest-shaped assertions.
        peer = validate(target["frames"], target["binding"], preparation, context)
        require(target["execution_authorized"] is False and target["runtime_evidence"] is False and
            all(target[key] == peer[key] for key in ("context_sha256", "preparation_sha256", "identity_sha256", "observation_sha256")), "ADMISSION_PEER_RECEIPT")
        require(peer["binding"]["role"] == "target" and peer["binding"]["case_id"] == binding["case_id"] and
            peer["binding"]["invocation_sha256"] == binding["invocation_sha256"] and peer["binding"]["container_id"] != binding["container_id"], "ADMISSION_PEER_BINDING")
        peer_identity = native_frame(peer["frames"][0], "IDENTITY")
        require(clock["monotonic_ns"] >= native_frame(peer["frames"][3], "CLOCK")["monotonic_ns"], "ADMISSION_PEER_CLOCK")
        for name in ("pid_ns", "user_ns", "time_ns", "ns_device", "boot_sha256", "uid_map_sha256", "gid_map_sha256"):
            require(identity[name] == peer_identity[name], "ADMISSION_PEER_NAMESPACE")
        for name in ("cgroup_ns", "mnt_ns", "net_ns", "ipc_ns"):
            require(identity[name] != peer_identity[name], "ADMISSION_PRIVATE_NAMESPACE")
    # Raw frames are returned privately for revalidation. Export only the compact summary below.
    return {"binding": dict(binding), "frames": list(raw_frames), "context_sha256": sha(canonical(context)),
        "preparation_sha256": sha(canonical(preparation)), "identity_sha256": sha(canonical(identity)),
        "observation_sha256": sha(b"".join(raw_frames)), "execution_authorized": False, "runtime_evidence": False}


def summary(value):
    """A digest-only preparation result, not a transferable admission capability."""
    exact(value, ("binding", "frames", "context_sha256", "preparation_sha256", "identity_sha256", "observation_sha256", "execution_authorized", "runtime_evidence"))
    exact(value["binding"], ("case_id", "invocation_sha256", "container_id", "image", "role"))
    require(type(value["frames"]) is list and len(value["frames"]) == 4, "ADMISSION_FRAMES")
    frames = [native_frame(raw, phase) for raw, phase in zip(value["frames"], PHASES)]
    require(all(all(frame[key] == value["binding"][key] for key in ("case_id", "invocation_sha256", "role")) for frame in frames), "ADMISSION_BINDING")
    for key in ("context_sha256", "preparation_sha256", "identity_sha256", "observation_sha256"):
        digest(value[key])
    digest(value["binding"]["container_id"])
    require(value["identity_sha256"] == sha(canonical(frames[0])) and value["observation_sha256"] == sha(b"".join(value["frames"])), "ADMISSION_DIGEST")
    require(value["execution_authorized"] is False and value["runtime_evidence"] is False, "ADMISSION_AUTHORITY")
    result = {"domain": DOMAIN, "version": 1, "phase": "native_admission_validated", "role": value["binding"]["role"],
        "case_id": value["binding"]["case_id"], "invocation_sha256": value["binding"]["invocation_sha256"],
        "container_sha256": value["binding"]["container_id"], "identity_sha256": value["identity_sha256"],
        "observation_sha256": value["observation_sha256"], "context_sha256": value["context_sha256"],
        "preparation_sha256": value["preparation_sha256"], "execution_authorized": False, "runtime_evidence": False}
    require(len(canonical(result)) <= LIMITS["receipt_bytes"], "ADMISSION_SUMMARY_BOUND")
    return result
