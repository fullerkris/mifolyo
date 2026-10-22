"""Read retained files only. Never executes the stored commands or root helper."""
from collections import Counter, defaultdict
import hashlib
import json
from pathlib import Path
import sys

ROOT = Path('/tmp/opencode/c0-runtime-images-remediation-2026-10-17')
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

source_hashes = source_files(PACKAGE)
sources = {name: (PACKAGE / name).read_bytes() for name in (*SOURCE_FILES, 'build_oracle.py')}
old = json.loads(Path('/tmp/opencode/c0-runtime-images-2026-10-17/security/retained-verification.json').read_bytes())
result = {'mode': 'retained-data-only', 'sets': {}, 'reported_suites': {},
          'root_metadata_helper_sha256': sha(ROOT_METADATA_SCRIPT.encode()),
          'root_metadata_helper_executed': False}
for arch in ('aarch64', 'x86_64'):
    root = Path('/tmp/opencode/c0-images-' + arch + '-remediation-2026-10-17')
    record = json.loads((root / 'image-set.json').read_bytes())
    assert record['status'] == 'PASS_STATIC_IMAGE_SET' and record['engine'] == 'podman'
    assert record['source_files'] == source_hashes == source_files(root / 'source')
    assert record['source_snapshot_sha256'] == sha(canonical(source_hashes))
    actual_artifacts = {name: digest(root / name) for name in record['artifacts']}
    assert actual_artifacts == record['artifacts']
    assert record['stable_artifacts_sha256'] == sha(canonical(actual_artifacts))
    assert all(value == old['sets'][arch]['artifacts'][name] for name, value in actual_artifacts.items() if name.startswith('native/'))
    manifest = json.loads((root / 'oracle/oracle-artifacts.json').read_bytes())
    assert manifest['version'] == 3 and manifest['architecture'] == arch and manifest['platform_sha256'] == platform_digest(arch)
    assert manifest['files'] == {name: source_hashes[name] for name in SOURCE_FILES}
    assert manifest['oracle_worker_source'] == sha(canonical(manifest['files']))
    assert manifest['worker_import_checked'] is True
    assert all(manifest[key] is False for key in ('worker_executed', 'runtime_feasibility_proven', 'execution_authorized'))
    artifacts = {'target': (root / 'native/target').read_bytes(), 'observer': (root / 'native/observer').read_bytes(),
                 'oracle_elf': manifest['oracle_elf'], 'extensions': manifest['extensions']}
    assert len(record['commands']) == 61 and len(record['cleanup']) == 5
    assert record['cleanup_complete'] and record['inspection_containers_started'] == record['c0_actors_started'] == 0
    assert record['runtime_acceptance'] is record['execution_authorized'] is False
    root_observations, inspections, created, removal_ids = defaultdict(list), defaultdict(list), {}, []
    command_kinds = Counter()
    for command in record['commands']:
        argv, log = command['argv'], root / command['log']
        assert command['returncode'] == 0 and command['log_sha256'] == digest(log)
        assert argv[1] == '--remote=false'
        operation = argv[2]
        assert operation not in ('run', 'start', 'exec', 'load', 'kill')
        command_kinds[operation] += 1
        if operation == 'info': assert log.read_bytes().strip() == b'true'
        if operation == 'create':
            cid = log.read_text().strip()
            assert len(cid) == 64
            created[cid] = {'name': argv[argv.index('--name') + 1], 'image': argv[-1]}
            assert argv[argv.index('--entrypoint') + 1] == '/__obs1_inspection_only__'
            assert argv[argv.index('--network') + 1] == 'none' and '--read-only' in argv and '--no-healthcheck' in argv
        if operation == 'container':
            assert argv[3] == 'inspect'
            rows = json.loads(log.read_bytes()); assert len(rows) == 1
            value = rows[0]; cid = argv[4]
            assert value['Id'].removeprefix('sha256:') == cid
            assert image_id(value['Image']) == image_id(created[cid]['image'])
            assert value['Name'].lstrip('/') == created[cid]['name']
            state = value['State']
            assert state['Running'] is False and type(state['Pid']) is int and state['Pid'] == 0
            assert state['Status'] in ('created', 'configured')
            assert state['StartedAt'] in ('0001-01-01T00:00:00Z', '0001-01-01T00:00:00+00:00')
            inspections[cid].append(command['log'])
        if operation == 'unshare':
            assert argv[4:7] == ['-I', '-B', '-c'] and argv[7] == ROOT_METADATA_SCRIPT
            assert argv[8] == argv[0] and argv[9] in created and len(argv) == 10
            root_observations[argv[9]].append({'log': command['log'], 'value': json.loads(log.read_bytes())})
        if operation == 'rm':
            assert argv[3] in created and len(argv) == 4
            removal_ids.append(argv[3])
        if operation == 'ps': assert not log.read_bytes().strip()
    assert len(created) == len(set(removal_ids)) == 5 and len(root_observations) == 3
    assert all(len(value) == 3 for value in inspections.values())
    for row in record['cleanup']:
        cid = row['id'].removeprefix('sha256:')
        assert cid in removal_ids and row['removed'] is True and row['running'] is False and row['pid'] == 0
        assert row['name'] == created[cid]['name'] and row['image'] == created[cid]['image']
    entry = {'image_set_sha256': digest(root / 'image-set.json'), 'commands_verified': len(record['commands']),
             'command_kinds': dict(command_kinds), 'cleanup_records_verified': 5,
             'source_files_verified': len(source_hashes), 'native_artifact_hashes_unchanged': True,
             'builder_version': record['builder_version'], 'root_observations': dict(root_observations), 'images': {}}
    for role, recorded in record['images'].items():
        meta = json.loads((root / (role + '-inspect.json')).read_bytes())
        env = record['bases']['python']['environment'] if role == 'oracle' else []
        admitted = image_metadata(meta, arch, role, env)
        assert all(recorded[key] == value for key, value in admitted.items())
        assert recorded['inspect_sha256'] == sha(canonical(meta))
        cid = recorded['inspection_container']['id'].removeprefix('sha256:')
        observations = root_observations[cid]
        root_metadata = recorded['inspection_container']['root_directory']
        assert len(observations) == 2 and all(row['value'] == root_metadata for row in observations)
        tree = ImageTree(root / (role + '.tar'), root_metadata)
        content = verify_content(tree, arch, role, artifacts, sources)
        assert canonical(content) == (root / (role + '-content.json')).read_bytes()
        assert recorded['content_sha256'] == sha(canonical(content)) and recorded['payload_sha256'] == content['payload_sha256']
        portable = archive_record(root / (role + '-image.tar'), recorded['id'], arch)
        assert portable == recorded['portable_archive']
        entry['images'][role] = {'id': recorded['id'], 'content_sha256': recorded['content_sha256'],
                                 'root_directory': root_metadata, 'members': tree.member_count,
                                 'hardlinks': sum(row.get('hardlink', False) for row in tree.entries.values()),
                                 'portable': portable, 'elf_files': len(content['elfs'])}
        del tree
    result['sets'][arch] = entry
old_ids = set(json.loads(Path('/tmp/opencode/c0-runtime-images-2026-10-17/tests-077.json').read_bytes())['inventory'])
for name in ('tests-022.json', 'tests-077.json'):
    path = ROOT / name
    report = json.loads(path.read_bytes())
    assert report['status'] == 'PASS' and report['tests_run'] == 283
    assert report['source_files'] == {'tests/crawl-jobs-v2-observer/' + name: value for name, value in source_hashes.items()}
    ids = set(report['inventory'])
    assert len(ids) == 283 and old_ids <= ids and len(ids - old_ids) == 11
    result['reported_suites'][name] = {'sha256': digest(path), 'tests_run': 283, 'source_bound': True,
                                      'all_272_prior_ids_retained': True, 'added_ids': sorted(ids - old_ids), 'independently_rerun': False}
result['pass'] = True
with (OUT / 'corrected-set-checks.json').open('x') as file:
    json.dump(result, file, sort_keys=True, indent=2)
    file.write('\n')
print(json.dumps({'pass': True, 'runtime_images': 6, 'recorded_commands_verified': 122,
                  'recorded_root_observations_verified': 12, 'recorded_cleanups_verified': 10}))
