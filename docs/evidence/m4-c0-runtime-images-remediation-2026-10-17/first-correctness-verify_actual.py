"""Read-only corrected archive replay, independent overlay joins and log checks."""
import copy
import json
import posixpath
import sys
import tarfile
from review_common import OUT, BASE, PACKAGE, REPO, OLD, files, hashfile, load, sha

sys.path.insert(0, str(PACKAGE))
from contracts import canonical, platform_digest
from image_artifacts import BASES, ImageTree, archive_record, image_metadata, verify_content
from image_build import ROOT_METADATA_SCRIPT, verify_artifacts
from oracle_protocol import SOURCE_FILES


def canonical_name(p):
    return posixpath.normpath('/' + p.lstrip('/'))


def tar_rows(t):
    rows, whiteouts, roots = {}, [], []
    for m in t:
        p = canonical_name(m.name)
        if p == '/':
            roots.append({'uid': m.uid, 'gid': m.gid, 'mode': m.mode})
            continue
        if posixpath.basename(p).startswith('.wh.'):
            whiteouts.append(p)
            continue
        assert p not in rows
        r = {'uid': m.uid, 'gid': m.gid, 'mode': m.mode}
        if m.isfile():
            import hashlib
            h = hashlib.sha256()
            with t.extractfile(m) as f:
                for raw in iter(lambda: f.read(65536), b''):
                    h.update(raw)
            r.update(kind='file', sha256=h.hexdigest())
        elif m.isdir():
            r.update(kind='directory')
        elif m.issym() or m.islnk():
            prefix = posixpath.dirname(p) + '/' if m.issym() and not m.linkname.startswith('/') else '/'
            r.update(kind='link', target=prefix + m.linkname, hardlink=m.islnk())
        else:
            raise AssertionError(('special member', p, m.type))
        rows[p] = r
    return rows, whiteouts, roots


def portable_rows(path, inspect, identity, expected):
    result, details = {}, []
    with tarfile.open(path, 'r:') as t:
        ms = t.getmembers()
        assert len({canonical_name(m.name) for m in ms}) == len(ms)
        manifest = json.load(t.extractfile('manifest.json'))
        raw = t.extractfile(manifest[0]['Config']).read()
        assert 'sha256:' + sha(raw) == identity
        cfg = json.loads(raw)
        assert cfg['config'] == inspect['Config']
        assert cfg['rootfs']['type'] == 'layers'
        assert cfg['rootfs']['diff_ids'] == expected == inspect['RootFS']['Layers']
        for layer in manifest[0]['Layers']:
            with t.extractfile(layer) as f, tarfile.open(fileobj=f, mode='r:') as lt:
                additions, whiteouts, roots = tar_rows(lt)
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


def check_permissions(t, content):
    checked = {}
    assert t.root['uid'] == t.root['gid'] == 0 and t.root['mode'] & 5 == 5
    wanted = set(content['declared_files']) | {p for p, e in content['elfs'].items() if e['required_dependency_closure']}
    for p in wanted:
        rp = t.resolve(p); row = t.entries[rp]
        assert row['uid'] == row['gid'] == 0 and row['mode'] & 4
        parent = posixpath.dirname(rp)
        while parent != '/':
            dr = t.entries[t.resolve(parent)]
            assert dr['mode'] & 5 == 5
            parent = posixpath.dirname(parent)
        checked[p] = oct(row['mode'])
    executable = '/usr/local/bin/python3' if content['role'] == 'oracle' else '/' + content['role']
    assert t.entries[t.resolve(executable)]['mode'] & 1
    interp_paths = {e['interpreter'] for e in content['elfs'].values() if e['required_dependency_closure'] and e['interpreter']}
    for p in interp_paths:
        assert t.entries[t.resolve(p)]['mode'] & 1, ('actual PT_INTERP lacks execute', p)
    return {'required_modes': checked, 'root_mode': oct(t.root['mode']), 'pt_interp_execute_checked': sorted(interp_paths)}


def main():
    inventory = load(BASE / 'source-inventory.json')
    wanted = {p.removeprefix('tests/crawl-jobs-v2-observer/'): v for p, v in inventory['files'].items()}
    assert files(PACKAGE) == wanted
    result = {'status': 'PASS_ACTUAL_ARTIFACT_JOINS', 'images': {}, 'evidence_hashes': {}, 'adjacent_permission_probes': []}
    for arch in ('aarch64', 'x86_64'):
        directory = __import__('pathlib').Path(f'/tmp/opencode/c0-images-{arch}-remediation-2026-10-17')
        hashes = files(directory)
        result['evidence_hashes'][str(directory)] = hashes
        s = load(directory / 'image-set.json')
        assert s['status'] == 'PASS_STATIC_IMAGE_SET' and s['architecture'] == arch and s['engine'] == 'podman'
        assert s['platform_sha256'] == platform_digest(arch)
        assert not s['runtime_acceptance'] and not s['execution_authorized']
        assert s['c0_actors_started'] == s['inspection_containers_started'] == 0
        assert files(directory / 'source') == s['source_files'] == wanted
        assert s['source_snapshot_sha256'] == sha(canonical(wanted))
        assert all(hashes[p] == v for p, v in s['artifacts'].items())
        assert s['stable_artifacts_sha256'] == sha(canonical(s['artifacts']))
        prior_path = REPO / f'docs/evidence/m4-c0-x64-runtime-2026-10-12/{arch}-build.json'
        prior = load(prior_path)
        result['evidence_hashes'][str(prior_path)] = hashfile(prior_path)
        assert {p.removeprefix('native/'): v for p, v in s['artifacts'].items() if p.startswith('native/')} == prior['artifacts']
        artifacts = verify_artifacts(directory / 'native', directory / 'oracle', wanted, arch)
        bases = {}
        for kind, index in (('compiler', 3), ('python', 5)):
            value = load(directory / f'command-{index:03}.log')[0]
            bases[kind] = value
            assert s['bases'][kind]['reference'] == BASES[arch][kind]
            assert BASES[arch][kind] in value['RepoDigests']
            assert value['Id'].removeprefix('sha256:') == s['bases'][kind]['id'].removeprefix('sha256:')
            assert value['Config']['Env'] == s['bases'][kind]['environment']
            assert value['Os'] == 'linux' and value['Architecture'] == ('arm64' if arch == 'aarch64' else 'amd64')
        commands = s['commands']; counts = {}
        assert len(commands) == 61 and len(s['cleanup']) == 5 and s['cleanup_complete']
        assert (directory / 'command-000.log').read_bytes().strip() == b'true'
        assert (directory / 'command-001.log').read_text() == s['builder_version']
        for command in commands:
            assert command['returncode'] == 0 and hashes[command['log']] == command['log_sha256']
            a = command['argv']; assert a[:2] == ['/usr/bin/podman', '--remote=false']
            verb = a[2]; counts[verb] = counts.get(verb, 0) + 1
            assert verb not in ('run', 'start', 'exec', 'kill')
            if verb == 'build':
                assert '--no-cache' in a and '--pull=never' in a and '--identity-label=false' in a
                assert a[a.index('--network') + 1] == 'none'
                assert ('--unsetenv' in a) == (a[a.index('--target') + 1] in ('target', 'observer'))
            if verb == 'create':
                for flag, value in (('--entrypoint', '/__obs1_inspection_only__'), ('--network', 'none'), ('--user', '999:999'), ('--cap-drop', 'ALL'), ('--security-opt', 'no-new-privileges')):
                    assert a[a.index(flag) + 1] == value
                assert '--read-only' in a and '--no-healthcheck' in a
            if verb == 'unshare':
                assert a[3].startswith('/') and a[4:7] == ['-I', '-B', '-c'] and a[7] == ROOT_METADATA_SCRIPT
                assert a[8] == '/usr/bin/podman' and len(a[9]) == 64 and len(a) == 10
        assert counts['build'] == counts['create'] == counts['rm'] == 5 and counts['unshare'] == 6
        for proof in s['cleanup']:
            assert proof['removed'] and not proof['running'] and proof['pid'] == 0
            cid = proof['id'].removeprefix('sha256:')
            inspections = [cmd for cmd in commands if cmd['argv'][2:4] == ['container', 'inspect'] and cmd['argv'][-1] == cid]
            assert len(inspections) == 3
            for cmd in inspections:
                value = load(directory / cmd['log'])[0]
                assert value['Id'].removeprefix('sha256:') == cid
                assert value['Image'].removeprefix('sha256:') == proof['image'].removeprefix('sha256:')
                assert value['Name'].lstrip('/') == proof['name']
                state = value['State']
                assert state['Running'] is False and type(state['Pid']) is int and state['Pid'] == 0
                assert state['Status'] in ('created', 'configured')
                assert state['StartedAt'] in ('0001-01-01T00:00:00Z', '0001-01-01T00:00:00+00:00')
            absence = [cmd for cmd in commands if cmd['argv'][2] == 'ps' and 'id=' + cid in cmd['argv']]
            assert len(absence) == 1 and not (directory / absence[0]['log']).read_bytes().strip()
        roles = {}
        for role, image in s['images'].items():
            metadata = load(directory / f'{role}-inspect.json')
            env = s['bases']['python']['environment'] if role == 'oracle' else []
            admitted = image_metadata(metadata, arch, role, env)
            assert all(image[k] == v for k, v in admitted.items())
            assert hashes[f'{role}-inspect.json'] == image['inspect_sha256']
            assert metadata['RootFS']['Layers'][:-1] == bases['python']['RootFS']['Layers'] if role == 'oracle' else len(metadata['RootFS']['Layers']) == 1
            cid = image['inspection_container']['id'].removeprefix('sha256:')
            root = image['inspection_container']['root_directory']
            assert root == {'uid': 0, 'gid': 0, 'mode': 0o755 if role == 'oracle' else 0o555}
            root_indices = [i for i, cmd in enumerate(commands) if cmd['argv'][2] == 'unshare' and cmd['argv'][-1] == cid]
            exports = [i for i, cmd in enumerate(commands) if cmd['argv'][2] == 'export' and cmd['argv'][-1] == cid]
            assert len(root_indices) == 2 and len(exports) == 1 and root_indices[0] < exports[0] < root_indices[1]
            for i in root_indices:
                assert load(directory / commands[i]['log']) == root
            t = ImageTree(directory / f'{role}.tar', root)
            sources = {p: (PACKAGE / p).read_bytes() for p in (*SOURCE_FILES, 'build_oracle.py')}
            content = verify_content(t, arch, role, artifacts, sources)
            assert content == load(directory / f'{role}-content.json')
            assert sha(canonical(content)) == image['content_sha256'] and content['payload_sha256'] == image['payload_sha256']
            assert content['root_directory'] == root and len(content['elfs']) == image['elf_files']
            saved = archive_record(directory / f'{role}-image.tar', image['id'], arch)
            assert saved == image['portable_archive']
            overlaid, layers = portable_rows(directory / f'{role}-image.tar', metadata, image['id'], saved['layers'])
            flat = {p: {k: v for k, v in row.items() if k != 'raw'} for p, row in t.entries.items()}
            assert overlaid == flat, ('portable/flat mismatch', arch, role, list(set(overlaid) ^ set(flat)))
            perms = check_permissions(t, content)
            closure = {p: r for p, r in content['elfs'].items() if r['required_dependency_closure']}
            assert not any(r['unresolved_dependencies'] for r in closure.values())
            roles[role] = {'id': image['id'], 'root': root, 'root_command_indices': root_indices, 'archive': saved,
                           'content_sha256': image['content_sha256'], 'inspect_sha256': image['inspect_sha256'], 'payload_sha256': content['payload_sha256'],
                           'overlay_difference_count': 0, 'layers': layers, 'elf_count': len(content['elfs']), 'required_count': len(closure),
                           'optional_unresolved': {p: r['unresolved_dependencies'] for p, r in content['elfs'].items() if r['unresolved_dependencies']},
                           'permissions': perms}
            if role == 'oracle':
                python = t.resolve('/usr/local/bin/python3')
                loader = content['elfs'][python]['dependencies']['PT_INTERP']
                original_mode = t.entries[loader]['mode']
                t.entries[loader]['mode'] = 0o644  # In-memory row only; archive is never changed.
                try:
                    changed = verify_content(t, arch, role, artifacts, sources)
                    observation = {'accepted': True, 'same_payload_sha256': changed['payload_sha256'] == content['payload_sha256']}
                except Exception as e:
                    observation = {'accepted': False, 'exception': type(e).__name__, 'reason': str(e)}
                finally:
                    t.entries[loader]['mode'] = original_mode
                result['adjacent_permission_probes'].append({'architecture': arch, 'path': loader, 'original_mode': oct(original_mode),
                    'in_memory_mutation': 'mode=0644', 'observation': observation, 'archive_unmodified': True})
        result['images'][arch] = {'image_set_sha256': hashes['image-set.json'], 'roles': roles, 'command_counts': counts,
            'source_snapshot_sha256': s['source_snapshot_sha256'], 'source_files': len(wanted), 'native_prior_matches': len(prior['artifacts']),
            'builder_version': s['builder_version']}
    old_tests = OLD.parent / 'tests-022.json'; old = load(old_tests)
    result['evidence_hashes'][str(old_tests)] = hashfile(old_tests)
    result['test_continuity'] = []
    for mode in ('022', '077'):
        p = BASE / f'tests-{mode}.json'; r = load(p)
        result['evidence_hashes'][str(p)] = hashfile(p)
        result['evidence_hashes'][str(p.with_suffix('.log'))] = hashfile(p.with_suffix('.log'))
        assert r['status'] == 'PASS' and r['tests_run'] == 283 and r['inventory'] == r['passed'] == r['started']
        assert not any(r[k] for k in ('errors', 'failures', 'skipped', 'expected_failures', 'unexpected_successes'))
        assert r['source_files'] == inventory['files']
        assert r['runner_sha256'] == inventory['auxiliary_files']['scripts/test-crawl-jobs-v2-observer.py']
        assert len(old['inventory']) == 272 and set(old['inventory']) <= set(r['inventory'])
        result['test_continuity'].append({'umask': mode, 'tests': 283, 'prior_retained': 272, 'new_ids': sorted(set(r['inventory']) - set(old['inventory']))})
    (OUT / 'actual-verification.json').write_text(json.dumps(result, indent=2, sort_keys=True) + '\n')
    print(json.dumps({'status': result['status'], 'images': {a: {'native_prior_matches': v['native_prior_matches'],
                     'roles': {r: {'elf_count': x['elf_count'], 'required_count': x['required_count'], 'overlay_differences': 0, 'root': x['root']} for r, x in v['roles'].items()}} for a, v in result['images'].items()},
                     'adjacent_permission_probes': result['adjacent_permission_probes'], 'test_continuity': result['test_continuity']}, indent=2))


if __name__ == '__main__':
    main()
