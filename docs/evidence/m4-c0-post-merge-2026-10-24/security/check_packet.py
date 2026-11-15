"""Read-only frozen-packet integrity check; outputs only in this review directory."""
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import stat
import sys

ROOT = Path(__file__).resolve().parents[3]
OUT = Path(__file__).resolve().parent
PACKET = ROOT / "out/c0-hosted-images-2026-10-24/review-inputs.json"
EXPECTED = "d63b6e8d9b9bab7d0a883d2cbdaa59eb7a8de8050e56dae28b447a2e638a315d"


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def main():
    stage = sys.argv[1]
    if stage not in ("before", "after"):
        raise ValueError("stage must be before or after")
    raw = PACKET.read_bytes()
    actual = hashlib.sha256(raw).hexdigest()
    if actual != EXPECTED:
        raise ValueError("frozen packet hash mismatch")
    packet = json.loads(raw)
    mismatches = []
    verified = {}
    for name, expected in packet["files"].items():
        rel = PurePosixPath(name)
        if rel.is_absolute() or ".." in rel.parts or str(rel) != name:
            raise ValueError("noncanonical packet path")
        path = ROOT / name
        parent = path
        while parent != ROOT:
            if parent.is_symlink():
                raise ValueError("symlink in packet path: " + name)
            parent = parent.parent
        try:
            info = path.stat()
            if not stat.S_ISREG(info.st_mode):
                raise ValueError("nonregular packet path: " + name)
            observed = {"bytes": info.st_size, "sha256": digest(path)}
            if observed != {key: expected[key] for key in ("bytes", "sha256")}:
                mismatches.append({"path": name, "expected": expected, "observed": observed})
            verified[name] = observed
        except OSError as error:
            mismatches.append({"path": name, "error": str(error)})
    result = {
        "kind": "independent_frozen_packet_integrity",
        "stage": stage,
        "packet_sha256": actual,
        "commit": packet["commit"],
        "files_expected": len(packet["files"]),
        "files_checked": len(verified),
        "bytes_checked": sum(row["bytes"] for row in verified.values()),
        "mismatches": mismatches,
        "status": "PASS" if len(verified) == 428 and not mismatches else "FAIL",
        "files": verified,
    }
    with (OUT / ("integrity-" + stage + ".json")).open("x") as stream:
        json.dump(result, stream, sort_keys=True, indent=2)
        stream.write("\n")
    print(json.dumps({key: value for key, value in result.items() if key != "files"}, sort_keys=True))
    return result["status"] != "PASS"


if __name__ == "__main__":
    raise SystemExit(main())
