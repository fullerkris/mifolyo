"""Replay unchanged original independent tests against final frozen modules.

Only module bindings/output destinations are redirected in memory. The initial
probe source and every initial evidence file remain untouched.
"""
import hashlib
import importlib.util
import json
from pathlib import Path
import sys

HERE = Path(__file__).resolve().parent
OUT = HERE.parent
INITIAL = OUT.parent
FINAL = OUT / "source/tests/crawl-jobs-v2-observer"
ORIGINAL_PROBE = INITIAL / "correctness-review/independent_probes.py"
sys.dont_write_bytecode = True
sys.path.insert(0, str(FINAL))

# Bind product modules first; the unchanged probe's imports then reuse these.
import contracts
import docker_backend
import local_process
import streams

modules = (contracts, docker_backend, local_process, streams)
assert all(Path(module.__file__).resolve().parent == FINAL for module in modules)
spec = importlib.util.spec_from_file_location("original_independent_controls_replayed", ORIGINAL_PROBE)
original = importlib.util.module_from_spec(spec)
spec.loader.exec_module(original)
original.HERE, original.SOURCE = HERE, FINAL
sys.path = [str(FINAL)] + [entry for entry in sys.path if entry not in (str(FINAL), str(original.HERE.parent / "source/tests/crawl-jobs-v2-observer"), str(INITIAL / "source/tests/crawl-jobs-v2-observer"))]
assert original.db is docker_backend and original.streams is streams and original.lp is local_process
mode = sys.argv[1]
assert mode in ("022", "077")
exit_code = original.main()
wrong = {name: module.__file__ for name, module in sys.modules.items()
    if getattr(module, "__file__", None) and str(INITIAL / "source") + "/" in str(module.__file__)}
assert not wrong, wrong
value = {"old_probe": str(ORIGINAL_PROBE),
    "old_probe_sha256": hashlib.sha256(ORIGINAL_PROBE.read_bytes()).hexdigest(),
    "wrapper_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
    "old_probe_modified": False, "test_methods_modified": False,
    "in_memory_redirects": ["preloaded final product modules", "HERE output directory", "SOURCE hash directory", "fixture import path"],
    "product_modules": {module.__name__: {"path": module.__file__, "sha256": hashlib.sha256(Path(module.__file__).read_bytes()).hexdigest()}
        for module in modules}, "old_source_modules_loaded": wrong,
    "output_directory": str(HERE), "umask": mode, "exit_code": exit_code}
with (HERE / ("replay-binding-" + mode + ".json")).open("x") as stream:
    json.dump(value, stream, sort_keys=True, indent=2)
    stream.write("\n")
raise SystemExit(exit_code)
