"""Closed metadata/process admission predicates; no lifecycle dispatch."""
from pathlib import Path
import os
import re
import sys

import harness as h

ENTRY = "/app/tests/crawl-jobs-v2-redis/executor.py"
BASE_ENV = {"PATH", "LANG", "GPG_KEY", "PYTHON_VERSION", "PYTHON_SHA256", "GOSU_VERSION",
            "REDIS_VERSION", "REDIS_DOWNLOAD_URL", "REDIS_DOWNLOAD_SHA"}


def environment_digest(values):
    h.require(type(values) is list and len(values) <= len(BASE_ENV), "CONTAINER_ENV")
    names = set()
    for entry in values:
        h.require(type(entry) is str and "=" in entry and len(entry) <= 2048 and "\x00" not in entry and "\n" not in entry, "CONTAINER_ENV")
        name, value = entry.split("=", 1)
        h.require(name in BASE_ENV and name not in names and len(value) > 0, "CONTAINER_ENV")
        names.add(name)
    return h.digest(h.canonical(sorted(values)))


def command_for(role):
    h.require(role in ("init", "executor", "redis", "revocation"), "CONTAINER_ROLE")
    return (["redis-server"], ["/run/cj2/redis.conf"]) if role == "redis" else (["python3"], ["-B", ENTRY, "hold"])


def validate_processes(rows, current_pid, expected_current):
    """Exact role program/process inventory; values never appear in diagnostics."""
    h.require(type(rows) is list and 1 <= len(rows) <= 2 and type(current_pid) is int and current_pid > 0 and
              type(expected_current) is list and all(type(part) is str for part in expected_current), "PROCESS_INVENTORY")
    expected = {1: ["python3", "-B", ENTRY, "hold"], current_pid: expected_current}
    actual = {}
    for row in rows:
        h.exact(row, {"pid", "argv"})
        pid, argv = row["pid"], row["argv"]
        h.require(type(pid) is int and pid > 0 and pid not in actual and type(argv) is list and 1 <= len(argv) <= 8 and
                  all(type(part) is str and len(part) <= 256 for part in argv), "PROCESS_INVENTORY")
        actual[pid] = argv
    h.require(actual == expected, "PROCESS_INVENTORY")
    return {"process_count": len(rows), "process_inventory_sha256": h.digest(h.canonical(sorted(rows, key=lambda row: row["pid"])))}


def process_inventory():
    # Only the fixed holder and current exec may exist. Image validation instead
    # runs the reviewed stdin program as PID 1, with two immutable image arguments.
    if sys.argv[0] == "-":
        h.require(os.getpid() == 1 and len(sys.argv) == 3 and all(
            value.startswith("sha256:") and h.nonzero(value[7:]) for value in sys.argv[1:]), "PROCESS_INVENTORY")
        expected = ["python3", "-I", "-B", "-", *sys.argv[1:]]
    else:
        h.require(sys.argv[0] == ENTRY and len(sys.argv) == 3 and
                  sys.argv[1] in ("init", "ready", "probe", "resume", "measure", "revoke") and
                  re.fullmatch(r"[1-9][0-9]{0,4}", sys.argv[2]) and int(sys.argv[2]) <= 30000, "PROCESS_INVENTORY")
        expected = ["python3", "-B", *sys.argv]
    rows = []
    entries = [path for path in Path("/proc").iterdir() if path.name.isdigit()]
    h.require(len(entries) <= 64, "PROCESS_INVENTORY")
    for path in entries:
        try:
            with (path / "cmdline").open("rb") as stream:
                raw = stream.read(2049)
        except FileNotFoundError:
            continue
        h.require(raw.endswith(b"\x00") and len(raw) <= 2048, "PROCESS_INVENTORY")
        try:
            argv = [part.decode("utf-8") for part in raw[:-1].split(b"\x00")]
        except UnicodeError:
            raise h.InvalidArtifact("PROCESS_INVENTORY") from None
        rows.append({"pid": int(path.name), "argv": argv})
    return validate_processes(rows, os.getpid(), expected)


def empty_directories(directories):
    for directory in directories:
        h.require(directory.is_dir() and not directory.is_symlink() and not any(directory.iterdir()), "VOLUME_NOT_EMPTY")


def volume_attachments(volumes, attachments, allowed):
    h.exact(volumes, {"control", "data"})
    h.require(len(set(volumes.values())) == 2 and all(type(value) is str and value for value in volumes.values()), "VOLUME_SHARED")
    h.require(type(attachments) is dict and set(attachments) == set(volumes.values()) and type(allowed) is dict, "VOLUME_SHARED")
    for volume, names in attachments.items():
        h.require(type(names) is list and len(names) == len(set(names)) and len(names) <= 4 and
                  all(type(name) is str and name in allowed and volume in allowed[name] for name in names), "VOLUME_SHARED")
