# C0 architecture-contract review remediation — 2026-10-12

**Status: remediation and independent re-review complete — both reviewers return
GO_FOR_CONTINUED_IMPLEMENTATION.**
On `feature/crawl-jobs-v2-c0-architecture-contracts`, based on `ced6e1c`, the
[initial architecture review](crawl-jobs-v2-m4-c0-architecture-contracts-2026-10-12.md)
identified three gaps. The corrected **47-file package passes 238 C0 tests under
both umasks**, retaining all 229 previous IDs and the original 202 merged IDs.
Work remains local/uncommitted.

## Findings and corrections

| Finding | Reproduced defect | Local correction |
|---|---|---|
| COR-ARCH-001 — P1 | Direct actor startup from a registered snapshot skipped actual image architecture inspection | Shared image Id/OS/architecture/config/environment admission runs before both creation and startup; the exact referenced image is checked before the start transport |
| COR-ARCH-002 — P2 | Public anchor mutation during callbacks could detach response/receipt architecture from the signed request, or accept a legacy anchor | Revalidate and freeze operation-local anchors/keys; reject anchor drift across callbacks; use the snapshots for request/response/receipt joins and enforce lease-context architecture consistency |
| COR-ARCH-003 — P2 | Standalone record mode could label an x64 target as ARM64 or hash a target inconsistent with the instruction manifest | Read/validate one target snapshot, regenerate and compare the exact canonical manifest before compiler queries/output, and record that same snapshot hash |

Changes from the initial reviewed snapshot are limited to `docker_backend.py`,
`runtime_context.py`, `build_tools.py`, the synthetic image-metadata fixture in
`test_owned_resources.py`, and new `test_architecture_review.py`.

## Verification

- Retained red run reproduces all three defects; the valid context and matching
  ARM64 build-record controls pass.
- **238/238 full-suite passes under umask 022**, and **238/238 under umask 077**;
  all 229 previous IDs retained, nine new regression/positive tests, no skips,
  failures, errors or ResourceWarnings.
- Startup regressions cover target/observer/oracle roles with foreign, missing
  and valid image architecture. Context tests cover callback drift, endpoint/source
  lifecycle validation, authenticated relabelling and valid request/receipt joins.
- Build-record tests use inert synthetic ELF tables and simulated compiler queries
  for foreign targets, stale/legacy manifests and the matching ARM64 positive case.
  No ELF is executed by these tests.
- Fresh native artifact-only rebuild passes. **Ten outputs are byte-identical** to
  the initial architecture build, including both ELFs, target manifest/headers,
  BPF and profiles. `toolchain.json` changes to bind the corrected builder source.
- All **ten oracle source/recipe inputs** and oracle manifest/interpreter hashes
  are unchanged, so the already-verified oracle artifact build is retained.

No native actor, real collector, oracle worker, live SSH or calibration ran. X64
remains metadata-only until its separate native port. Both original reviewers
verified the exact corrected snapshot and replayed their initial counterexamples.

## Identities

| Record | SHA-256 |
|---|---|
| Corrected 47-file inventory | `a02661da8cb299ff60e4ad9752ea53329b65c384d7f8e15853641dcf6db426cc` |
| Local remediation verification | `3617c193c2bbe652ccbc1a526d7710a020e28fc62aa2f808d906bf917b79ea87` |
| Full-suite report, each umask | `d6bf46767e6c7909789e53ce008dc0e26ebd60f4c3e3f76fe4837747ceab38c4` |
| Host-source bundle | `ffae141857d61996ab5e5b53e8b55402a94b3caed39fa623665467610d81d836` |
| Regenerated native toolchain record | `4926c2e34b92a025c27e0fecdfd64c597da94d53a78d9a4231d51f8c8f0e5eae` |

Transport/oracle-worker/platform identities retain the initial architecture-contract
values. The host-source change requires regenerated anchors/deployment/preparation
bindings. Initial source, reviews, failed probes and build records retain their
original bytes and scope.

The [evidence index](evidence/m4-c0-architecture-remediation-2026-10-12/README.md)
and [local manifest](evidence/m4-c0-architecture-remediation-2026-10-12/local-manifest.json)
bind eleven exact exports; manifest identity
`ad7d1f828702fbf2a6a1849fbf5aca10a5c93d7dce18e3d583d44bbc00a5f7d0`.
Private snapshot/artifact/re-review workspace:
`/tmp/opencode/m4-c0-architecture-remediation-2026-10-12/`.

## Final independent re-review

**Correctness re-review complete:** GO_FOR_CONTINUED_IMPLEMENTATION. All three
findings are closed, 238/238 tests pass independently under umask 022, original
counterexamples replay with corrected outcomes, and 76 additional boundary
controls pass. Source/runner/artifact integrity is preserved. Review:
`6e4b9dd0240cd10027dc78802ffdb1b3748adb31d5487a55eea34f855b60f236`.
The [correctness manifest](evidence/m4-c0-architecture-remediation-2026-10-12/correctness-manifest.json)
binds 14 exact exports.

**Security re-review complete:** GO_FOR_CONTINUED_IMPLEMENTATION. The initial
counterexamples were independently reproduced and all three fixes verified;
238/238 tests passed under umask 077. The reviewer preserved all prior test
assertions, passed 72 create/start image cases per revision, 40 anchor-lifecycle
cases, record-mode negatives/positives and the retained security/artifact probes,
including 3,092 interpreted BPF checks. No new in-scope finding. Review:
`7f37b6ed3c89af0cb30c06b7d91f7f64a5d21c29b06ef5454a8677bc258263c5`.
Its [manifest](evidence/m4-c0-architecture-remediation-2026-10-12/security-manifest.json)
binds 26 exact exports. Original GO/NEEDS_REMEDIATION reports and failed probes
remain unchanged.

The [final gate](evidence/m4-c0-architecture-remediation-2026-10-12/final-gate.json)
has identity `482c0303fdee6bc49c317a6ec8f592526d0ed1d646236537543d7dab162bae9a`.
The [final manifest](evidence/m4-c0-architecture-remediation-2026-10-12/manifest.json)
binds **55 exact local/reviewer exports**, identity
`cad1997113c13cced6aa9edd276bc093bce8f3f949f5a9eefd1a8be5d8320c5c`.
Both final verdicts cover source/static artifacts, not publication or live runtime
authorization. The reviewed package remains unchanged and uncommitted.

## Next gates

1. Separate scoped publication/CI decision for the reviewed architecture increment.
2. X64 implementation, both runtime image sets, host/backend selection and native
   per-platform admission/calibration under C0-ARCH-03–06. Full M4 remains open.
