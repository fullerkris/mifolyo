"""Offline only: read retained image files, never contact a container engine."""
import hashlib
import json
from pathlib import Path
import sys

ROOT = Path('/tmp/opencode/c0-runtime-images-2026-10-17')
OUT = ROOT / 'security'
PACKAGE = ROOT / 'snapshot/tests/crawl-jobs-v2-observer'
REPO = Path('/var/home/kfuller/Documents/Git_Projects/mifolyo')
sys.path.insert(0, str(PACKAGE))
from contracts import canonical, sha
from image_artifacts import ImageTree, archive_record, image_metadata, verify_content
from image_build import source_files, verify_artifacts
from oracle_protocol import SOURCE_FILES

def file_sha(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()

result = {'python': sys.version, 'mode': 'offline-static-no-engine', 'sets': {}, 'reported_suites': {}}
sources = source_files(PACKAGE)
source_bytes = {name: (PACKAGE / name).read_bytes() for name in (*SOURCE_FILES, 'build_oracle.py')}
for arch in ('aarch64', 'x86_64'):
    base = Path('/tmp/opencode/c0-images-' + arch + '-pass5-2026-10-17')
    record = json.loads((base / 'image-set.json').read_bytes())
    assert record['status'] == 'PASS_STATIC_IMAGE_SET'
    assert record['source_files'] == source_files(base / 'source') == sources
    assert record['source_snapshot_sha256'] == sha(canonical(sources))
    actual_artifacts = {name: file_sha(base / name) for name in record['artifacts']}
    assert actual_artifacts == record['artifacts']
    assert record['stable_artifacts_sha256'] == sha(canonical(actual_artifacts))
    artifacts = verify_artifacts(base / 'native', base / 'oracle', sources, arch)
    previous_path = REPO / 'docs/evidence/m4-c0-x64-runtime-2026-10-12' / (arch + '-build.json')
    previous = json.loads(previous_path.read_bytes())
    assert all(actual_artifacts['native/' + name] == value for name, value in previous['artifacts'].items())
    commands = record['commands']
    assert len(record['cleanup']) == 5 and all(row['removed'] and row['pid'] == 0 and not row['running'] for row in record['cleanup'])
    assert record['cleanup_complete'] and record['runtime_acceptance'] is record['execution_authorized'] is False
    assert record['inspection_containers_started'] == record['c0_actors_started'] == 0
    for row in commands:
        assert row['returncode'] == 0 and row['log_sha256'] == file_sha(base / row['log'])
        assert row['argv'][1] == '--remote=false'
        assert row['argv'][2] not in ('run', 'start', 'exec', 'load')
    entry = {'image_set_sha256': file_sha(base / 'image-set.json'), 'source_files_verified': len(sources),
             'previous_build_record_sha256': file_sha(previous_path), 'native_artifacts_match_previous': True,
             'artifacts': actual_artifacts, 'commands_verified': len(commands), 'cleanup_records_verified': 5,
             'directory_mode': oct(base.stat().st_mode & 0o777), 'images': {}}
    for role, recorded in record['images'].items():
        metadata = json.loads((base / (role + '-inspect.json')).read_bytes())
        environment = record['bases']['python']['environment'] if role == 'oracle' else []
        admitted = image_metadata(metadata, arch, role, environment)
        assert all(recorded[key] == value for key, value in admitted.items())
        assert recorded['inspect_sha256'] == sha(canonical(metadata))
        tree = ImageTree(base / (role + '.tar'))
        content = verify_content(tree, arch, role, artifacts, source_bytes)
        assert canonical(content) == (base / (role + '-content.json')).read_bytes()
        assert recorded['content_sha256'] == sha(canonical(content))
        assert recorded['payload_sha256'] == content['payload_sha256']
        portable = archive_record(base / (role + '-image.tar'), recorded['id'], arch)
        assert portable == recorded['portable_archive']
        unresolved = {name: row['unresolved_dependencies'] for name, row in content['elfs'].items() if row['unresolved_dependencies']}
        entry['images'][role] = {'id': recorded['id'], 'content_sha256': recorded['content_sha256'],
                                 'rootfs_sha256': file_sha(base / (role + '.tar')), 'portable': portable,
                                 'archive_members': len(tree.entries), 'regular_file_bytes': tree.total_bytes,
                                 'elf_count': len(content['elfs']), 'optional_unresolved': unresolved,
                                 'required_elf_count': sum(row['required_dependency_closure'] for row in content['elfs'].values())}
        del tree
    result['sets'][arch] = entry
for name in ('tests-022.json', 'tests-077.json'):
    report = json.loads((ROOT / name).read_bytes())
    result['reported_suites'][name] = {'sha256': file_sha(ROOT / name), 'status': report['status'],
                                       'tests_run': report['tests_run'], 'source_matches':
                                       report['source_files'] == {'tests/crawl-jobs-v2-observer/' + k: v for k, v in sources.items()},
                                       'independently_rerun': False}
    assert report['status'] == 'PASS' and report['tests_run'] == 272
result['pass'] = True
with (OUT / 'retained-verification.json').open('x') as file:
    json.dump(result, file, indent=2, sort_keys=True)
    file.write('\n')
print(json.dumps({'pass': True, 'architectures': list(result['sets']), 'runtime_images': 6, 'reported_suites': result['reported_suites']}))
