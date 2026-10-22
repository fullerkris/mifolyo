"""Independently join final validator to unchanged, previously built images."""
from collections import Counter, defaultdict
import fnmatch
import hashlib
import json
from pathlib import Path
import sys

ROOT = Path('/tmp/opencode/c0-runtime-images-final-2026-10-17')
PRIOR = Path('/tmp/opencode/c0-runtime-images-remediation-2026-10-17')
OUT = ROOT / 'security'
PACKAGE = ROOT / 'snapshot/tests/crawl-jobs-v2-observer'
sys.path.insert(0, str(PACKAGE))
from contracts import canonical, sha, platform_digest
from image_artifacts import ImageTree, archive_record, image_id, image_metadata, verify_content
from image_build import ROOT_METADATA_SCRIPT, source_files
from oracle_protocol import SOURCE_FILES

def digest(path):
    with path.open('rb') as file:
        return hashlib.file_digest(file, 'sha256').hexdigest()

inventory = json.loads((ROOT / 'source-inventory.json').read_bytes())
old_inventory = json.loads((PRIOR / 'source-inventory.json').read_bytes())
continuity = json.loads((ROOT / 'artifact-revalidation.json').read_bytes())
assert digest(ROOT / 'artifact-revalidation.json') == '40e1106f132e054fa7654a94bba30ff85a3567b738f057527e24bba21704b851'
assert continuity['source_inventory_sha256'] == digest(ROOT / 'source-inventory.json')
assert continuity['validator_test_delta'] == inventory['delta']
assert continuity['engine_commands_executed'] == 0 and continuity['image_rebuild_required'] is False
assert continuity['runtime_acceptance'] is False and continuity['new_hosted_workflow_executed'] is False
final_hashes = source_files(PACKAGE)
old_hashes = {name.removeprefix('tests/crawl-jobs-v2-observer/'): value for name, value in old_inventory['files'].items()}
changed = {name for name in final_hashes if final_hashes[name] != old_hashes[name]}
assert changed == {'image_artifacts.py', 'test_image_review.py'}
construction = {'Dockerfile', 'Dockerfile.oracle', '.dockerignore', 'Dockerfile.oracle.dockerignore'}
for ignore in ('.dockerignore', 'Dockerfile.oracle.dockerignore'):
    rows = (PACKAGE / ignore).read_text().splitlines()
    assert rows[0] == '**' and all(row.startswith('!') for row in rows[1:])
    for name in final_hashes:
        if any(fnmatch.fnmatchcase(name, pattern[1:]) for pattern in rows[1:]): construction.add(name)
assert not changed & construction
assert all(final_hashes[name] == old_hashes[name] for name in construction)
sources = {name: (PACKAGE / name).read_bytes() for name in (*SOURCE_FILES, 'build_oracle.py')}
result = {'pass': False, 'continuity_record_sha256': digest(ROOT / 'artifact-revalidation.json'),
          'final_source_inventory_sha256': digest(ROOT / 'source-inventory.json'),
          'retained_source_inventory_sha256': digest(PRIOR / 'source-inventory.json'),
          'exact_host_only_delta': inventory['delta'], 'unchanged_construction_inputs': sorted(construction),
          'root_helper_sha256': sha(ROOT_METADATA_SCRIPT.encode()), 'engine_or_helper_executions': 0,
          'image_sets': {}, 'recorded_suites': {}}
for arch in ('aarch64', 'x86_64'):
    root = Path('/tmp/opencode/c0-images-' + arch + '-remediation-2026-10-17')
    record = json.loads((root / 'image-set.json').read_bytes())
    linked = continuity['image_sets'][arch]
    assert record['status'] == 'PASS_STATIC_IMAGE_SET' and record['engine'] == 'podman'
    assert digest(root / 'image-set.json') == linked['image_set_sha256']
    assert linked['retained_source_inventory_sha256'] == digest(PRIOR / 'source-inventory.json')
    assert linked['construction_inputs_unchanged'] is True
    # The historical full source map intentionally remains at the old validator.
    assert record['source_files'] == old_hashes == source_files(root / 'source')
    assert record['source_snapshot_sha256'] == sha(canonical(old_hashes))
    assert record['source_files'] != final_hashes
    actual_artifacts = {name: digest(root / name) for name in record['artifacts']}
    assert actual_artifacts == record['artifacts'] and record['stable_artifacts_sha256'] == sha(canonical(actual_artifacts))
    manifest = json.loads((root / 'oracle/oracle-artifacts.json').read_bytes())
    assert manifest['version'] == 3 and manifest['architecture'] == arch and manifest['platform_sha256'] == platform_digest(arch)
    assert manifest['files'] == {name: final_hashes[name] for name in SOURCE_FILES}
    assert manifest['oracle_worker_source'] == sha(canonical(manifest['files']))
    assert manifest['worker_import_checked'] is True and all(manifest[key] is False for key in
        ('worker_executed', 'runtime_feasibility_proven', 'execution_authorized'))
    artifacts = {'target': (root / 'native/target').read_bytes(), 'observer': (root / 'native/observer').read_bytes(),
                 'oracle_elf': manifest['oracle_elf'], 'extensions': manifest['extensions']}
    assert record['runtime_acceptance'] is record['execution_authorized'] is False
    assert record['inspection_containers_started'] == record['c0_actors_started'] == 0
    assert len(record['commands']) == 61 and record['cleanup_complete'] and len(record['cleanup']) == 5
    created, observed, inspections, removed, absent = {}, defaultdict(list), Counter(), [], []
    kinds = Counter()
    for command in record['commands']:
        argv, log = command['argv'], root / command['log']
        assert argv[1] == '--remote=false' and command['returncode'] == 0 and command['log_sha256'] == digest(log)
        operation = argv[2]; kinds[operation] += 1
        assert operation not in ('run', 'start', 'exec', 'kill', 'load')
        if operation == 'info': assert log.read_bytes().strip() == b'true'
        if operation == 'create':
            cid = log.read_text().strip()
            assert len(cid) == 64
            created[cid] = {'image': image_id(argv[-1]), 'name': argv[argv.index('--name') + 1]}
            assert argv[argv.index('--entrypoint') + 1] == '/__obs1_inspection_only__'
        elif operation == 'container':
            rows = json.loads(log.read_bytes()); assert len(rows) == 1 and argv[3] == 'inspect'
            value, cid = rows[0], argv[4]
            assert image_id(value['Id']) == image_id(cid)
            assert image_id(value['Image']) == created[cid]['image'] and value['Name'].lstrip('/') == created[cid]['name']
            state = value['State']
            assert state['Running'] is False and type(state['Pid']) is int and state['Pid'] == 0
            assert state['Status'] in ('created', 'configured') and state['StartedAt'] in ('0001-01-01T00:00:00Z', '0001-01-01T00:00:00+00:00')
            inspections[cid] += 1
        elif operation == 'unshare':
            assert len(argv) == 10 and argv[4:7] == ['-I', '-B', '-c'] and argv[7] == ROOT_METADATA_SCRIPT
            assert argv[8] == argv[0] and argv[9] in created
            observed[argv[9]].append({'log': command['log'], 'value': json.loads(log.read_bytes())})
        elif operation == 'rm':
            assert len(argv) == 4 and argv[3] in created
            removed.append(argv[3])
        elif operation == 'ps':
            assert not log.read_bytes().strip()
            selected = argv[argv.index('--filter') + 1]
            assert selected.startswith('id=') and selected[3:] in removed
            absent.append(selected[3:])
    assert len(created) == len(set(removed)) == len(set(absent)) == 5
    assert all(inspections[cid] == 3 for cid in created)
    assert len(observed) == 3 and all(len(rows) == 2 for rows in observed.values())
    for cleanup in record['cleanup']:
        cid = cleanup['id'].removeprefix('sha256:')
        assert cid in removed and cid in absent and cleanup['removed'] is True and cleanup['running'] is False and cleanup['pid'] == 0
        assert cleanup['image'] == created[cid]['image'] and cleanup['name'] == created[cid]['name']
    entry = {'image_set_sha256': linked['image_set_sha256'], 'historical_source_map_preserved': True,
             'precise_two_file_delta_verified': True, 'construction_inputs_unchanged': True,
             'commands_verified': 61, 'command_kinds': dict(kinds), 'root_observations_verified': 6,
             'stopped_cleanup_records_verified': 5, 'images': {}}
    for role, metadata in record['images'].items():
        inspection = json.loads((root / (role + '-inspect.json')).read_bytes())
        environment = record['bases']['python']['environment'] if role == 'oracle' else []
        admitted = image_metadata(inspection, arch, role, environment)
        assert all(metadata[key] == value for key, value in admitted.items())
        assert metadata['inspect_sha256'] == sha(canonical(inspection))
        cid = metadata['inspection_container']['id'].removeprefix('sha256:')
        root_metadata = metadata['inspection_container']['root_directory']
        assert all(row['value'] == root_metadata for row in observed[cid])
        tree = ImageTree(root / (role + '.tar'), root_metadata)
        checked = verify_content(tree, arch, role, artifacts, sources)
        assert canonical(checked) == (root / (role + '-content.json')).read_bytes()
        portable = archive_record(root / (role + '-image.tar'), metadata['id'], arch)
        assert portable == metadata['portable_archive'] == linked['images'][role]['portable_archive']
        assert linked['images'][role]['id'] == metadata['id'] and linked['images'][role]['content_bytes_unchanged'] is True
        assert sha(canonical(checked)) == metadata['content_sha256'] == linked['images'][role]['content_sha256']
        assert checked['payload_sha256'] == metadata['payload_sha256'] == linked['images'][role]['payload_sha256']
        entry['images'][role] = {'id': metadata['id'], 'content_sha256': metadata['content_sha256'],
                                 'payload_sha256': metadata['payload_sha256'], 'portable_archive': portable,
                                 'content_bytes_unchanged': True, 'root_directory': root_metadata,
                                 'archive_members': tree.member_count}
        del tree
    result['image_sets'][arch] = entry
previous_ids = set(json.loads((PRIOR / 'tests-077.json').read_bytes())['inventory'])
for name in ('tests-022.json', 'tests-077.json'):
    report = json.loads((ROOT / name).read_bytes())
    assert report['status'] == 'PASS' and report['tests_run'] == 286
    assert report['source_files'] == {'tests/crawl-jobs-v2-observer/' + name: value for name, value in final_hashes.items()}
    current_ids = set(report['inventory'])
    assert len(current_ids) == 286 and previous_ids <= current_ids and len(current_ids - previous_ids) == 3
    result['recorded_suites'][name] = {'sha256': digest(ROOT / name), 'tests_run': 286, 'source_bound': True,
                                      'all_283_prior_ids_retained': True, 'added_ids': sorted(current_ids - previous_ids), 'independently_rerun': False}
result['pass'] = True
with (OUT / 'artifact-checks.json').open('x') as file:
    json.dump(result, file, sort_keys=True, indent=2)
    file.write('\n')
print(json.dumps({'pass': True, 'images_revalidated': 6, 'historical_source_maps_not_relabelled': True,
                  'recorded_commands_verified': 122, 'root_observations_verified': 12, 'cleanup_records_verified': 10}))
