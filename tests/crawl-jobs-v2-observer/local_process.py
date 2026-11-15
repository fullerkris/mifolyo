"""Linux child ownership: signal the reserved process group before reaping.

Callers exclusively own a Popen child created with start_new_session=True. They
must not poll/wait/reap elsewhere. Group signalling is not descendant-absence or
remote-container proof: a process that creates another session is outside it.
"""
import os
import signal
import subprocess
import time

from contracts import Invalid, require

CLEANUP_SECONDS = 5


def observe_exit(process):
    require(process.returncode is None, "LOCAL_PROCESS_OWNERSHIP")
    try:
        return os.waitid(os.P_PID, process.pid, os.WEXITED | os.WNOHANG | os.WNOWAIT)
    except (OSError, ChildProcessError):
        raise Invalid("LOCAL_PROCESS_OWNERSHIP") from None


def wait_unreaped(process, deadline):
    """Bounded exit observation which keeps the leader's PID reserved."""
    while True:
        deadline.check()
        exited = observe_exit(process)
        deadline.check()
        if exited is not None:
            return
        time.sleep(min(deadline.remaining(), 0.01))


def kill_and_reap(process):
    # Even an exited leader with closed stdout/stderr can have live children.
    # WNOWAIT checks ownership without releasing the PID used by killpg.
    observe_exit(process)
    failed = False
    try:
        os.killpg(process.pid, signal.SIGKILL)
    except ProcessLookupError:
        pass
    except OSError:
        failed = True
    try:
        process.wait(timeout=CLEANUP_SECONDS)
    except (OSError, subprocess.TimeoutExpired):
        failed = True
    require(not failed, "LOCAL_PROCESS_CLEANUP")
