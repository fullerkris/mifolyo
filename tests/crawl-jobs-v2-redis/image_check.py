"""Run via stdin in a networkless candidate image; never connect to Redis.

Checks the complete /app inventory, then exercises the real request validator
under the candidate's container memory limit. JSON output contains no credentials.
The host compares every emitted digest with the reviewed source tree.
"""
import hashlib
import json
import os
from pathlib import Path
import resource
import sys
import time

ROOT = Path("/app")
HERE = ROOT / "tests/crawl-jobs-v2-redis"
sys.path.insert(0, str(HERE))

import executor
import harness
import runtime_case


def main():
    started = time.monotonic()
    identities, operations = harness.source_identity()
    expected = {
        "docs/crawl-jobs-v2.md", "contracts/crawl-jobs-v2/digest-vectors.json",
        "scripts/generate-crawl-jobs-v2-bundle.py",
        "services/spider/internal/database/crawljobsv2/script_bundle_generated.go",
        *("services/spider/internal/database/crawljobsv2/lua/" + op.lower() + ".lua" for op in operations),
        *("tests/crawl-jobs-v2-redis/" + name for name in runtime_case.FILES),
    }
    actual = set()
    for path in ROOT.rglob("*"):
        if path.is_symlink():
            raise ValueError("unexpected image symlink")
        if path.is_file():
            actual.add(path.relative_to(ROOT).as_posix())
    if actual != expected:
        raise ValueError("image inventory mismatch")
    files = {name: hashlib.sha256((ROOT / name).read_bytes()).hexdigest() for name in sorted(actual)}
    redis_image, harness_image = sys.argv[1:3]
    recipes = {}
    for case_id, scenario in runtime_case.CASES.items():
        inputs = {"format_version": 1, "scenario": scenario, "redis_version": "7.4.11",
                  "redis_image": redis_image, "harness_image": harness_image, "standin_image": harness_image}
        plan = harness.compile_plan(inputs)
        recipes[case_id] = runtime_case.recipe_sha256(case_id)
        request = {"plan": plan, "recipe_sha256": recipes[case_id], "fixture_id": "1" * 32,
                   "credentials": {role: format(i + 1, "064x") for i, role in enumerate(runtime_case.ROLES)}, "previous": {}}
        if case_id == runtime_case.CLAIM_CASE:
            request["claim_material"] = {"owner_a": "2" * 32, "owner_b": "3" * 32,
                                         "token_a": "4" * 64, "token_b": "5" * 64, "wrong_token": "6" * 64}
        executor.validate_request(request)
    isolation = executor.environment(init=os.geteuid() == 0)
    peak_file = Path("/sys/fs/cgroup/memory.peak")
    memory_peak = int(peak_file.read_text().strip()) if peak_file.is_file() else None
    print(json.dumps({"status": "PASS", "kind": "image_validation_only", "execution_authorized": False,
                      "python": sys.version.split()[0], "identities": identities,
                      "recipe_sha256": runtime_case.recipe_sha256(), "recipes": recipes, "files": files,
                      "file_count": len(files), "isolation": isolation,
                      "max_rss_bytes": resource.getrusage(resource.RUSAGE_SELF).ru_maxrss * 1024,
                      "cgroup_memory_peak_bytes": memory_peak,
                      "elapsed_ms": int((time.monotonic() - started) * 1000)}, sort_keys=True))


if __name__ == "__main__":
    main()
