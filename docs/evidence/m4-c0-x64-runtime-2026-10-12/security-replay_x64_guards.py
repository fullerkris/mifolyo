"""Replay existing authorization/expiry tests with x64 fixture data, no source edits."""
import importlib.util
import json
from pathlib import Path
import sys
import unittest
from unittest.mock import patch

ROOT = Path('/tmp/opencode/m4-c0-x64-runtime-2026-10-12')
SOURCE = ROOT/'snapshot/tests/crawl-jobs-v2-observer'
sys.path.insert(0,str(SOURCE))
import test_admission_dispatch as dispatch
import test_oracle_bridge as bridge
import test_context_oracle_review as review
import test_runtime_admission as native
import test_owned_resources as owned
import docker_backend as db

spec = importlib.util.spec_from_file_location('review_runner',ROOT/'snapshot/scripts/test-crawl-jobs-v2-observer.py')
runner = importlib.util.module_from_spec(spec); spec.loader.exec_module(runner)

original_fixture = native.DockerAdmissionTests.fixture
original_init = db.DockerBackend.__init__
observed = []
def backend_init(self,value,*args,**kwargs):
    assert value['architecture'] == 'x86_64'
    observed.append(value['architecture'])
    original_init(self,value,*args,**kwargs)

def x64_fixture(self):
    # Only test-fixture scope and fake daemon response are changed. Production
    # admission/dispatch implementations and every test assertion are untouched.
    with patch.object(native,'prepared',side_effect=lambda:owned.prepared('x86_64')):
        parts = original_fixture(self)
    backend = parts[2]
    previous = backend.runner
    def x64_runner(argv,deadline):
        result = previous(argv,deadline)
        return (0,b'{"Os":"linux","Arch":"amd64"}',b'') if argv[3] == 'version' else result
    backend.runner = x64_runner
    return parts

suite = unittest.TestSuite()
loader = unittest.TestLoader()
for cls in (dispatch.DispatchRegressionTests,bridge.OracleBridgeTests,native.DockerAdmissionTests):
    suite.addTests(loader.loadTestsFromTestCase(cls))
for name in ('test_final_oracle_poll_rechecks_observer_expiry','test_final_oracle_poll_rechecks_context_lease',
             'test_final_oracle_poll_rechecks_target_expiry','test_final_oracle_poll_rechecks_oracle_expiry',
             'test_live_final_oracle_poll_still_allows_start'):
    suite.addTest(review.ContextOracleReviewTests(name))

with patch.object(dispatch.PipePair.__init__,'__kwdefaults__',{'architecture':'x86_64'}), \
     patch.object(bridge.OraclePair.__init__,'__kwdefaults__',{'architecture':'x86_64'}), \
     patch.object(native.preparation,'__defaults__',('x86_64',)), \
     patch.object(native.context,'__defaults__',('x86_64',)), \
     patch.object(native.DockerAdmissionTests,'fixture',x64_fixture), \
     patch.object(db.DockerBackend,'__init__',backend_init):
    result = runner.run_suite(suite)
result.update(kind='reviewer_x64_fixture_replay',architecture='x86_64',
              backend_architectures=observed,production_source_edited=False,
              test_assertions_changed=False,real_docker_or_linux_collector=False)
(ROOT/'security/x64-guards.json').write_text(json.dumps(result,sort_keys=True,indent=2)+'\n')
print(json.dumps({'status':result['status'],'tests_run':result['tests_run'],'x64_backend_instances':len(observed)}))
raise SystemExit(result['status'] != 'PASS')
