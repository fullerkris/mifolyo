"""Verify merged-base test/control continuity and provided reports against source."""
import ast
import json
from pathlib import Path
import subprocess
import sys

ROOT = Path('/tmp/opencode/m4-c0-x64-runtime-2026-10-12')
SOURCE = ROOT/'snapshot/tests/crawl-jobs-v2-observer'
REPO = Path('/var/home/kfuller/Documents/Git_Projects/mifolyo')
BASE = 'd8016469a5ffcceeb04329deec4e3dea2c72dd97'
PREFIX = 'tests/crawl-jobs-v2-observer/'
sys.path.insert(0,str(SOURCE))
import contracts as c
import runtime_context
import oracle_protocol

def original(name): return subprocess.check_output(['git','show',BASE+':'+PREFIX+name],cwd=REPO)
def methods(name,raw):
    result = {}
    for cls in ast.parse(raw).body:
        if isinstance(cls,ast.ClassDef):
            for method in cls.body:
                if isinstance(method,ast.FunctionDef) and method.name.startswith('test_'):
                    result[name[:-3]+'.'+cls.name+'.'+method.name] = ast.dump(method,include_attributes=False)
    return result

paths = subprocess.check_output(['git','ls-tree','-r','--name-only',BASE,'--',PREFIX],cwd=REPO,text=True).splitlines()
prior = {}; current = {}
for path in paths:
    name = path.removeprefix(PREFIX)
    if name.startswith('test_') and name.endswith('.py'): prior.update(methods(name,original(name)))
for path in SOURCE.glob('test_*.py'): current.update(methods(path.name,path.read_bytes()))
removed = sorted(prior.keys()-current.keys())
added = sorted(current.keys()-prior.keys())
changed = sorted(name for name in prior.keys() & current.keys() if prior[name] != current[name])
assert len(removed) == 5
expected_removed = {
    # Every removal is recorded below with its complete original AST. This is
    # the owner-authorized replacement of now-obsolete unimplemented-x64 tests.
    name for name in removed if name.startswith('test_architecture_contracts.')
}
assert set(removed) == expected_removed
for name in removed:
    assert 'x64' in name or 'unsupported' in name, name

reports = {}
for label,path in [('independent',ROOT/'security/tests.json'),('owner-022',ROOT/'tests-022.json'),('owner-077',ROOT/'tests-077.json')]:
    value = json.loads(path.read_bytes())
    assert value['status'] == 'PASS' and value['tests_run'] == 256
    assert value['inventory'] == value['started'] == value['passed'] == sorted(current)
    assert not any(value[name] for name in ('errors','failures','skipped','expected_failures','unexpected_successes'))
    assert value['source_files'] == json.loads((ROOT/'source-inventory.json').read_bytes())['files']
    reports[label] = {'sha256':c.sha(path.read_bytes()),'tests_run':value['tests_run'],'matching_inventory_and_source':True}

controls = ['docker_backend.py','owned_resources.py','runtime_context.py','runtime_admission.py','context_provision.py',
            'context_transport.py','context_files.py','context_ssh.py','context_worker.py','oracle_bridge.py','oracle_protocol.py',
            'oracle.py','streams.py','watchdog.py','controller.py','d02.py','worker.py','build_oracle.py','Dockerfile.oracle']
unchanged = []
for name in controls:
    assert original(name) == (SOURCE/name).read_bytes(), name
    unchanged.append(name)

result = {'status':'PASS','base':BASE,'prior_test_count':len(prior),'current_test_count':len(current),
          'retained_prior_test_ids':len(set(prior)&set(current)),'removed_ids':removed,'new_ids':added,
          'changed_retained_test_method_asts':changed,'unchanged_retained_method_asts':len(set(prior)&set(current))-len(changed),
          'removed_test_method_asts':{name:prior[name] for name in removed},'reports':reports,
          'unchanged_control_files':unchanged,'runtime_context_source_sha256':runtime_context.source_digest(),
          'oracle_worker_source_sha256':oracle_protocol.source_digest(),
          'note':'Fixture defaults/data changed for dual-ABI tests; changed retained methods are separately reviewed, not claimed unchanged.'}
(ROOT/'security/continuity.json').write_text(json.dumps(result,sort_keys=True,indent=2)+'\n')
print(json.dumps({key:result[key] for key in ('status','prior_test_count','current_test_count','retained_prior_test_ids','removed_ids','new_ids','changed_retained_test_method_asts','unchanged_retained_method_asts')},indent=2))
