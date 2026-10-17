"""Read-only current-code validation with explicit historical source-map join."""
from collections import Counter, defaultdict
import fnmatch
import hashlib
import json
from pathlib import Path
import sys

ROOT = Path('/tmp/opencode/c0-runtime-images-completion-2026-10-17')
LAST = Path('/tmp/opencode/c0-runtime-images-final-2026-10-17')
BUILD = Path('/tmp/opencode/c0-runtime-images-remediation-2026-10-17')
OUT = ROOT / 'security'
PACKAGE = ROOT / 'snapshot/tests/crawl-jobs-v2-observer'
sys.path.insert(0, str(PACKAGE))
from contracts import canonical, sha
from image_artifacts import ImageTree, archive_record, image_metadata, verify_content
from image_build import ROOT_METADATA_SCRIPT, source_files
from oracle_protocol import SOURCE_FILES

def digest(path):
    with path.open('rb') as file: return hashlib.file_digest(file, 'sha256').hexdigest()

continuity = json.loads((ROOT / 'artifact-revalidation.json').read_bytes())
inventory = json.loads((ROOT / 'source-inventory.json').read_bytes())
baseline = json.loads((BUILD / 'source-inventory.json').read_bytes())
before = json.loads((OUT / 'inputs-before.json').read_bytes())
last_after = json.loads((LAST / 'security/inputs-after.json').read_bytes())
assert before['retained'] == last_after['retained']
assert digest(ROOT / 'artifact-revalidation.json') == '55a7c9f9738d942a745ab47626afe9ef157998039a2d2e8779bee24f992aa3fd'
assert continuity['source_inventory_sha256'] == digest(ROOT / 'source-inventory.json')
assert continuity['validator_test_delta'] == inventory['delta']
assert continuity['runtime_acceptance'] is False and continuity['new_hosted_workflow_executed'] is False
assert continuity['engine_commands_executed'] == 0 and continuity['image_rebuild_required'] is False
current = source_files(PACKAGE)
built = {name.removeprefix('tests/crawl-jobs-v2-observer/'): value for name, value in baseline['files'].items()}
changed = {name for name in current if current[name] != built[name]}
assert changed == {'image_artifacts.py', 'test_image_review.py'}
construction = {'Dockerfile', 'Dockerfile.oracle', '.dockerignore', 'Dockerfile.oracle.dockerignore'}
for ignore in ('.dockerignore', 'Dockerfile.oracle.dockerignore'):
    rows = (PACKAGE / ignore).read_text().splitlines()
    assert rows[0] == '**' and all(row.startswith('!') for row in rows[1:])
    construction |= {name for name in current if any(fnmatch.fnmatchcase(name, row[1:]) for row in rows[1:])}
assert not changed & construction and all(current[name] == built[name] for name in construction)
sources = {name: (PACKAGE / name).read_bytes() for name in (*SOURCE_FILES, 'build_oracle.py')}
result = {'pass': False, 'continuity_sha256': digest(ROOT / 'artifact-revalidation.json'),
          'current_inventory_sha256': digest(ROOT / 'source-inventory.json'),
          'historical_inventory_sha256': digest(BUILD / 'source-inventory.json'),
          'exact_host_only_delta': inventory['delta'], 'construction_inputs_unchanged': sorted(construction),
          'all_retained_entries_match_prior_independent_review': True, 'image_sets': {}, 'recorded_suites': {},
          'engine_or_helper_executions': 0}
for architecture in ('aarch64', 'x86_64'):
    root = Path('/tmp/opencode/c0-images-' + architecture + '-remediation-2026-10-17')
    record = json.loads((root / 'image-set.json').read_bytes())
    linked = continuity['image_sets'][architecture]
    assert digest(root / 'image-set.json') == linked['image_set_sha256']
    assert linked['retained_source_inventory_sha256'] == digest(BUILD / 'source-inventory.json')
    assert linked['construction_inputs_unchanged'] is True
    assert record['source_files'] == built == source_files(root / 'source') and record['source_files'] != current
    assert record['source_snapshot_sha256'] == sha(canonical(built))
    actual = {name: digest(root / name) for name in record['artifacts']}
    assert actual == record['artifacts'] and record['stable_artifacts_sha256'] == sha(canonical(actual))
    manifest = json.loads((root / 'oracle/oracle-artifacts.json').read_bytes())
    assert manifest['files'] == {name: current[name] for name in SOURCE_FILES}
    assert manifest['oracle_worker_source'] == sha(canonical(manifest['files']))
    assert manifest['worker_import_checked'] is True and all(manifest[key] is False for key in ('worker_executed', 'runtime_feasibility_proven', 'execution_authorized'))
    artifacts = {'target': (root / 'native/target').read_bytes(), 'observer': (root / 'native/observer').read_bytes(),
                 'oracle_elf': manifest['oracle_elf'], 'extensions': manifest['extensions']}
    assert record['status'] == 'PASS_STATIC_IMAGE_SET' and record['engine'] == 'podman'
    assert record['runtime_acceptance'] is record['execution_authorized'] is False
    assert record['inspection_containers_started'] == record['c0_actors_started'] == 0
    assert len(record['commands']) == 61 and len(record['cleanup']) == 5 and record['cleanup_complete'] is True
    observations, kinds = defaultdict(list), Counter()
    for command in record['commands']:
        argv = command['argv']; kinds[argv[2]] += 1
        log = root / command['log']
        assert command['returncode'] == 0 and digest(log) == command['log_sha256'] and argv[1] == '--remote=false'
        assert argv[2] not in ('run', 'start', 'exec', 'load', 'kill')
        if argv[2] == 'unshare':
            assert len(argv) == 10 and argv[4:7] == ['-I', '-B', '-c'] and argv[7] == ROOT_METADATA_SCRIPT and argv[8] == argv[0]
            observations[argv[9]].append(json.loads(log.read_bytes()))
        if argv[2] == 'ps': assert not log.read_bytes().strip()
    assert len(observations) == 3 and all(len(values) == 2 for values in observations.values())
    assert all(row['removed'] is True and row['running'] is False and row['pid'] == 0 for row in record['cleanup'])
    entry = {'image_set_sha256': linked['image_set_sha256'], 'source_map_preserved_as_historical': True,
             'commands_verified': 61, 'command_kinds': dict(kinds), 'root_observations_verified': 6,
             'cleanup_records_verified': 5, 'images': {}}
    for role, metadata in record['images'].items():
        value = json.loads((root / (role + '-inspect.json')).read_bytes())
        admitted = image_metadata(value, architecture, role, record['bases']['python']['environment'] if role == 'oracle' else [])
        assert all(metadata[key] == item for key, item in admitted.items())
        assert metadata['inspect_sha256'] == sha(canonical(value))
        cid = metadata['inspection_container']['id'].removeprefix('sha256:')
        root_observation = metadata['inspection_container']['root_directory']
        assert all(value == root_observation for value in observations[cid])
        tree = ImageTree(root / (role + '.tar'), root_observation)
        content = verify_content(tree, architecture, role, artifacts, sources)
        assert canonical(content) == (root / (role + '-content.json')).read_bytes()
        assert sha(canonical(content)) == metadata['content_sha256'] == linked['images'][role]['content_sha256']
        assert content['payload_sha256'] == metadata['payload_sha256'] == linked['images'][role]['payload_sha256']
        portable = archive_record(root / (role + '-image.tar'), metadata['id'], architecture)
        assert portable == metadata['portable_archive'] == linked['images'][role]['portable_archive']
        assert linked['images'][role]['id'] == metadata['id'] and linked['images'][role]['content_bytes_unchanged'] is True
        entry['images'][role] = {'id': metadata['id'], 'content_sha256': metadata['content_sha256'],
                                 'payload_sha256': metadata['payload_sha256'], 'portable_archive': portable,
                                 'content_bytes_unchanged': True, 'root_directory': root_observation,
                                 'hardlinks': {name: tree.resolve(name) for name, row in tree.entries.items() if row.get('hardlink')}}
        del tree
    result['image_sets'][architecture] = entry
previous_ids = set(json.loads((LAST / 'tests-077.json').read_bytes())['inventory'])
for name in ('tests-022.json', 'tests-077.json'):
    report = json.loads((ROOT / name).read_bytes()); ids = set(report['inventory'])
    assert report['status'] == 'PASS' and report['tests_run'] == len(ids) == 287
    assert previous_ids <= ids and len(ids - previous_ids) == 1
    assert report['source_files'] == {'tests/crawl-jobs-v2-observer/' + name: value for name, value in current.items()}
    result['recorded_suites'][name] = {'sha256': digest(ROOT / name), 'source_bound': True, 'tests_run': 287,
                                      'all_286_prior_ids_retained': True, 'added_ids': sorted(ids - previous_ids), 'independently_rerun': False}
result['pass'] = True
with (OUT / 'artifact-checks.json').open('x') as file:
    json.dump(result, file, sort_keys=True, indent=2); file.write('\n')
print(json.dumps({'pass': True, 'images_revalidated': 6, 'historical_source_maps_preserved': True,
                  'commands_rehashed': 122, 'root_observations_rejoined': 12, 'cleanup_records_unchanged': 10}))
