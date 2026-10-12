"""Stable owned-file reads for provisioned context credentials and code."""
import os
from pathlib import Path
import stat
from contracts import require

CONFIG = Path("/etc/obs1")
CODE = Path("/opt/obs1/context-source")
SERVICE_USER = "obs1-context"
REMOTE_COMMAND = "obs1-context-v1"
HOST_ALIAS = "obs1-context-source"
FORCED_COMMAND = "/usr/bin/python3 -I -B /opt/obs1/context-source/context_ssh.py"


def identity(info):
    return (info.st_dev, info.st_ino, info.st_uid, info.st_gid, info.st_mode,
        info.st_nlink, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def directory(path, uid, mode):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        info = os.fstat(fd)
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == uid and stat.S_IMODE(info.st_mode) == mode, "CONTEXT_DIRECTORY")
        return fd
    except BaseException:
        os.close(fd)
        raise


def read_at(parent, name, uid, modes, limit):
    require(type(name) is str and name not in ("", ".", "..") and "/" not in name and "\0" not in name, "CONTEXT_FILE_NAME")
    fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=parent)
    with os.fdopen(fd, "rb") as stream:
        before = os.fstat(stream.fileno())
        require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and before.st_uid == uid and
            stat.S_IMODE(before.st_mode) in modes and 0 < before.st_size <= limit, "CONTEXT_FILE")
        raw = stream.read(limit + 1)
        after = os.fstat(stream.fileno())
        named = os.stat(name, dir_fd=parent, follow_symlinks=False)
        require(len(raw) == before.st_size and identity(before) == identity(after) == identity(named), "CONTEXT_FILE_CHANGED")
        return raw


def read_config(name, uid, modes, limit=4096):
    fd = directory(CONFIG, 0, 0o755)
    try:
        return read_at(fd, name, uid, modes, limit)
    finally:
        os.close(fd)
