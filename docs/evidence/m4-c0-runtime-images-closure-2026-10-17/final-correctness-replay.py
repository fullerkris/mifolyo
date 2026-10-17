"""Closure-validator replay of retained files only; no engine/helper execution."""
import json
import sys
import tarfile
from pathlib import PurePosixPath
from common import OUT, BASE, PACKAGE, PREVIOUS, GO, CONSTRUCTION, INVENTORY_SHA, REVALIDATION_SHA, files, hashfile, image_dir, load, sha

sys.path.insert(0, str(PACKAGE))
from contracts import canonical
from image_artifacts import ImageTree, archive_record, image_metadata, verify_content
from image_build import ROOT_METADATA_SCRIPT, verify_artifacts
from oracle_protocol import SOURCE_FILES

inventory = load(BASE / 'source-inventory.json')
prior_inventory = load(PREVIOUS / 'source-inventory.json')
construction = load(CONSTRUCTION / 'source-inventory.json')
continuity = load(BASE / 'artifact-revalidation.json')
prior_report = load(PREVIOUS / 'correctness/review.json')
go_report = load(GO / 'correctness/review.json')
assert hashfile(BASE / 'source-inventory.json') == INVENTORY_SHA
assert hashfile(BASE / 'artifact-revalidation.json') == REVALIDATION_SHA
current = inventory['files'] | inventory['auxiliary_files']
previous = prior_inventory['files'] | prior_inventory['auxiliary_files']
built = construction['files'] | construction['auxiliary_files']
assert current.keys() == previous.keys() == built.keys()
delta = {path: {'before': previous[path], 'after': current[path]} for path in current if current[path] != previous[path]}
build_delta = {path: {'before': built[path], 'after': current[path]} for path in current if current[path] != built[path]}
assert set(delta) == set(build_delta) == {'tests/crawl-jobs-v2-observer/image_artifacts.py', 'tests/crawl-jobs-v2-observer/test_image_review.py'}
assert build_delta == inventory['delta'] == continuity['validator_test_delta']
assert continuity['source_inventory_sha256'] == INVENTORY_SHA and continuity['status'] == 'PASS_REVALIDATED_RETAINED_IMAGES'
assert continuity['engine_commands_executed'] == 0 and not continuity['image_rebuild_required'] and not continuity['runtime_acceptance'] and not continuity['new_hosted_workflow_executed']
want = {path.removeprefix('tests/crawl-jobs-v2-observer/'): value for path, value in inventory['files'].items()}
retained = {path.removeprefix('tests/crawl-jobs-v2-observer/'): value for path, value in construction['files'].items()}
assert files(PACKAGE) == want
for path, value in go_report['revalidation']['unchanged_construction_inputs'].items():
    assert want[path] == retained[path] == value
for path, value in go_report['revalidation']['unchanged_driver_and_auxiliary'].items():
    assert current[path] == value
result = {'status': 'PASS', 'delta_from_completion': delta, 'delta_from_construction': build_delta,
          'unchanged_construction_inputs': go_report['revalidation']['unchanged_construction_inputs'],
          'unchanged_driver_and_auxiliary': go_report['revalidation']['unchanged_driver_and_auxiliary'], 'images': {}, 'tests': []}
protected = load(OUT / 'inventory-before.json')['protected_roots']
for arch in ('aarch64', 'x86_64'):
    directory = image_dir(arch); hashes = files(directory); image_set = load(directory / 'image-set.json')
    reference = continuity['image_sets'][arch]
    assert hashes == protected[str(directory)]
    assert hashes['image-set.json'] == reference['image_set_sha256'] == prior_report['checks']['images'][arch]['image_set_sha256']
    assert files(directory / 'source') == image_set['source_files'] == retained
    assert image_set['source_snapshot_sha256'] == sha(canonical(retained))
    assert reference['retained_source_inventory_sha256'] == inventory['prior_source_inventory_sha256']
    assert reference['construction_inputs_unchanged'] and image_set['status'] == 'PASS_STATIC_IMAGE_SET'
    assert image_set['c0_actors_started'] == image_set['inspection_containers_started'] == 0
    assert not image_set['execution_authorized'] and not image_set['runtime_acceptance']
    artifacts = verify_artifacts(directory / 'native', directory / 'oracle', want, arch)
    assert all(hashes[path] == value for path, value in image_set['artifacts'].items())
    assert image_set['stable_artifacts_sha256'] == sha(canonical(image_set['artifacts']))
    commands = image_set['commands']; assert len(commands) == 61
    for command in commands:
        assert command['returncode'] == 0 and hashes[command['log']] == command['log_sha256']
        assert command['argv'][:2] == ['/usr/bin/podman', '--remote=false']
        assert command['argv'][2] not in ('run', 'start', 'exec', 'kill')
        if command['argv'][2] == 'unshare': assert command['argv'][7] == ROOT_METADATA_SCRIPT
    assert image_set['cleanup_complete'] and len(image_set['cleanup']) == reference['stopped_cleanup_records'] == 5
    assert len({row['id'] for row in image_set['cleanup']}) == 5
    for proof in image_set['cleanup']:
        assert proof['removed'] and proof['pid'] == 0 and proof['running'] is False
        cid = proof['id'].removeprefix('sha256:')
        inspected = [command for command in commands if command['argv'][2:4] == ['container', 'inspect'] and command['argv'][-1] == cid]
        assert len(inspected) == 3
        for command in inspected:
            value = load(directory / command['log'])[0]; state = value['State']
            assert value['Id'].removeprefix('sha256:') == cid and value['Name'].lstrip('/') == proof['name']
            assert value['Image'].removeprefix('sha256:') == proof['image'].removeprefix('sha256:')
            assert state['Running'] is False and type(state['Pid']) is int and state['Pid'] == 0
            assert state['Status'] in ('created', 'configured') and state['StartedAt'] in ('0001-01-01T00:00:00Z', '0001-01-01T00:00:00+00:00')
        absent = [command for command in commands if command['argv'][2] == 'ps' and 'id=' + cid in command['argv']]
        assert len(absent) == 1 and not (directory / absent[0]['log']).read_bytes().strip()
    roles = {}
    for role, entry in image_set['images'].items():
        inspected = load(directory / (role + '-inspect.json'))
        environment = image_set['bases']['python']['environment'] if role == 'oracle' else []
        admitted = image_metadata(inspected, arch, role, environment)
        assert all(entry[key] == value for key, value in admitted.items())
        assert entry['inspect_sha256'] == hashes[role + '-inspect.json']
        root = entry['inspection_container']['root_directory']
        assert root == {'uid': 0, 'gid': 0, 'mode': 0o755 if role == 'oracle' else 0o555}
        cid = entry['inspection_container']['id'].removeprefix('sha256:')
        observations = [index for index, command in enumerate(commands) if command['argv'][2] == 'unshare' and command['argv'][-1] == cid]
        exports = [index for index, command in enumerate(commands) if command['argv'][2] == 'export' and command['argv'][-1] == cid]
        assert len(observations) == 2 and len(exports) == 1 and observations[0] < exports[0] < observations[1]
        assert all(load(directory / commands[index]['log']) == root for index in observations)
        tree = ImageTree(directory / (role + '.tar'), root)
        # Independent ancestry check for every level, rather than relying only
        # on the new constructor's direct-parent predicate.
        for name in tree.entries:
            for parent in PurePosixPath(name).parents:
                if str(parent) == '/': continue
                assert tree.entries[str(parent)]['kind'] == 'directory'
        hardlinks = []
        for name, row in tree.entries.items():
            if not row.get('hardlink'): continue
            target = tree.resolve(name)
            assert all(row[key] == tree.entries[target][key] for key in ('mode', 'uid', 'gid'))
            hardlinks.append({'name': name, 'declared_target': row['target'], 'resolved_target': target})
        content = verify_content(tree, arch, role, artifacts, {path: (PACKAGE / path).read_bytes() for path in (*SOURCE_FILES, 'build_oracle.py')})
        assert canonical(content) == (directory / (role + '-content.json')).read_bytes()
        assert sha(canonical(content)) == entry['content_sha256'] == reference['images'][role]['content_sha256']
        assert entry['id'] == reference['images'][role]['id'] and content['payload_sha256'] == reference['images'][role]['payload_sha256']
        saved = archive_record(directory / (role + '-image.tar'), entry['id'], arch)
        assert saved == entry['portable_archive'] == reference['images'][role]['portable_archive']
        assert saved == go_report['checks']['actual_images'][arch]['roles'][role]['portable_archive']
        with tarfile.open(directory / (role + '-image.tar'), 'r:') as archive:
            manifest = json.load(archive.extractfile('manifest.json'))
            raw = archive.extractfile(manifest[0]['Config']).read(); config = json.loads(raw)
            assert 'sha256:' + sha(raw) == entry['id'] and config['config'] == inspected['Config']
            assert config['rootfs']['diff_ids'] == inspected['RootFS']['Layers'] == saved['layers']
        roles[role] = {'id': entry['id'], 'root_directory': root, 'literal_ancestry_entries_checked': len(tree.entries), 'hardlinks': hardlinks,
                       'content_bytes_unchanged': True, 'content_sha256': entry['content_sha256'], 'payload_sha256': content['payload_sha256'],
                       'portable_archive': saved, 'elf_count': len(content['elfs']), 'required_elf_count': sum(row['required_dependency_closure'] for row in content['elfs'].values())}
    result['images'][arch] = {'image_set_sha256': hashes['image-set.json'], 'roles': roles, 'verified_command_logs': 61, 'cleanup_records': 5}
prior_tests = load(PREVIOUS / 'tests-022.json')
assert len(prior_tests['inventory']) == 287
for mask in ('022', '077'):
    path = BASE / ('tests-' + mask + '.json'); tests = load(path)
    assert tests['status'] == 'PASS' and tests['tests_run'] == len(tests['inventory']) == len(set(tests['inventory'])) == 288
    assert tests['inventory'] == tests['started'] == tests['passed']
    assert not any(tests[key] for key in ('errors', 'failures', 'skipped', 'expected_failures', 'unexpected_successes'))
    assert tests['source_files'] == inventory['files'] and tests['runner_sha256'] == inventory['auxiliary_files']['scripts/test-crawl-jobs-v2-observer.py']
    assert set(prior_tests['inventory']) <= set(tests['inventory'])
    result['tests'].append({'umask': mask, 'count': 288, 'retained_ids': 287, 'new_ids': sorted(set(tests['inventory']) - set(prior_tests['inventory'])),
                            'report_sha256': hashfile(path), 'log_sha256': hashfile(path.with_suffix('.log'))})
(OUT / 'replay.json').write_text(json.dumps(result, indent=2, sort_keys=True) + '\n')
print(json.dumps({'status': result['status'], 'content_records_identical': 6, 'construction_inputs_unchanged': len(result['unchanged_construction_inputs']),
                  'delta_from_completion': delta, 'literal_entries_checked': {arch: {role: row['literal_ancestry_entries_checked'] for role, row in data['roles'].items()} for arch, data in result['images'].items()},
                  'tests': result['tests']}, indent=2))
