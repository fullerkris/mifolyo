"""Final-validator replay of retained images; never invoke an engine or helper."""
import hashlib
import json
import posixpath
import sys
import tarfile
from common import OUT, BASE, PACKAGE, PREVIOUS, DURABLE, INVENTORY_SHA, REVALIDATION_SHA, files, hashfile, image_dir, load, sha

sys.path.insert(0, str(PACKAGE))
from contracts import canonical, platform_digest
from image_artifacts import BASES, ImageTree, archive_record, image_metadata, verify_content
from image_build import ROOT_METADATA_SCRIPT, verify_artifacts
from oracle_protocol import SOURCE_FILES


def clean_name(p):
    return posixpath.normpath('/' + p.lstrip('/'))


def read_layer(t):
    rows, whiteouts, roots = {}, [], []
    for m in t:
        p = clean_name(m.name)
        if p == '/':
            roots.append({'uid': m.uid, 'gid': m.gid, 'mode': m.mode})
            continue
        if posixpath.basename(p).startswith('.wh.'):
            whiteouts.append(p)
            continue
        assert p not in rows
        r = {'uid': m.uid, 'gid': m.gid, 'mode': m.mode}
        if m.isfile():
            digest = hashlib.sha256()
            with t.extractfile(m) as f:
                for raw in iter(lambda: f.read(65536), b''):
                    digest.update(raw)
            r.update(kind='file', sha256=digest.hexdigest())
        elif m.isdir():
            r.update(kind='directory')
        elif m.issym() or m.islnk():
            prefix = posixpath.dirname(p) + '/' if m.issym() and not m.linkname.startswith('/') else '/'
            r.update(kind='link', target=prefix + m.linkname, hardlink=m.islnk())
        else:
            raise AssertionError(('special member', p, m.type))
        rows[p] = r
    return rows, whiteouts, roots


def overlay(path, inspected, identity, expected_layers):
    result, details = {}, []
    with tarfile.open(path, 'r:') as t:
        members = t.getmembers()
        assert len({clean_name(m.name) for m in members}) == len(members)
        manifest = json.load(t.extractfile('manifest.json'))
        raw = t.extractfile(manifest[0]['Config']).read()
        assert 'sha256:' + sha(raw) == identity
        config = json.loads(raw)
        assert config['config'] == inspected['Config']
        assert config['rootfs']['type'] == 'layers'
        assert config['rootfs']['diff_ids'] == expected_layers == inspected['RootFS']['Layers']
        for layer in manifest[0]['Layers']:
            with t.extractfile(layer) as f, tarfile.open(fileobj=f, mode='r:') as lt:
                additions, whiteouts, roots = read_layer(lt)
            for wh in whiteouts:
                parent, leaf = posixpath.split(wh)
                target = parent if leaf == '.wh..wh..opq' else parent + '/' + leaf[4:]
                for p in list(result):
                    if p.startswith(target + '/') or (leaf != '.wh..wh..opq' and p == target):
                        del result[p]
            for p, row in additions.items():
                if row['kind'] != 'directory':
                    for child in list(result):
                        if child.startswith(p + '/'):
                            del result[child]
                result[p] = row
            details.append({'layer': layer, 'members': len(additions), 'whiteouts': whiteouts, 'root_headers': roots})
    return result, details


def main():
    inventory = load(BASE / 'source-inventory.json')
    old_inventory = load(PREVIOUS / 'source-inventory.json')
    revalidation = load(BASE / 'artifact-revalidation.json')
    assert hashfile(BASE / 'source-inventory.json') == INVENTORY_SHA
    assert hashfile(BASE / 'artifact-revalidation.json') == REVALIDATION_SHA
    assert hashfile(PREVIOUS / 'source-inventory.json') == inventory['prior_source_inventory_sha256']
    current = inventory['files'] | inventory['auxiliary_files']
    old = old_inventory['files'] | old_inventory['auxiliary_files']
    assert current.keys() == old.keys()
    delta = {p: {'before': old[p], 'after': current[p]} for p in current if current[p] != old[p]}
    assert delta == inventory['delta'] == revalidation['validator_test_delta']
    assert set(delta) == {'tests/crawl-jobs-v2-observer/image_artifacts.py', 'tests/crawl-jobs-v2-observer/test_image_review.py'}
    assert revalidation['source_inventory_sha256'] == INVENTORY_SHA
    assert revalidation['status'] == 'PASS_REVALIDATED_RETAINED_IMAGES' and revalidation['engine_commands_executed'] == 0
    assert not revalidation['image_rebuild_required'] and not revalidation['runtime_acceptance'] and not revalidation['new_hosted_workflow_executed']
    want = {p.removeprefix('tests/crawl-jobs-v2-observer/'): v for p, v in inventory['files'].items()}
    retained = {p.removeprefix('tests/crawl-jobs-v2-observer/'): v for p, v in old_inventory['files'].items()}
    assert files(PACKAGE) == want
    previous_report = load(PREVIOUS / 'correctness/review.json')
    assert hashfile(PREVIOUS / 'correctness/review.json') == '83a7c544781d0b0fde3ce6030f5c073826a7cf3fb7f029d5cdd3cf6b7d0f664f'
    before = load(OUT / 'inventory-before.json')
    result = {'status': 'PASS', 'source_delta': delta, 'artifact_revalidation_sha256': REVALIDATION_SHA,
              'actual_images': {}, 'actual_loader_negative_controls': [], 'durable_retention': {}, 'tests': []}
    construction_inputs = {'Dockerfile', '.dockerignore', 'contracts.py', 'profiles.py', 'build_tools.py',
                           'Dockerfile.oracle', 'Dockerfile.oracle.dockerignore', 'build_oracle.py', *SOURCE_FILES}
    construction_inputs.update(p for p in want if p.startswith('native/') and p.endswith(('.h', '.c')))
    assert all(want[p] == retained[p] for p in construction_inputs)
    assert not set(delta) & {'tests/crawl-jobs-v2-observer/' + p for p in construction_inputs}
    result['unchanged_construction_inputs'] = {p: want[p] for p in sorted(construction_inputs)}
    result['unchanged_driver_and_auxiliary'] = {p: current[p] for p in ['tests/crawl-jobs-v2-observer/image_build.py', *inventory['auxiliary_files']]}
    for p, expected in result['unchanged_driver_and_auxiliary'].items():
        assert old[p] == expected
    for arch in ('aarch64', 'x86_64'):
        directory = image_dir(arch)
        hashes = files(directory)
        assert hashes == before['protected_roots'][str(directory)] == previous_report['input_evidence_hashes'][str(directory)]
        s = load(directory / 'image-set.json')
        r = revalidation['image_sets'][arch]
        assert hashes['image-set.json'] == r['image_set_sha256'] == previous_report['actual_images'][arch]['image_set_sha256']
        assert s['architecture'] == arch and s['platform_sha256'] == platform_digest(arch) and s['status'] == 'PASS_STATIC_IMAGE_SET'
        assert s['engine'] == 'podman' and not s['runtime_acceptance'] and not s['execution_authorized']
        assert s['c0_actors_started'] == s['inspection_containers_started'] == 0
        # Retained maps intentionally describe the 14ac snapshot, not the final
        # host-side validator. Only the declared two-file delta is permitted.
        assert files(directory / 'source') == s['source_files'] == retained
        assert s['source_snapshot_sha256'] == sha(canonical(retained))
        assert {p for p in want if want[p] != retained[p]} == {'image_artifacts.py', 'test_image_review.py'}
        assert r['retained_source_inventory_sha256'] == inventory['prior_source_inventory_sha256'] and r['construction_inputs_unchanged']
        artifacts = verify_artifacts(directory / 'native', directory / 'oracle', want, arch)
        assert all(hashes[p] == expected for p, expected in s['artifacts'].items())
        assert s['stable_artifacts_sha256'] == sha(canonical(s['artifacts']))
        for p in s['artifacts']:
            assert hashes[p] == previous_report['input_evidence_hashes'][str(directory)][p]
        bases = {}
        for kind, index in (('compiler', 3), ('python', 5)):
            value = load(directory / f'command-{index:03}.log')[0]; bases[kind] = value
            assert s['bases'][kind]['reference'] == BASES[arch][kind] and BASES[arch][kind] in value['RepoDigests']
            assert value['Id'].removeprefix('sha256:') == s['bases'][kind]['id'].removeprefix('sha256:')
            assert value['Config']['Env'] == s['bases'][kind]['environment']
        commands = s['commands']; counts = {}
        assert len(commands) == 61
        for cmd in commands:
            assert hashes[cmd['log']] == cmd['log_sha256'] and cmd['returncode'] == 0
            a = cmd['argv']; assert a[:2] == ['/usr/bin/podman', '--remote=false']
            verb = a[2]; counts[verb] = counts.get(verb, 0) + 1
            assert verb not in ('start', 'run', 'exec', 'kill')
            if verb == 'unshare':
                assert a[4:7] == ['-I', '-B', '-c'] and a[7] == ROOT_METADATA_SCRIPT and a[8] == '/usr/bin/podman'
                assert len(a) == 10 and len(a[9]) == 64
            if verb == 'create':
                assert a[a.index('--entrypoint') + 1] == '/__obs1_inspection_only__'
        assert (directory / 'command-000.log').read_bytes().strip() == b'true'
        assert s['cleanup_complete'] and len(s['cleanup']) == r['stopped_cleanup_records'] == 5
        assert len({proof['id'] for proof in s['cleanup']}) == 5
        for proof in s['cleanup']:
            assert proof['removed'] and not proof['running'] and proof['pid'] == 0
            cid = proof['id'].removeprefix('sha256:')
            inspected = [cmd for cmd in commands if cmd['argv'][2:4] == ['container', 'inspect'] and cmd['argv'][-1] == cid]
            assert len(inspected) == 3
            for cmd in inspected:
                value = load(directory / cmd['log'])[0]
                assert value['Id'].removeprefix('sha256:') == cid and value['Name'].lstrip('/') == proof['name']
                assert value['Image'].removeprefix('sha256:') == proof['image'].removeprefix('sha256:')
                state = value['State']
                assert state['Running'] is False and type(state['Pid']) is int and state['Pid'] == 0
                assert state['Status'] in ('created', 'configured') and state['StartedAt'] in ('0001-01-01T00:00:00Z', '0001-01-01T00:00:00+00:00')
            absence = [cmd for cmd in commands if cmd['argv'][2] == 'ps' and 'id=' + cid in cmd['argv']]
            assert len(absence) == 1 and not (directory / absence[0]['log']).read_bytes().strip()
        roles = {}
        for role, image in s['images'].items():
            inspected = load(directory / f'{role}-inspect.json')
            env = s['bases']['python']['environment'] if role == 'oracle' else []
            admitted = image_metadata(inspected, arch, role, env)
            assert all(image[k] == v for k, v in admitted.items())
            assert hashes[f'{role}-inspect.json'] == image['inspect_sha256']
            assert inspected['RootFS']['Layers'][:-1] == bases['python']['RootFS']['Layers'] if role == 'oracle' else len(inspected['RootFS']['Layers']) == 1
            cid = image['inspection_container']['id'].removeprefix('sha256:')
            root = image['inspection_container']['root_directory']
            assert root == {'uid': 0, 'gid': 0, 'mode': 0o755 if role == 'oracle' else 0o555}
            observations = [i for i, cmd in enumerate(commands) if cmd['argv'][2] == 'unshare' and cmd['argv'][-1] == cid]
            exports = [i for i, cmd in enumerate(commands) if cmd['argv'][2] == 'export' and cmd['argv'][-1] == cid]
            assert len(observations) == 2 and len(exports) == 1 and observations[0] < exports[0] < observations[1]
            for i in observations: assert load(directory / commands[i]['log']) == root
            t = ImageTree(directory / f'{role}.tar', root)
            source_bytes = {p: (PACKAGE / p).read_bytes() for p in (*SOURCE_FILES, 'build_oracle.py')}
            content = verify_content(t, arch, role, artifacts, source_bytes)
            encoded = canonical(content)
            assert encoded == (directory / f'{role}-content.json').read_bytes()
            assert sha(encoded) == image['content_sha256'] == r['images'][role]['content_sha256']
            assert content['payload_sha256'] == image['payload_sha256'] == r['images'][role]['payload_sha256']
            assert image['id'] == r['images'][role]['id'] and r['images'][role]['content_bytes_unchanged']
            assert content['root_directory'] == root and len(content['elfs']) == image['elf_files']
            saved = archive_record(directory / f'{role}-image.tar', image['id'], arch)
            assert saved == image['portable_archive'] == r['images'][role]['portable_archive']
            overlaid, layers = overlay(directory / f'{role}-image.tar', inspected, image['id'], saved['layers'])
            flat = {p: {k: v for k, v in row.items() if k != 'raw'} for p, row in t.entries.items()}
            assert overlaid == flat
            closure = {p: e for p, e in content['elfs'].items() if e['required_dependency_closure']}
            assert all(not e['unresolved_dependencies'] for e in closure.values())
            roles[role] = {'id': image['id'], 'root': root, 'root_log_indices': observations, 'portable_archive': saved,
                           'content_sha256': sha(encoded), 'content_bytes_identical': True, 'payload_sha256': content['payload_sha256'],
                           'elf_count': len(content['elfs']), 'required_closure_nodes': len(closure), 'overlay_differences': 0,
                           'layer_details': layers, 'optional_unresolved': {p: e['unresolved_dependencies'] for p, e in content['elfs'].items() if e['unresolved_dependencies']}}
            if role == 'oracle':
                loader = content['elfs'][t.resolve('/usr/local/bin/python3')]['dependencies']['PT_INTERP']
                mode = t.entries[loader]['mode']; assert mode & 1
                t.entries[loader]['mode'] = 0o644  # RAM-only mutation, never archive bytes.
                try:
                    verify_content(t, arch, role, artifacts, source_bytes)
                except Exception as e:
                    refusal = {'exception': type(e).__name__, 'reason': str(e)}
                else:
                    raise AssertionError('non-executable real loader accepted')
                finally:
                    t.entries[loader]['mode'] = mode
                assert refusal['reason'] == 'IMAGE_FILE_EXECUTABLE'
                assert canonical(verify_content(t, arch, role, artifacts, source_bytes)) == encoded
                result['actual_loader_negative_controls'].append({'architecture': arch, 'loader': loader, 'positive_mode': oct(mode), 'negative_mode': '0o644', 'refusal': refusal, 'restored_positive': True})
        result['actual_images'][arch] = {'image_set_sha256': hashes['image-set.json'], 'command_counts': counts, 'roles': roles,
            'retained_source_inventory_sha256': inventory['prior_source_inventory_sha256'], 'retained_source_snapshot_sha256': s['source_snapshot_sha256'],
            'native_artifacts_unchanged': 11, 'cleanup_records': 5}
    # Durable copies are checked against their own manifest AND the reviewed
    # temporary sets; they deliberately retain the original construction source.
    retention = load(DURABLE / 'retention.json')
    assert retention['source_inventory_sha256'] == inventory['prior_source_inventory_sha256']
    assert hashfile(DURABLE / 'source-inventory.json') == inventory['prior_source_inventory_sha256']
    assert files(DURABLE / 'source') == old
    for name, record in retention['files'].items():
        p = DURABLE / name
        assert p.stat().st_size == record['bytes'] and hashfile(p) == record['sha256']
        if name.split('/')[0] in ('aarch64', 'x86_64'):
            arch, relative = name.split('/', 1)
            assert hashfile(image_dir(arch) / relative) == record['sha256']
    assert set(files(DURABLE)) == set(retention['files']) | {'retention.json'}
    result['durable_retention'] = {'directory': str(DURABLE), 'retention_sha256': hashfile(DURABLE / 'retention.json'),
                                   'declared_files_verified': len(retention['files']), 'copies_equal_retained_images': True, 'source_matches_retained_inventory': True}
    old_tests = load(PREVIOUS / 'tests-022.json')
    for mask in ('022', '077'):
        p = BASE / f'tests-{mask}.json'; tests = load(p)
        assert tests['status'] == 'PASS' and tests['tests_run'] == 286
        assert tests['inventory'] == tests['started'] == tests['passed']
        assert len(tests['inventory']) == len(set(tests['inventory'])) == 286
        assert not any(tests[k] for k in ('errors', 'failures', 'skipped', 'expected_failures', 'unexpected_successes'))
        assert tests['source_files'] == inventory['files'] and tests['runner_sha256'] == inventory['auxiliary_files']['scripts/test-crawl-jobs-v2-observer.py']
        assert len(old_tests['inventory']) == 283 and set(old_tests['inventory']) <= set(tests['inventory'])
        result['tests'].append({'umask': mask, 'tests_run': 286, 'retained_prior_ids': 283, 'report_sha256': hashfile(p),
                                'log_sha256': hashfile(p.with_suffix('.log')), 'new_ids': sorted(set(tests['inventory']) - set(old_tests['inventory']))})
    (OUT / 'replay.json').write_text(json.dumps(result, indent=2, sort_keys=True) + '\n')
    print(json.dumps({'status': result['status'], 'construction_inputs': len(construction_inputs), 'source_delta': list(delta),
        'six_content_records_byte_identical': True, 'overlay_differences': 0, 'loader_refusals': result['actual_loader_negative_controls'],
        'durable_retention': result['durable_retention'], 'tests': result['tests']}, indent=2))


if __name__ == '__main__':
    main()
