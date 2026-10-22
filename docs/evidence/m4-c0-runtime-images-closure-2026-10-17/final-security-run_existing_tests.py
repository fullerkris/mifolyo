import json
from pathlib import Path
import subprocess
import sys
import unittest
from unittest.mock import patch

ROOT = Path('/tmp/opencode/c0-runtime-images-closure-2026-10-17')
OUT = ROOT / 'security'
PACKAGE = ROOT / 'snapshot/tests/crawl-jobs-v2-observer'
sys.dont_write_bytecode = True
sys.path.insert(0, str(PACKAGE))
suite = unittest.defaultTestLoader.discover(str(PACKAGE), pattern='test_image*.py')
def ids(suite):
    for item in suite:
        if isinstance(item, unittest.TestSuite): yield from ids(item)
        else: yield item.id()
inventory = sorted(ids(suite))
with patch.object(subprocess, 'run', side_effect=AssertionError('Real subprocess prohibited')), \
        patch.object(subprocess, 'Popen', side_effect=AssertionError('Real subprocess prohibited')), \
        patch.object(subprocess, 'check_output', side_effect=AssertionError('Real subprocess prohibited')):
    result = unittest.TextTestRunner(verbosity=2).run(suite)
report = {'python': sys.version, 'inventory': inventory, 'tests_run': result.testsRun,
          'failures': [{'id': test.id(), 'traceback': trace} for test, trace in result.failures],
          'errors': [{'id': test.id(), 'traceback': trace} for test, trace in result.errors],
          'skipped': list(result.skipped), 'pass': result.wasSuccessful() and not result.skipped,
          'real_subprocess_APIs_blocked': True}
with (OUT / 'existing-image-tests.json').open('x') as file:
    json.dump(report, file, sort_keys=True, indent=2); file.write('\n')
raise SystemExit(0 if report['pass'] else 1)
