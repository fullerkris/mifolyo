"""Run retained initial controls against the new snapshot without altering them.

Only the controls module's snapshot-selection string is replaced, in memory.
The original regression assertions are unchanged and its __main__ writer is not
executed. All new evidence is written to correctness-final.
"""
import hashlib
import json
from pathlib import Path
import sys
import types
import unittest

ROOT = Path('/tmp/opencode/c0-native-preflight-2026-10-17')
OUT = ROOT / 'correctness-final'
SOURCE = ROOT / 'remediation-snapshot/tests/crawl-jobs-v2-observer'
sys.dont_write_bytecode = True


def load_retained(name, filename, selection=False):
    path = ROOT / 'correctness' / filename
    text = path.read_text()
    if selection:
        old = "ROOT / 'snapshot/tests/crawl-jobs-v2-observer'"
        new = "ROOT / 'remediation-snapshot/tests/crawl-jobs-v2-observer'"
        assert text.count(old) == 1
        text = text.replace(old, new)
    module = types.ModuleType(name)
    module.__file__ = str(path)
    sys.modules[name] = module
    exec(compile(text, str(path), 'exec'), module.__dict__)
    return module


controls = load_retained('controls', 'controls.py', selection=True)
regressions = load_retained('retained_regressions', 'regressions.py')
import host_preflight
import test_host_preflight
assert Path(host_preflight.__file__).resolve() == SOURCE / 'host_preflight.py'
assert Path(test_host_preflight.__file__).resolve() == SOURCE / 'test_host_preflight.py'

suite = unittest.TestSuite([
    unittest.defaultTestLoader.loadTestsFromTestCase(controls.Controls),
    unittest.defaultTestLoader.loadTestsFromTestCase(regressions.Regressions),
])
result = unittest.TextTestRunner(verbosity=2).run(suite)
evidence = {
    'host_preflight_path': str(Path(host_preflight.__file__).resolve()),
    'host_preflight_sha256': hashlib.sha256(Path(host_preflight.__file__).read_bytes()).hexdigest(),
    'retained_controls_source_sha256': hashlib.sha256((ROOT / 'correctness/controls.py').read_bytes()).hexdigest(),
    'retained_regressions_source_sha256': hashlib.sha256((ROOT / 'correctness/regressions.py').read_bytes()).hexdigest(),
    'source_selection_only_changed_in_memory': True,
    'tests_run': result.testsRun,
    'success': result.wasSuccessful(),
    'failures': [test.id() for test, _ in result.failures],
    'errors': [test.id() for test, _ in result.errors],
    'skips': [test.id() for test, _ in result.skipped],
    'regression_observations': regressions.EVIDENCE,
}
(OUT / 'retained-probe-evidence.json').write_text(json.dumps(evidence, indent=2) + '\n')
raise SystemExit(0 if result.wasSuccessful() else 1)
