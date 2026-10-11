"""Fixed-path Linux observations shared by the trusted host source and oracle.

This reader neither enters namespaces nor changes credentials/kernel policy.
The host source must separately match an operator-admitted namespace/boot anchor.
"""
import ctypes
import os
from pathlib import Path
import re
import resource
import stat
import time

from contracts import LIMITS, Invalid, canonical, clock_ns, integer, require, sha
from runtime_admission import NAMESPACES, native_frame


def source_hash(names):
    root = Path(__file__).resolve().parent
    files = {}
    for name in names:
        path = root / name
        fd = os.open(path, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(fd, "rb") as stream:
            info = os.fstat(stream.fileno())
            require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and 0 < info.st_size <= 262144, "SOURCE_FILE")
            raw = stream.read(262145)
            after = os.fstat(stream.fileno())
            require(len(raw) == info.st_size and (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns) ==
                (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns), "SOURCE_CHANGED")
            files[name] = sha(raw)
    return sha(canonical(files))


class LinuxReader:
    evidence_kind = "linux_fixed_path_observation"

    def read(self, path, limit=4095):
        fd = os.open(path, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW | os.O_NONBLOCK)
        try:
            require(stat.S_ISREG(os.fstat(fd).st_mode), "LINUX_FILE")
            chunks, used = [], 0
            while True:
                raw = os.read(fd, min(4096, limit + 1 - used))
                if not raw:
                    break
                chunks.append(raw)
                used += len(raw)
                require(used <= limit, "LINUX_READ_BOUND")
            result = b"".join(chunks)
            require(result and b"\0" not in result, "LINUX_FILE")
            return result
        finally:
            os.close(fd)

    def number(self, path):
        raw = self.read(path, 127).strip()
        require(re.fullmatch(rb"[0-9]+", raw) is not None, "LINUX_NUMBER")
        return integer(int(raw))

    def namespaces(self):
        values, device = {}, None
        for name in NAMESPACES:
            info = os.stat("/proc/self/ns/" + name.removesuffix("_ns"))
            integer(info.st_ino, 1)
            integer(info.st_dev, 1)
            require(device is None or device == info.st_dev, "LINUX_NAMESPACE_DEVICE")
            device, values[name] = info.st_dev, info.st_ino
        return values, device

    def monotonic_ns(self):
        return clock_ns(time.clock_gettime_ns(time.CLOCK_MONOTONIC), 1)

    def kernel(self):
        uname = os.uname()
        require(uname.sysname == "Linux" and uname.machine == "aarch64", "LINUX_PLATFORM")
        namespaces, device = self.namespaces()
        offsets = self.read("/proc/self/timens_offsets")
        require(re.fullmatch(rb"\s*monotonic\s+0\s+0\s+boottime\s+0\s+0\s*", offsets) is not None, "LINUX_TIME_OFFSETS")
        resolution = round(time.clock_getres(time.CLOCK_MONOTONIC) * 1000000000)
        integer(resolution, 1, 1000000)
        yama = integer(self.number("/proc/sys/kernel/yama/ptrace_scope"), 0, 3)
        return {"kernel_boot_sha256": sha(self.read("/proc/sys/kernel/random/boot_id")),
            "kernel_release_sha256": sha(uname.release.encode()), "uid_map_sha256": sha(self.read("/proc/self/uid_map")),
            "gid_map_sha256": sha(self.read("/proc/self/gid_map")), "lsm_sha256": sha(self.read("/sys/kernel/security/lsm")),
            "time_offsets_sha256": sha(offsets), "namespaces": namespaces, "ns_device": device,
            "resolution_ns": resolution, "yama_scope": yama}

    def ticks(self):
        raw = self.read("/proc/self/stat")
        end = raw.rfind(b") ")
        require(end > 0, "LINUX_STAT")
        fields = raw[end + 2:].split()
        require(len(fields) >= 20 and fields[19].isdigit(), "LINUX_STAT")
        return integer(int(fields[19]), 1)

    def executable(self):
        import hashlib
        fd = os.open("/proc/self/exe", os.O_RDONLY | os.O_CLOEXEC)
        try:
            before = os.fstat(fd)
            require(stat.S_ISREG(before.st_mode) and 0 < before.st_size <= 32 * 1024 * 1024, "LINUX_EXECUTABLE")
            digest, size = hashlib.sha256(), 0
            while True:
                raw = os.read(fd, 32768)
                if not raw:
                    break
                size += len(raw)
                require(size <= before.st_size, "LINUX_EXECUTABLE")
                digest.update(raw)
            after = os.fstat(fd)
            require(size == before.st_size and (before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns) ==
                (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns), "LINUX_EXECUTABLE")
            return digest.hexdigest()
        finally:
            os.close(fd)

    def security(self):
        selected = ("Uid", "Gid", "Groups", "CapInh", "CapPrm", "CapEff", "CapBnd", "CapAmb",
            "NoNewPrivs", "Seccomp", "Seccomp_filters", "Threads", "TracerPid")
        values = {}
        for line in self.read("/proc/self/status", 8191).splitlines():
            key, separator, value = line.partition(b":")
            if separator and key.decode("ascii") in selected:
                key = key.decode("ascii")
                require(key not in values, "LINUX_STATUS")
                parts = value.split()
                base = 16 if key.startswith("Cap") else 10
                require(all(re.fullmatch(rb"[0-9a-fA-F]+" if base == 16 else rb"[0-9]+", part) for part in parts), "LINUX_STATUS")
                values[key] = [int(part, base) for part in parts]
        require(set(values) == set(selected), "LINUX_STATUS")
        require(values["Uid"] == values["Gid"] == [999] * 4 and values["Groups"] in ([], [999]), "LINUX_UID")
        for key in selected[3:8]:
            require(values[key] == [0], "LINUX_CAPABILITIES")
        for key, expected in (("NoNewPrivs", 1), ("Seccomp", 2), ("Threads", 1), ("TracerPid", 0)):
            require(values[key] == [expected], "LINUX_CONFINEMENT")
        require(len(values["Seccomp_filters"]) == 1, "LINUX_CONFINEMENT")
        filters = integer(values["Seccomp_filters"][0], 1, 16)
        prctl = ctypes.CDLL(None, use_errno=True).prctl
        prctl.argtypes = [ctypes.c_int, ctypes.c_ulong, ctypes.c_ulong, ctypes.c_ulong, ctypes.c_ulong]
        require(all(prctl(op, 0, 0, 0, 0) == expected for op, expected in ((39, 1), (3, 1), (21, 2))), "LINUX_CONFINEMENT")
        require(resource.getrlimit(resource.RLIMIT_CORE) == (0, 0), "LINUX_CORE")
        require(os.listdir("/proc/self/task") == [str(os.getpid())], "LINUX_TASKS")
        return {"caps": [0] * 5, "core_hard": 0, "core_soft": 0, "dumpable": 1, "uids": values["Uid"],
            "gids": values["Gid"], "groups": values["Groups"], "nnp": 1, "seccomp": 2,
            "seccomp_filters": filters, "tasks": [os.getpid()], "threads": 1, "tracer_pid": 0}

    def actor_frames(self, case_id, invocation_sha256):
        """Oracle-only startup observations; target/observer retain their native collectors."""
        require(os.getpid() == 1, "LINUX_PID")
        before, ticks, security = self.kernel(), self.ticks(), self.security()
        executable = self.executable()
        cgroup = self.read("/proc/self/cgroup")
        require(cgroup == b"0::/\n", "LINUX_CGROUP")
        cpu = self.read("/sys/fs/cgroup/cpu.max", 127).split()
        require(len(cpu) == 2 and all(part.isdigit() for part in cpu), "LINUX_CPU")
        resources = {key: self.number("/sys/fs/cgroup/" + path) for key, path in {
            "memory_max": "memory.max", "swap_max": "memory.swap.max", "memory_current": "memory.current",
            "memory_peak": "memory.peak", "pids_max": "pids.max", "pids_current": "pids.current"}.items()}
        processes = self.read("/sys/fs/cgroup/cgroup.procs", 127).split()
        require(processes == [b"1"], "LINUX_TASKS")
        resources.update(cgroup_sha256=sha(cgroup), cpu_quota=int(cpu[0]), cpu_period=int(cpu[1]), processes=[1])
        identity = {"boot_sha256": before["kernel_boot_sha256"], "executable_sha256": executable,
            "uid_map_sha256": before["uid_map_sha256"], "gid_map_sha256": before["gid_map_sha256"],
            "pid": 1, "tid": 1, "start_ticks": ticks, "ns_device": before["ns_device"], **before["namespaces"]}
        require(self.kernel() == before and self.ticks() == ticks and self.security() == security, "LINUX_CHANGED")
        clock = {key: before[key] for key in ("kernel_release_sha256", "lsm_sha256", "time_offsets_sha256", "resolution_ns", "yama_scope")}
        clock["monotonic_ns"] = self.monotonic_ns()
        common = {"case_id": case_id, "invocation_sha256": invocation_sha256, "role": "oracle", "version": 1}
        output = []
        for phase, fields in zip(("IDENTITY", "CONFINEMENT", "RESOURCES", "CLOCK"), (identity, security, resources, clock)):
            raw = canonical(common | {"phase": phase} | fields)
            native_frame(raw, phase)
            output.append(raw)
        return output
