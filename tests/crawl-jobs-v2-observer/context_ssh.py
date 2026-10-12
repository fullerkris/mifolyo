"""Fixed SSH forced-command entrypoint; no request-selected command or path."""
import os
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parent))
from contracts import decode, digest, exact, integer, require
import context_files as files
from linux_observation import LinuxReader
import runtime_context as context
import context_worker


def admit_session():
    require(len(sys.argv) == 1 and os.environ.get("SSH_ORIGINAL_COMMAND") == files.REMOTE_COMMAND, "CONTEXT_SSH_COMMAND")
    service = decode(files.read_config("context-service.json", 0, (0o444,)), 4096)
    exact(service, ("uid", "gid", "python_sha256"))
    digest(service["python_sha256"])
    uid, gid = integer(service["uid"], 1, 2**31 - 1), integer(service["gid"], 1, 2**31 - 1)
    require(uid != 999 and gid != 999 and os.getuid() == os.geteuid() == uid and
        os.getgid() == os.getegid() == gid and os.getgroups() in ([], [gid]), "CONTEXT_SERVICE_IDENTITY")
    require(Path(__file__).resolve().parent == files.CODE, "CONTEXT_CODE_LOCATION")
    fd = files.directory(files.CODE, 0, 0o755)
    try:
        require(set(os.listdir(fd)) == set(context.SOURCE_FILES), "CONTEXT_CODE_INVENTORY")
        for name in context.SOURCE_FILES:
            files.read_at(fd, name, 0, (0o444,), 262144)
    finally:
        os.close(fd)
    require(LinuxReader().executable() == service["python_sha256"], "CONTEXT_PYTHON_IDENTITY")


def main():
    try:
        admit_session()
        return context_worker.main()
    except Exception:
        # No SSH/config/credential bytes appear in a refusal.
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
