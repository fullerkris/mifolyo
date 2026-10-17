"""Read-only replay and independent joins. Does not call a container engine."""
import hashlib
import json
import posixpath
from pathlib import Path
import sys
import tarfile

OUT = Path(__file__).resolve().parent
REPO = Path('/var/home/kfuller/Documents/Git_Projects/mifolyo')
ROOT = OUT.parent
PACKAGE = ROOT / 'snapshot/tests/crawl-jobs-v2-observer'
sys.path.insert(0, str(PACKAGE))
from contracts import canonical, platform_digest
from image_artifacts import BASES, ImageTree, archive_record, image_metadata, verify_content
from image_build import verify_artifacts
from oracle_protocol import SOURCE_FILES


def hashfile(p):
    h = hashlib.sha256()
    with p.open('rb') as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()


def h(raw):
    return hashlib.sha256(raw).hexdigest()


def load(p):
    return json.loads(p.read_bytes())


def files(root):
    return {p.relative_to(root).as_posix(): hashfile(p) for p in sorted(root.rglob('*'))
            if p.is_file() and '__pycache__' not in p.parts and p.suffix != '.pyc'}


def name(p):
    return posixpath.normpath('/' + p.lstrip('/'))


def independent_tar_rows(t):
    rows = {}
    whiteouts = []
    for m in t:
        p = name(m.name)
        if p == '/':
            continue
        if posixpath.basename(p).startswith('.wh.'):
            whiteouts.append(p)
            continue
        assert p not in rows, ('duplicate member', p)
        r = {'mode': m.mode, 'uid': m.uid, 'gid': m.gid}
        if m.isfile():
            with t.extractfile(m) as f:
                hh = hashlib.sha256()
                for chunk in iter(lambda: f.read(65536), b''):
                    hh.update(chunk)
            r.update(kind='file', sha256=hh.hexdigest())
        elif m.isdir():
            r.update(kind='directory')
        elif m.issym() or m.islnk():
            target = m.linkname
            if m.issym() and not target.startswith('/'):
                target = posixpath.join(posixpath.dirname(p), target)
            r.update(kind='link', target=name(target))
        else:
            raise AssertionError(('special member', p, m.type))
        rows[p] = r
    return rows, whiteouts


def overlay_archive(path, inspect, identity, expected_layers):
    result, layers = {}, []
    with tarfile.open(path, 'r:') as t:
        ms = t.getmembers()
        assert len({name(m.name) for m in ms}) == len(ms)
        manifest = json.load(t.extractfile('manifest.json'))
        raw = t.extractfile(manifest[0]['Config']).read()
        assert 'sha256:' + h(raw) == identity
        cfg = json.loads(raw)
        assert cfg['config'] == inspect['Config']
        assert cfg['rootfs']['type'] == 'layers'
        assert cfg['rootfs']['diff_ids'] == expected_layers == inspect['RootFS']['Layers']
        for layer in manifest[0]['Layers']:
            with t.extractfile(layer) as f, tarfile.open(fileobj=f, mode='r:') as lt:
                additions, whiteouts = independent_tar_rows(lt)
            for wh in whiteouts:
                parent, leaf = posixpath.split(wh)
                if leaf == '.wh..wh..opq':
                    doomed = [p for p in result if p.startswith(parent + '/')]
                else:
                    target = parent + '/' + leaf[4:]
                    doomed = [p for p in result if p == target or p.startswith(target + '/')]
                for p in doomed:
                    del result[p]
            for p, row in additions.items():
                if row['kind'] != 'directory':
                    for child in list(result):
                        if child.startswith(p + '/'):
                            del result[child]
                result[p] = row
            layers.append({'path': layer, 'members': len(additions), 'whiteouts': whiteouts})
    return result, layers


def access_checks(tree, content):
    checked = []
    wanted = dict(content['declared_files'])
    wanted.update({p: r['sha256'] for p, r in content['elfs'].items() if r['required_dependency_closure']})
    for p in wanted:
        rp = tree.resolve(p)
        row = tree.entries[rp]
        assert row['mode'] & 0o004, ('not world-readable', p, oct(row['mode']))
        parent = posixpath.dirname(rp)
        while parent != '/':
            dr = tree.entries[tree.resolve(parent)]
            assert dr['mode'] & 0o001, ('not searchable', parent, oct(dr['mode']))
            parent = posixpath.dirname(parent)
        checked.append(p)
    executable = '/' + content['role'] if content['role'] != 'oracle' else '/usr/local/bin/python3'
    assert tree.entries[tree.resolve(executable)]['mode'] & 0o001
    return checked


def main():
    result = {'kind': 'independent_image_evidence_replay', 'checks': {}, 'images': {}, 'evidence_hashes': {}}
    inventory = load(ROOT / 'source-inventory.json')
    expected = {p.removeprefix('tests/crawl-jobs-v2-observer/'): v for p, v in inventory['files'].items()}
    assert files(PACKAGE) == expected
    for arch in ('aarch64', 'x86_64'):
        directory = Path(f'/tmp/opencode/c0-images-{arch}-pass5-2026-10-17')
        hashes = files(directory)
        result['evidence_hashes'][str(directory)] = hashes
        s = load(directory / 'image-set.json')
        assert s['status'] == 'PASS_STATIC_IMAGE_SET' and s['architecture'] == arch
        assert s['platform_sha256'] == platform_digest(arch)
        assert not s['execution_authorized'] and not s['runtime_acceptance']
        assert s['c0_actors_started'] == s['inspection_containers_started'] == 0
        assert s['cleanup_complete'] and len(s['cleanup']) == 5
        assert len({p['id'] for p in s['cleanup']}) == 5
        assert all(p['removed'] and not p['running'] and p['pid'] == 0 for p in s['cleanup'])
        assert files(directory / 'source') == s['source_files'] == expected
        assert s['source_snapshot_sha256'] == h(canonical(expected))
        assert s['stable_artifacts_sha256'] == h(canonical(s['artifacts']))
        assert all(hashes[p] == v for p, v in s['artifacts'].items())
        prior_path = REPO / f'docs/evidence/m4-c0-x64-runtime-2026-10-12/{arch}-build.json'
        prior = load(prior_path)
        result['evidence_hashes'][str(prior_path)] = hashfile(prior_path)
        assert {p.removeprefix('native/'): v for p, v in s['artifacts'].items() if p.startswith('native/')} == prior['artifacts']
        artifacts = verify_artifacts(directory / 'native', directory / 'oracle', expected, arch)
        base_inspections = {}
        for kind, index in (('compiler', 1), ('python', 3)):
            value = load(directory / ('command-%03d.log' % index))[0]
            base_inspections[kind] = value
            reference = BASES[arch][kind]
            assert s['bases'][kind]['reference'] == reference and reference in value['RepoDigests']
            assert value['Id'].removeprefix('sha256:') == s['bases'][kind]['id'].removeprefix('sha256:')
            assert value['Config']['Env'] == s['bases'][kind]['environment']
            assert value['Os'] == 'linux' and value['Architecture'] == ('arm64' if arch == 'aarch64' else 'amd64')
        if arch == 'aarch64':
            prior_oracle_path = REPO / 'docs/evidence/m4-c0-x64-runtime-2026-10-12/oracle-aarch64-manifest.json'
            prior_oracle = load(prior_oracle_path)
            current_oracle = load(directory / 'oracle/oracle-artifacts.json')
            result['evidence_hashes'][str(prior_oracle_path)] = hashfile(prior_oracle_path)
            assert all(prior_oracle[k] == current_oracle[k] for k in ('files', 'oracle_worker_source', 'oracle_elf', 'python'))
            assert 'docker.io/library/python@sha256:2325bb286ec344af3e5898cc224b5844e2707ac6e26b1632516fd3edc84a5e26' in base_inspections['python']['RepoDigests']
            result['checks']['arm_oracle_prior_interpreter_and_source_unchanged'] = True
        assert len(s['commands']) == 53
        counts = {}
        for cmd in s['commands']:
            assert cmd['returncode'] == 0 and hashes[cmd['log']] == cmd['log_sha256']
            argv = cmd['argv']; verb = argv[2]
            assert argv[:2] == ['/usr/bin/podman', '--remote=false']
            assert verb not in ('run', 'start', 'exec', 'kill')
            counts[verb] = counts.get(verb, 0) + 1
            if verb == 'create':
                assert argv[argv.index('--entrypoint') + 1] == '/__obs1_inspection_only__'
            if verb == 'build':
                assert '--no-cache' in argv and '--pull=never' in argv
                assert argv[argv.index('--network') + 1] == 'none'
        assert counts['create'] == counts['rm'] == counts['build'] == 5
        # Join every retained inspection/absence log back to cleanup records.
        for proof in s['cleanup']:
            cid = proof['id'].removeprefix('sha256:')
            inspections = [c for c in s['commands'] if c['argv'][2:4] == ['container', 'inspect'] and c['argv'][-1] == cid]
            assert len(inspections) == 3
            for command in inspections:
                value = load(directory / command['log'])[0]
                assert value['Id'].removeprefix('sha256:') == cid
                assert value['Image'].removeprefix('sha256:') == proof['image'].removeprefix('sha256:')
                assert value['Name'].lstrip('/') == proof['name']
                assert not value['State']['Running'] and value['State']['Pid'] == 0
                assert value['State']['Status'] in ('created', 'configured')
                assert value['State']['StartedAt'] in ('0001-01-01T00:00:00Z', '0001-01-01T00:00:00+00:00')
            absence = [c for c in s['commands'] if c['argv'][2] == 'ps' and 'id=' + cid in c['argv']]
            assert len(absence) == 1 and not (directory / absence[0]['log']).read_bytes().strip()
        roles = {}
        for role, image in s['images'].items():
            inspection = load(directory / f'{role}-inspect.json')
            if role == 'oracle':
                assert inspection['RootFS']['Layers'][:-1] == base_inspections['python']['RootFS']['Layers']
            else:
                assert len(inspection['RootFS']['Layers']) == 1
            env = s['bases']['python']['environment'] if role == 'oracle' else []
            meta = image_metadata(inspection, arch, role, env)
            assert all(image[k] == v for k, v in meta.items())
            assert hashfile(directory / f'{role}-inspect.json') == image['inspect_sha256']
            tree = ImageTree(directory / f'{role}.tar')
            content = verify_content(tree, arch, role, artifacts, {p: (PACKAGE / p).read_bytes() for p in (*SOURCE_FILES, 'build_oracle.py')})
            assert content == load(directory / f'{role}-content.json')
            assert h(canonical(content)) == image['content_sha256']
            assert content['payload_sha256'] == image['payload_sha256']
            assert len(content['elfs']) == image['elf_files']
            saved = archive_record(directory / f'{role}-image.tar', image['id'], arch)
            assert saved == image['portable_archive']
            final, layers = overlay_archive(directory / f'{role}-image.tar', inspection, image['id'], saved['layers'])
            flat = {p: {k: v for k, v in row.items() if k != 'raw'} for p, row in tree.entries.items()}
            differences = {p: {'portable': final.get(p), 'flattened': flat.get(p)} for p in final.keys() | flat.keys() if final.get(p) != flat.get(p)}
            required_access = access_checks(tree, content)
            for p in content['declared_files']:
                rp = tree.resolve(p)
                assert final[rp] == flat[rp], ('declared bytes/config differ', rp)
            closure = {p: r for p, r in content['elfs'].items() if r['required_dependency_closure']}
            for p, r in closure.items():
                assert not r['unresolved_dependencies'] and final[p] == flat[p]
            roles[role] = {'image_id': image['id'], 'archive': saved, 'elf_count': len(content['elfs']),
                           'required_closure_count': len(closure), 'permissions_checked': required_access,
                           'optional_unresolved': {p: r['unresolved_dependencies'] for p, r in content['elfs'].items() if r['unresolved_dependencies']},
                           'portable_layers': layers, 'portable_flat_differences': differences,
                           'required_search_paths': {p: r['search_paths'] for p, r in closure.items() if r['search_paths']},
                           'source_config_archive_and_content_replay': 'PASS'}
        result['images'][arch] = {'roles': roles, 'native_prior_artifacts_identical': len(prior['artifacts']), 'commands': counts,
                                  'source_files': len(expected), 'image_set_sha256': hashes['image-set.json']}
    test_reports = []
    original = REPO / 'docs/evidence/m4-c0-x64-runtime-2026-10-12/tests-022.json'
    old = load(original)
    result['evidence_hashes'][str(original)] = hashfile(original)
    for mode in ('022', '077'):
        path = ROOT / f'tests-{mode}.json'
        r = load(path)
        result['evidence_hashes'][str(path)] = hashfile(path)
        result['evidence_hashes'][str(path.with_suffix('.log'))] = hashfile(path.with_suffix('.log'))
        assert r['status'] == 'PASS' and r['tests_run'] == 272
        assert r['inventory'] == r['started'] == r['passed']
        assert not any(r[k] for k in ('skipped', 'failures', 'errors', 'expected_failures', 'unexpected_successes'))
        assert r['source_files'] == inventory['files']
        assert r['runner_sha256'] == inventory['auxiliary_files']['scripts/test-crawl-jobs-v2-observer.py']
        assert set(old['inventory']) <= set(r['inventory']) and len(old['inventory']) == 256
        test_reports.append({'umask': mode, 'count': 272, 'prior_count': 256, 'retained': 256,
                             'new': sorted(set(r['inventory']) - set(old['inventory']))})
    result['checks']['retained_full_test_reports'] = test_reports
    result['status'] = 'PASS'
    (OUT / 'evidence-verification.json').write_text(json.dumps(result, indent=2, sort_keys=True) + '\n')
    print(json.dumps({'status': result['status'], 'image_sets': {a: {'native_identical': r['native_prior_artifacts_identical'],
        'roles': {role: {'elfs': v['elf_count'], 'closure': v['required_closure_count'],
            'overlay_differences': list(v['portable_flat_differences'])} for role, v in r['roles'].items()}} for a, r in result['images'].items()},
        'test_reports': [{'count': r['count'], 'retained': r['retained']} for r in test_reports]}, indent=2))


if __name__ == '__main__':
    main()
