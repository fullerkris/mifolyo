"""Deterministic candidate OCI profiles; generation is not runtime admission."""
from contracts import require, runtime_architecture

COMMON = ("read", "write", "close", "fstat", "newfstatat", "statx", "lseek", "pread64", "openat",
    "readlinkat", "getdents64", "munmap", "brk", "madvise", "futex",
    "set_tid_address", "set_robust_list", "rseq", "rt_sigaction", "rt_sigprocmask", "rt_sigreturn",
    "sigaltstack", "clock_gettime", "clock_nanosleep", "nanosleep", "getpid", "gettid", "getppid",
    "getuid", "geteuid", "getgid", "getegid", "getrandom", "prlimit64", "fcntl",
    "execve", "exit", "exit_group", "sched_yield")
PTRACE = (0x4206, 0x4207, 0x4202, 0x4204, 0x4205, 2, 7, 9, 17)
# Linux x86-64 LP64 struct user; native code asserts these offsets at compile time.
X64_DEBUG_READ = (848, 856, 864, 872, 896, 904)  # DR0..3, DR6, DR7 (not DR4/5).
X64_DEBUG_WRITE = {848: None, 896: (0xffff0ff0,), 904: (0, 1)}


def profile(role, architecture="aarch64"):
    selected = runtime_architecture(architecture)
    require(role in ("observer", "target", "oracle"), "ROLE")
    allowed = list(COMMON)
    allowed += ["clock_getres", "uname"]
    if architecture == "x86_64":
        allowed += ["readlink"]
    if role != "target":
        allowed.remove("openat")
    if role == "observer":
        allowed.append("wait4")
    elif role == "target":
        allowed += ["ftruncate", "bind", "listen", "accept4", "sendto", "fchmodat"]
        if architecture == "x86_64":
            allowed.append("chmod")  # glibc's x64 chmod wrapper uses the legacy syscall.
    else:
        allowed += ["connect", "recvfrom", "poll", "ppoll", "getcwd"]
    rows = [{"names": sorted(allowed), "action": "SCMP_ACT_ALLOW", "args": []}]
    if architecture == "x86_64":
        rows.append({"names": ["arch_prctl"], "action": "SCMP_ACT_ALLOW",
            "args": [{"index": 0, "value": 0x1002, "op": "SCMP_CMP_EQ"}]})  # ARCH_SET_FS, libc TLS.
    rows += [{"names": ["mmap", "mprotect"], "action": "SCMP_ACT_ALLOW",
        "args": [{"index": 2, "value": 4, "valueTwo": 0, "op": "SCMP_CMP_MASKED_EQ"}]},
         {"names": ["prctl"], "action": "SCMP_ACT_ALLOW", "args": [{"index": 0, "value": 39, "op": "SCMP_CMP_EQ"}]}]
    if role == "oracle":
        # CPython's loader needs read-only file-backed executable mappings.
        # Anonymous executable mappings, W+X and executable mprotect stay denied.
        rows += [{"names": ["mmap"], "action": "SCMP_ACT_ALLOW", "args": [
            {"index": 2, "value": 2, "valueTwo": 0, "op": "SCMP_CMP_MASKED_EQ"},
            {"index": 3, "value": 32, "valueTwo": 0, "op": "SCMP_CMP_MASKED_EQ"}]},
            {"names": ["getsockopt"], "action": "SCMP_ACT_ALLOW", "args": [
                {"index": 1, "value": 1, "op": "SCMP_CMP_EQ"}, {"index": 2, "value": 17, "op": "SCMP_CMP_EQ"}]}]
    if role != "target":
        rows.append({"names": ["openat"], "action": "SCMP_ACT_ALLOW",
            "args": [{"index": 2, "value": 1 | 2 | 64 | 512 | 1024, "valueTwo": 0, "op": "SCMP_CMP_MASKED_EQ"}]})
    for operation in (3, 21):  # PR_GET_DUMPABLE / PR_GET_SECCOMP; no setters.
        rows.append({"names": ["prctl"], "action": "SCMP_ACT_ALLOW", "args": [{"index": 0, "value": operation, "op": "SCMP_CMP_EQ"}]})
    if role != "observer":
        rows.append({"names": ["socket"], "action": "SCMP_ACT_ALLOW",
            "args": [{"index": 0, "value": 1, "op": "SCMP_CMP_EQ"}, {"index": 1, "value": 15, "valueTwo": 1, "op": "SCMP_CMP_MASKED_EQ"}]})
    if role == "target":
        rows += [{"names": ["clone"], "action": "SCMP_ACT_ALLOW",
            "args": [{"index": 0, "value": 0x3d0f00, "op": "SCMP_CMP_EQ"}]},
            {"names": ["clone3"], "action": "SCMP_ACT_ERRNO", "errnoRet": 38, "args": []}]
    if role == "observer":
        rows.append({"names": ["prctl"], "action": "SCMP_ACT_ALLOW",
            "args": [{"index": 0, "value": 22, "op": "SCMP_CMP_EQ"}, {"index": 1, "value": 2, "op": "SCMP_CMP_EQ"}]})
        for request in PTRACE:
            if architecture == "x86_64" and request == 0x4205:
                continue  # No GP, FP, ARM regset or shadow-stack writes on x64.
            regsets = (1, 0x402) if request == 0x4204 else (0x402,) if request == 0x4205 else (None,)
            if architecture == "x86_64" and request == 0x4204:
                regsets = (1,)
            for regset in regsets:
                args = [{"index": 0, "value": request, "op": "SCMP_CMP_EQ"}, {"index": 1, "value": 1, "op": "SCMP_CMP_EQ"}]
                if regset is not None:
                    args.append({"index": 2, "value": regset, "op": "SCMP_CMP_EQ"})
                rows.append({"names": ["ptrace"], "action": "SCMP_ACT_ALLOW", "args": args})
        if architecture == "x86_64":
            for request, offsets in ((3, dict.fromkeys(X64_DEBUG_READ)), (6, X64_DEBUG_WRITE)):
                for offset, values in offsets.items():
                    for value in (None,) if values is None else values:
                        args = [{"index": index, "value": item, "op": "SCMP_CMP_EQ"}
                            for index, item in enumerate((request, 1, offset))]
                        if value is not None:
                            args.append({"index": 3, "value": value, "op": "SCMP_CMP_EQ"})
                        rows.append({"names": ["ptrace"], "action": "SCMP_ACT_ALLOW", "args": args})
    return {"defaultAction": "SCMP_ACT_ERRNO", "defaultErrnoRet": 1,
        "architectures": [selected["seccomp_architecture"]], "syscalls": rows}


def target_filter_program(architecture="aarch64"):
    """Classic BPF: fixed owned inner TID1, no post-admission exec or write regset."""
    selected = runtime_architecture(architecture)
    operations, labels = [], {}

    def label(name):
        labels[name] = len(operations)

    def load(offset):
        operations.append((0x20, 0, 0, offset))

    def equal(value, yes, no=None):
        operations.append((0x15, yes, no, value))

    load(4)
    equal(selected["audit_architecture"], "number", "kill")
    label("number")
    load(0)
    if architecture == "x86_64":
        # Closed native syscall range: excludes x32-bit calls, historical 512..547
        # compat aliases (including ptrace 521), and negative syscall numbers.
        operations.append((0x35, "kill", None, 512))
    equal(221 if architecture == "aarch64" else 59, "deny")
    equal(281 if architecture == "aarch64" else 322, "deny")
    equal(117 if architecture == "aarch64" else 101, "tid", "allow")
    label("tid")
    load(28)
    equal(0, "tid_low", "deny")
    label("tid_low")
    load(24)
    equal(1, "request_high", "deny")
    label("request_high")
    load(20)
    equal(0, "request", "deny")
    label("request")
    load(16)
    for request in (0x4206, 0x4207, 0x4202, 2, 7, 9, 17):
        equal(request, "allow")
    equal(0x4204, "get_high")
    if architecture == "aarch64":
        equal(0x4205, "set_high", "deny")
    else:
        equal(3, "debug_read_high")
        equal(6, "debug_write_high", "deny")
    label("get_high")
    load(36)
    equal(0, "get_low", "deny")
    label("get_low")
    load(32)
    if architecture == "aarch64":
        equal(1, "allow")
        equal(0x402, "allow", "deny")
        label("set_high")
        load(36)
        equal(0, "set_low", "deny")
        label("set_low")
        load(32)
        equal(0x402, "allow", "deny")
    else:
        equal(1, "allow", "deny")
        for action, offsets in (("read", X64_DEBUG_READ), ("write", tuple(X64_DEBUG_WRITE))):
            label("debug_" + action + "_high")
            load(36)
            equal(0, "debug_" + action + "_low", "deny")
            label("debug_" + action + "_low")
            load(32)
            for index, offset in enumerate(offsets):
                destination = "allow" if action == "read" or offset == 848 else "write_" + str(offset)
                equal(offset, destination, "deny" if index == len(offsets) - 1 else None)
        for offset in (896, 904):
            label("write_" + str(offset))
            load(44)
            equal(0, "write_low_" + str(offset), "deny")
            label("write_low_" + str(offset))
            load(40)
            for index, value in enumerate(X64_DEBUG_WRITE[offset]):
                equal(value, "allow", "deny" if index == len(X64_DEBUG_WRITE[offset]) - 1 else None)
    for name, result in (("kill", 0x80000000), ("deny", 0x50001), ("allow", 0x7fff0000)):
        label(name)
        operations.append((0x06, 0, 0, result))
    output = []
    for index, (code, yes, no, value) in enumerate(operations):
        if code in (0x15, 0x35):
            yes, no = [0 if target is None else labels[target] - index - 1 for target in (yes, no)]
            require(0 <= yes <= 255 and 0 <= no <= 255, "FILTER_JUMP")
        output.append((code, yes, no, value))
    return output
