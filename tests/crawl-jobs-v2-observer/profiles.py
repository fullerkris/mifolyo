"""Deterministic candidate OCI profiles; generation is not runtime admission."""
from contracts import require

COMMON = ("read", "write", "close", "fstat", "newfstatat", "statx", "lseek", "pread64", "openat",
    "readlinkat", "getdents64", "munmap", "brk", "madvise", "futex",
    "set_tid_address", "set_robust_list", "rseq", "rt_sigaction", "rt_sigprocmask", "rt_sigreturn",
    "sigaltstack", "clock_gettime", "clock_nanosleep", "nanosleep", "getpid", "gettid", "getppid",
    "getuid", "geteuid", "getgid", "getegid", "getrandom", "prlimit64", "fcntl",
    "execve", "exit", "exit_group", "sched_yield")
PTRACE = (0x4206, 0x4207, 0x4202, 0x4204, 0x4205, 2, 7, 9, 17)


def profile(role):
    require(role in ("observer", "target", "oracle"), "ROLE")
    allowed = list(COMMON)
    if role in ("target", "observer"):
        allowed += ["clock_getres", "uname"]
    if role != "target":
        allowed.remove("openat")
    if role == "observer":
        allowed.append("wait4")
    elif role == "target":
        allowed += ["ftruncate", "bind", "listen", "accept4", "sendto", "fchmodat"]
    else:
        allowed += ["connect", "recvfrom", "poll", "ppoll", "getcwd"]
    rows = [{"names": sorted(allowed), "action": "SCMP_ACT_ALLOW", "args": []}]
    rows += [{"names": ["mmap", "mprotect"], "action": "SCMP_ACT_ALLOW",
        "args": [{"index": 2, "value": 4, "valueTwo": 0, "op": "SCMP_CMP_MASKED_EQ"}]},
        {"names": ["prctl"], "action": "SCMP_ACT_ALLOW", "args": [{"index": 0, "value": 39, "op": "SCMP_CMP_EQ"}]}]
    if role != "target":
        rows.append({"names": ["openat"], "action": "SCMP_ACT_ALLOW",
            "args": [{"index": 2, "value": 1 | 2 | 64 | 512 | 1024, "valueTwo": 0, "op": "SCMP_CMP_MASKED_EQ"}]})
    if role in ("target", "observer"):
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
            regsets = (1, 0x402) if request == 0x4204 else (0x402,) if request == 0x4205 else (None,)
            for regset in regsets:
                args = [{"index": 0, "value": request, "op": "SCMP_CMP_EQ"}, {"index": 1, "value": 1, "op": "SCMP_CMP_EQ"}]
                if regset is not None:
                    args.append({"index": 2, "value": regset, "op": "SCMP_CMP_EQ"})
                rows.append({"names": ["ptrace"], "action": "SCMP_ACT_ALLOW", "args": args})
    return {"defaultAction": "SCMP_ACT_ERRNO", "defaultErrnoRet": 1,
        "architectures": ["SCMP_ARCH_AARCH64"], "syscalls": rows}


def target_filter_program():
    """Classic BPF: fixed owned inner TID1, no post-admission exec or write regset."""
    operations, labels = [], {}

    def label(name):
        labels[name] = len(operations)

    def load(offset):
        operations.append((0x20, 0, 0, offset))

    def equal(value, yes, no=None):
        operations.append((0x15, yes, no, value))

    load(4)
    equal(0xc00000b7, "number", "kill")
    label("number")
    load(0)
    equal(221, "deny")
    equal(281, "deny")
    equal(117, "tid", "allow")
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
    equal(0x4205, "set_high", "deny")
    label("get_high")
    load(36)
    equal(0, "get_low", "deny")
    label("get_low")
    load(32)
    equal(1, "allow")
    equal(0x402, "allow", "deny")
    label("set_high")
    load(36)
    equal(0, "set_low", "deny")
    label("set_low")
    load(32)
    equal(0x402, "allow", "deny")
    for name, result in (("kill", 0x80000000), ("deny", 0x50001), ("allow", 0x7fff0000)):
        label(name)
        operations.append((0x06, 0, 0, result))
    output = []
    for index, (code, yes, no, value) in enumerate(operations):
        if code == 0x15:
            yes, no = [0 if target is None else labels[target] - index - 1 for target in (yes, no)]
            require(0 <= yes <= 255 and 0 <= no <= 255, "FILTER_JUMP")
        output.append((code, yes, no, value))
    return output
