"""Serialize the completed review without changing any reviewed input."""
import hashlib
import json
from pathlib import Path

OUT = Path(__file__).resolve().parent
BASE = OUT.parent


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def load(path):
    return json.loads(path.read_bytes())


evidence = load(OUT / 'evidence-verification.json')
integrity = load(OUT / 'inventory-after.json')
inventory = load(BASE / 'source-inventory.json')
probes = load(OUT / 'adversarial-results.json')
findings = [
    {
        'id': 'C0ARCH04-COR-001', 'priority': 'P1', 'marker': 'blocker', 'gate_blocking': True,
        'title': 'Docker/BuildKit adds a native PATH that image validation forbids',
        'locations': ['tests/crawl-jobs-v2-observer/image_build.py:80-81,133-138,179-180',
                      'tests/crawl-jobs-v2-observer/image_artifacts.py:43-46', '.github/workflows/c0-runtime-images.yml:52-56'],
        'reason': 'The workflow selects Docker and the builder enables BuildKit. Its Linux scratch image default is nonempty Config.Env; only the Podman branch removes PATH. Native image validation and the existing runtime scope require empty Env.',
        'effect': 'The Docker route rejects the first native image with IMAGE_ENVIRONMENT; both provided pass5 successes are Podman, not Docker.',
        'evidence': ['buildkit-scratch-path', 'buildkit-v0.20.2-image.go:27-39', 'buildkit-v0.20.2-convert.go:684-691', 'buildkit-current-image.go:40-55'],
        'suggestion': 'Use a tested construction path preserving the empty-native-environment contract (for example the proven Podman path), or implement a Docker-compatible config path; add realistic BuildKit metadata regression coverage.',
        'confidence': 'high', 'limit': 'Static upstream source and fake-command/metadata reproduction; no Docker build run.'
    },
    {
        'id': 'C0ARCH04-COR-002', 'priority': 'P1', 'marker': 'blocker', 'gate_blocking': True,
        'title': 'The x64 oracle manifest test reads unmocked host-native extension ELFs on ARM',
        'locations': ['tests/crawl-jobs-v2-observer/test_x64_runtime.py:181-194',
                      'tests/crawl-jobs-v2-observer/build_oracle.py:41-45', '.github/workflows/c0-runtime-images.yml:35-46'],
        'reason': 'The x64 positive test mocks only uname and sys.executable, but manifest v3 also validates sys.modules ExtensionFileLoader origins. Those are native ARM files on the ARM runner and cannot satisfy the forced x86_64 architecture.',
        'effect': 'ARM CI fails the offline suite with ORACLE_BUILD_ARCHITECTURE before building images.',
        'evidence': ['arm-ci-x64-manifest-fixture'],
        'suggestion': 'Isolate interpreter and extension fixtures for each selected ABI or make the host-positive test consistently native while retaining independent cross-architecture rejection tests; do not skip retained coverage.',
        'confidence': 'high', 'limit': 'Original test passed on x64 then failed with one inert ARM extension spec; no native ARM suite run here.'
    },
    {
        'id': 'C0ARCH04-COR-003', 'priority': 'P2', 'marker': 'suggestion', 'gate_blocking': False,
        'title': 'Portable archive accepts conflicting normalized config/layer names',
        'locations': ['tests/crawl-jobs-v2-observer/image_artifacts.py:269-283'],
        'reason': 'Member uniqueness is tested before path normalization. config.json and ./config.json (likewise layer.tar) can contain different bytes while only the exact manifest-selected spelling is verified.',
        'effect': 'A passing archive record does not establish which conflicting normalized file a consuming loader will observe, or whether it can load the archive at all.',
        'evidence': ['portable-normalized-config-duplicate', 'portable-normalized-layer-duplicate', 'portable-exact-duplicate-control'],
        'suggestion': 'Enforce canonical archive member/reference names or reject normalized collisions, including the validated legacy alias namespace.',
        'confidence': 'high', 'limit': 'Pure reads only; no archive extraction/load. No collision exists in supplied pass5 archives.'
    },
    {
        'id': 'C0ARCH04-COR-004', 'priority': 'P2', 'marker': 'suggestion', 'gate_blocking': False,
        'title': 'Required files and paths need not be accessible to configured UID/GID 999',
        'locations': ['tests/crawl-jobs-v2-observer/image_artifacts.py:102-123,219,233,244-252'],
        'reason': 'Any execute bit is accepted for root-owned executables, read bits are not checked for required source/library files, and parent directory search bits are ignored.',
        'effect': 'Static content verification passes root-owned mode-0700/0750 native executables, mode-0600 oracle sources/extensions and a mode-0700 /oracle directory, although UID/GID 999 cannot use them.',
        'evidence': ['target-not-executable-by-999', 'observer-not-executable-by-999', 'inaccessible-oracle/worker.py', 'inaccessible-oracle', 'inaccessible-usr/local/lib/fixture.so'],
        'suggestion': 'Validate required read/execute access and all traversed-directory search access for the configured identity, including archive root metadata.',
        'confidence': 'high', 'limit': 'Inert metadata mutations only. Actual pass5 declared/required paths pass added reviewer permission checks.'
    },
    {
        'id': 'C0ARCH04-COR-005', 'priority': 'P2', 'marker': 'suggestion', 'gate_blocking': False,
        'title': 'Static dependency resolver conflates RPATH/RUNPATH and misses symlinked search directories',
        'locations': ['tests/crawl-jobs-v2-observer/image_artifacts.py:174-177,188-206'],
        'reason': 'The resolver loses the dynamic tag distinction, searches RPATH even when RUNPATH should disable it, and requires exact candidate membership before resolving parent-directory symlinks.',
        'effect': 'It accepts a required dependency available only in ignored RPATH and falsely refuses an existing RUNPATH dependency through a directory symlink.',
        'evidence': ['rpath-runpath-false-positive-closure', 'runpath-symlink-directory-false-refusal', 'ld-so-manual.html'],
        'suggestion': 'Preserve supported loader search semantics or reject unsupported combinations; resolve candidate paths before classifying genuine absence, retaining malformed-link refusals.',
        'confidence': 'high', 'limit': 'No dynamic loader executed. Neither fixture corner case is present in the actual required graphs.'
    },
]

all_source_hashes = inventory['files'] | inventory['auxiliary_files']
for f in findings:
    f['source_hashes'] = {loc.split(':', 1)[0]: all_source_hashes[loc.split(':', 1)[0]] for loc in f['locations']}
    f['reproducer_script'] = str(OUT / 'adversarial_probes.py')
    f['reproducer_results'] = 'adversarial-results.json'

record = {
    'kind': 'c0_arch_04_independent_read_only_correctness_review', 'version': 1, 'date': '2026-10-17',
    'verdict': 'NEEDS_REMEDIATION', 'execution_authorized': False, 'runtime_acceptance': False,
    'summary': 'Both supplied Podman image sets pass byte-level static replay; two proposed CI paths are broken and three validator hardening gaps are independently reproduced. No actual pass5 payload/hash mismatch found.',
    'scope': {'repository': '/var/home/kfuller/Documents/Git_Projects/mifolyo', 'branch': integrity['branch'], 'head': integrity['head'],
              'inventory_sha256': integrity['inventory_sha256'], 'snapshot': str(BASE / 'snapshot'), 'review_directory': str(OUT),
              'package_files': 54, 'auxiliary_files': 3, 'before_after_match': True, 'evidence_files_unchanged': integrity['evidence_files_checked'],
              'unrelated_git_status_equality_required': False},
    'checks': {
        'actual_artifact_replay': 'PASS', 'independent_portable_overlay_comparison': 'Zero differences in all six complete inventories',
        'native_prior_artifacts_identical_per_architecture': 11,
        'arm_oracle_prior_interpreter_and_source_unchanged': True,
        'full_elf_counts': {'aarch64': {'target': 1, 'observer': 1, 'oracle': 790}, 'x86_64': {'target': 1, 'observer': 1, 'oracle': 791}},
        'oracle_required_closure_nodes_per_architecture': 18, 'recorded_oracle_extensions_per_architecture': 11,
        'optional_unresolved_edges': ['_tkinter -> libtk8.6.so', '_tkinter -> libtcl8.6.so'],
        'actual_declared_and_required_path_permission_checks': 'PASS',
        'command_records_and_log_hashes_per_architecture': 53, 'stopped_cleanup_records_per_architecture': 5,
        'existing_image_tests_executed': {'status': 'PASS', 'count': 16, 'log': 'image-unit-tests.log', 'source': 'frozen snapshot', 'resource_warnings': 'error'},
        'independent_probe_groups': {'completed': len(probes['results']), 'finding_reproductions': 11, 'expected_refusal_or_lifecycle_controls': 9, 'fake_lifecycle_failure_paths': 8},
        'retained_full_test_reports': evidence['checks']['retained_full_test_reports'],
        'full_272_test_suite_rerun_by_this_review': False,
        'ci': {'matrix': {'aarch64': 'ubuntu-24.04-arm', 'x86_64': 'ubuntu-24.04'}, 'action_pins_resolve': True,
               'review': 'manual static review; upstream BuildKit source corroboration; modeled environment and ARM extension regression',
               'hosted_execution': False, 'actionlint_run': False}
    },
    'findings': findings,
    'actual_images': evidence['images'],
    'input_evidence_hashes': evidence['evidence_hashes'],
    'source_inventory_files': all_source_hashes,
    'public_references': load(OUT / 'references.json'),
    'limits': [
        'No image build/pull/create/start/load, live image inspection, live cleanup, ptrace, installed filter, C0 worker/main/collector or live SSH was run.',
        'No repository or snapshot files were edited; no commits/pushes; only correctness/ outputs and inert fixtures were written.',
        'Engine never-started/removed state was joined within existing hashed logs, not freshly observed on an engine.',
        'Docker PATH failure is supported by versioned/current upstream source and real validator applied to modeled metadata, not a new Docker build.',
        'ARM CI regression is simulated with an inert extension ELF/spec; the actual native ARM offline suite was not executed.',
        'No filesystem extraction or image loading tested portable aliases; demonstrated conflicting normalized identities only.',
        'Overlay comparison covers path kinds, regular-content hashes, UID/GID, mode and link targets, not timestamps/xattrs. Flat archives contain no explicit root member, so added accessibility checks do not independently establish root-directory metadata.',
        'Supplied 272-test results and 256-ID continuity were checked, not rerun; independently ran 16 image tests and the targeted original manifest unit test.',
        'Live loader symbol versions/lazy loading, kernel confinement, calibration and hardware behavior remain C0-ARCH-05; registry publication remains C0-ARCH-06.',
        'The old Python parent-index membership was not independently queried from a registry; ARM continuity is supported by old interpreter/source identities and retained RepoDigests.',
        'Two early replay assertions mis-compared prefixed/unprefixed Podman IDs; final corrected replay passes and the diagnostic logs are retained.'
    ],
    'next_steps': 'Remedy COR-001/COR-002, add COR-003–005 regression controls, refresh inventory and repeat bounded review; retain pass5 evidence. No execution/publication authority is granted.',
    'review_markdown_sha256': sha(OUT / 'review.md'),
}
record['review_support_hashes'] = {p.name: sha(p) for p in sorted(OUT.iterdir()) if p.is_file() and p.name not in ('review.json', 'report-hashes.txt')}
(OUT / 'review.json').write_text(json.dumps(record, indent=2, sort_keys=True) + '\n')
print('review.md', sha(OUT / 'review.md'))
print('review.json', sha(OUT / 'review.json'))
