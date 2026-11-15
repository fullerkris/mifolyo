"""Usage: python -B audit_inputs.py before|after. Never modifies retained inputs."""
import sys
from review_common import *

phase = sys.argv[1]
assert phase in ("before", "after")
assert hashfile(HOSTED / "review-inputs.json") == PACKET
manifest = load(HOSTED / "review-inputs.json")
assert len(manifest["files"]) == 428 and manifest["commit"] == COMMIT
failures = []
files = {}
for name, expected in manifest["files"].items():
    actual = record(ROOT / name)
    files[name] = actual
    if actual != expected:
        failures.append({"path": name, "expected": expected, "actual": actual})
extras = set()
for directory in (SYNC / "before", SYNC / "merged-source", CLOSURE):
    extras.update(p for p in directory.rglob("*") if p.is_file())
extras.add(OLD / "retention.json")
extras.update(OLD / name for name in load(OLD / "retention.json")["files"])
extra_files = {str(p.relative_to(ROOT)): record(p) for p in sorted(extras)}
state = git_state()
result = {"phase": phase, "packet_sha256": PACKET, "frozen_file_count": len(files),
    "frozen_total_bytes": sum(r["bytes"] for r in files.values()), "failures": failures,
    "files": files, "additional_files": extra_files, "additional_file_count": len(extra_files),
    "git_state": state}
if phase == "after":
    initial = load(REVIEW / "audit-before.json")
    result["frozen_unchanged"] = files == initial["files"]
    result["additional_unchanged"] = extra_files == initial["additional_files"]
    result["git_state_unchanged"] = state == initial["git_state"]
write("audit-" + phase + ".json", result)
assert not failures
if phase == "after":
    assert result["frozen_unchanged"] and result["additional_unchanged"] and result["git_state_unchanged"]
print(json.dumps({k: v for k, v in result.items() if k not in ("files", "additional_files", "git_state")}))
