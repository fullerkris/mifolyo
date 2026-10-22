"""Source continuity and completion-validator replay. No engine/helper calls."""
import json
import sys
import tarfile
from common import OUT, BASE, PACKAGE, GO, CONSTRUCTION, INVENTORY_SHA, REVALIDATION_SHA, files, hashfile, image_dir, load, sha

sys.path.insert(0, str(PACKAGE))
from contracts import canonical
from image_artifacts import ImageTree, archive_record, image_metadata, verify_content
from image_build import verify_artifacts, ROOT_METADATA_SCRIPT
from oracle_protocol import SOURCE_FILES

inventory = load(BASE / 'source-inventory.json')
go_inventory = load(GO / 'source-inventory.json')
construction = load(CONSTRUCTION / 'source-inventory.json')
go_review = load(GO / 'correctness/review.json')
continuity = load(BASE / 'artifact-revalidation.json')
assert hashfile(BASE / 'source-inventory.json') == INVENTORY_SHA
assert hashfile(BASE / 'artifact-revalidation.json') == REVALIDATION_SHA
current = inventory['files'] | inventory['auxiliary_files']
go = go_inventory['files'] | go_inventory['auxiliary_files']
old = construction['files'] | construction['auxiliary_files']
assert current.keys() == go.keys() == old.keys()
delta_go = {p: {'before': go[p], 'after': current[p]} for p in current if current[p] != go[p]}
delta_build = {p: {'before': old[p], 'after': current[p]} for p in current if current[p] != old[p]}
assert set(delta_go) == set(delta_build) == {'tests/crawl-jobs-v2-observer/image_artifacts.py', 'tests/crawl-jobs-v2-observer/test_image_review.py'}
assert delta_build == inventory['delta'] == continuity['validator_test_delta']
assert continuity['source_inventory_sha256'] == INVENTORY_SHA and continuity['status'] == 'PASS_REVALIDATED_RETAINED_IMAGES'
assert not continuity['runtime_acceptance'] and not continuity['image_rebuild_required'] and continuity['engine_commands_executed'] == 0
want = {p.removeprefix('tests/crawl-jobs-v2-observer/'): v for p, v in inventory['files'].items()}
retained = {p.removeprefix('tests/crawl-jobs-v2-observer/'): v for p, v in construction['files'].items()}
assert files(PACKAGE) == want
for p, value in go_review['revalidation']['unchanged_construction_inputs'].items():
    assert want[p] == retained[p] == value
for p, value in go_review['revalidation']['unchanged_driver_and_auxiliary'].items():
    assert current[p] == value
result = {'status': 'PASS_RETAINED_ARTIFACTS', 'delta_from_go': delta_go, 'delta_from_construction': delta_build,
          'construction_inputs_unchanged': go_review['revalidation']['unchanged_construction_inputs'],
          'driver_and_auxiliary_unchanged': go_review['revalidation']['unchanged_driver_and_auxiliary'], 'images': {}, 'tests': []}
protected = load(OUT / 'inventory-before.json')['protected_roots']
for arch in ('aarch64', 'x86_64'):
    root = image_dir(arch); hashes = files(root); image_set = load(root / 'image-set.json')
    previous = go_review['checks']['actual_images'][arch]
    bound = continuity['image_sets'][arch]
    assert hashes == protected[str(root)]
    assert hashes['image-set.json'] == previous['image_set_sha256'] == bound['image_set_sha256']
    assert image_set['source_files'] == files(root / 'source') == retained
    assert image_set['source_snapshot_sha256'] == sha(canonical(retained))
    assert bound['retained_source_inventory_sha256'] == inventory['prior_source_inventory_sha256']
    assert bound['construction_inputs_unchanged'] and image_set['status'] == 'PASS_STATIC_IMAGE_SET'
    artifacts = verify_artifacts(root / 'native', root / 'oracle', want, arch)
    assert all(hashes[p] == value for p, value in image_set['artifacts'].items())
    assert image_set['stable_artifacts_sha256'] == sha(canonical(image_set['artifacts']))
    assert not image_set['execution_authorized'] and not image_set['runtime_acceptance']
    assert image_set['c0_actors_started'] == image_set['inspection_containers_started'] == 0
    commands = image_set['commands']; assert len(commands) == 61
    for cmd in commands:
        assert cmd['returncode'] == 0 and hashes[cmd['log']] == cmd['log_sha256']
        assert cmd['argv'][:2] == ['/usr/bin/podman', '--remote=false']
        assert cmd['argv'][2] not in ('run', 'start', 'exec', 'kill')
        if cmd['argv'][2] == 'unshare':
            assert cmd['argv'][7] == ROOT_METADATA_SCRIPT  # Compare text; never execute it.
    assert image_set['cleanup_complete'] and len(image_set['cleanup']) == bound['stopped_cleanup_records'] == 5
    assert len({r['id'] for r in image_set['cleanup']}) == 5
    for proof in image_set['cleanup']:
        assert proof['removed'] and proof['pid'] == 0 and proof['running'] is False
        cid = proof['id'].removeprefix('sha256:')
        inspected = [c for c in commands if c['argv'][2:4] == ['container', 'inspect'] and c['argv'][-1] == cid]
        assert len(inspected) == 3
        for cmd in inspected:
            value = load(root / cmd['log'])[0]; state = value['State']
            assert value['Id'].removeprefix('sha256:') == cid and value['Name'].lstrip('/') == proof['name']
            assert value['Image'].removeprefix('sha256:') == proof['image'].removeprefix('sha256:')
            assert state['Running'] is False and type(state['Pid']) is int and state['Pid'] == 0
            assert state['Status'] in ('created', 'configured') and state['StartedAt'] in ('0001-01-01T00:00:00Z', '0001-01-01T00:00:00+00:00')
        absent = [c for c in commands if c['argv'][2] == 'ps' and 'id=' + cid in c['argv']]
        assert len(absent) == 1 and not (root / absent[0]['log']).read_bytes().strip()
    roles = {}
    for role, entry in image_set['images'].items():
        inspected = load(root / (role + '-inspect.json'))
        env = image_set['bases']['python']['environment'] if role == 'oracle' else []
        admitted = image_metadata(inspected, arch, role, env)
        assert all(entry[k] == v for k, v in admitted.items())
        assert entry['inspect_sha256'] == hashes[role + '-inspect.json']
        observed_root = entry['inspection_container']['root_directory']
        assert observed_root == {'uid': 0, 'gid': 0, 'mode': 0o755 if role == 'oracle' else 0o555}
        cid = entry['inspection_container']['id'].removeprefix('sha256:')
        observations = [i for i, cmd in enumerate(commands) if cmd['argv'][2] == 'unshare' and cmd['argv'][-1] == cid]
        exports = [i for i, cmd in enumerate(commands) if cmd['argv'][2] == 'export' and cmd['argv'][-1] == cid]
        assert len(observations) == 2 and len(exports) == 1 and observations[0] < exports[0] < observations[1]
        assert all(load(root / commands[i]['log']) == observed_root for i in observations)
        tree = ImageTree(root / (role + '.tar'), observed_root)
        hardlinks = []
        for name, row in tree.entries.items():
            if not row.get('hardlink'): continue
            parts = name.strip('/').split('/')
            parents = ['/' + '/'.join(parts[:i]) for i in range(1, len(parts))]
            assert all(tree.entries[parent]['kind'] == 'directory' for parent in parents)
            resolved = tree.resolve(name)
            assert all(row[key] == tree.entries[resolved][key] for key in ('mode', 'uid', 'gid'))
            hardlinks.append({'name': name, 'target': row['target'], 'resolved': resolved, 'ancestor_directories': parents})
        content = verify_content(tree, arch, role, artifacts, {p: (PACKAGE / p).read_bytes() for p in (*SOURCE_FILES, 'build_oracle.py')})
        assert canonical(content) == (root / (role + '-content.json')).read_bytes()
        assert sha(canonical(content)) == entry['content_sha256'] == bound['images'][role]['content_sha256']
        assert entry['id'] == bound['images'][role]['id'] and content['payload_sha256'] == bound['images'][role]['payload_sha256']
        saved = archive_record(root / (role + '-image.tar'), entry['id'], arch)
        assert saved == entry['portable_archive'] == previous['roles'][role]['portable_archive'] == bound['images'][role]['portable_archive']
        with tarfile.open(root / (role + '-image.tar'), 'r:') as archive:
            manifest = json.load(archive.extractfile('manifest.json'))
            raw = archive.extractfile(manifest[0]['Config']).read(); config = json.loads(raw)
            assert 'sha256:' + sha(raw) == entry['id'] and config['config'] == inspected['Config']
            assert config['rootfs']['diff_ids'] == inspected['RootFS']['Layers'] == saved['layers']
        roles[role] = {'id': entry['id'], 'root_directory': observed_root, 'hardlinks_scanned': sum(bool(row.get('hardlink')) for row in tree.entries.values()),
                       'independent_hardlink_parent_checks': hardlinks,
                       'content_bytes_unchanged': True, 'content_sha256': entry['content_sha256'], 'payload_sha256': content['payload_sha256'],
                       'portable_archive': saved, 'elf_count': len(content['elfs']), 'required_elf_count': sum(e['required_dependency_closure'] for e in content['elfs'].values())}
    result['images'][arch] = {'image_set_sha256': hashes['image-set.json'], 'roles': roles, 'verified_command_logs': 61, 'cleanup_records': 5}
prior_tests = load(GO / 'tests-022.json')
assert len(prior_tests['inventory']) == 286
for mask in ('022', '077'):
    p = BASE / ('tests-' + mask + '.json'); tests = load(p)
    assert tests['status'] == 'PASS' and tests['tests_run'] == len(tests['inventory']) == len(set(tests['inventory'])) == 287
    assert tests['inventory'] == tests['started'] == tests['passed']
    assert not any(tests[k] for k in ('errors', 'failures', 'skipped', 'expected_failures', 'unexpected_successes'))
    assert tests['source_files'] == inventory['files'] and tests['runner_sha256'] == inventory['auxiliary_files']['scripts/test-crawl-jobs-v2-observer.py']
    assert set(prior_tests['inventory']) <= set(tests['inventory'])
    result['tests'].append({'umask': mask, 'count': 287, 'retained_ids': 286, 'new_ids': sorted(set(tests['inventory']) - set(prior_tests['inventory'])),
                            'report_sha256': hashfile(p), 'log_sha256': hashfile(p.with_suffix('.log'))})
(OUT / 'replay.json').write_text(json.dumps(result, indent=2, sort_keys=True) + '\n')
print(json.dumps({'status': result['status'], 'content_records_identical': 6, 'construction_inputs_unchanged': len(result['construction_inputs_unchanged']),
                  'delta_from_go': delta_go, 'hardlinks_scanned': {a: {r: v['hardlinks_scanned'] for r, v in d['roles'].items()} for a, d in result['images'].items()},
                  'tests': result['tests']}, indent=2))
