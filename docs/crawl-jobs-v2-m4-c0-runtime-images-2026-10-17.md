# C0 ARM64 and x64 runtime image sets — 2026-10-17

**Status: local source/static-image preparation and independent review complete;
both reviewers return GO_FOR_CONTINUED_IMPLEMENTATION.** Work is local on `feature/crawl-jobs-v2-c0-runtime-images`, based on
published PR #30 head `7ef2041`. The [PR publication and exact CI](evidence/m4-c0-x64-publication-2026-10-17/README.md)
pass all 14 required contexts / 23 jobs.
PR #30 was subsequently owner-merged as `82cd307`, with the exact published tree
preserved and its own [main CI verified](evidence/m4-c0-x64-main-2026-10-17/README.md).

## Image construction and verification

- Explicit GCC 14.3.0/bookworm and Python 3.13.15/slim-bookworm **platform pins**.
  The former Python pin `2325bb28…` is an index; selected ARM64 child `ad4c34ff…`
  preserves the existing base content, with x64 child `3e2de9c4…` selected separately.
- A rootless local Podman builder constructs target, observer and oracle images
  for each architecture. OCI environment/role/configuration checks preserve the
  existing native empty-environment contract. Default PATH/builder labels are
  removed, and cached final metadata is avoided.
- Native ELF, instruction-manifest, profile, compiled BPF and compiler/source
  records are joined to actual exported bytes. **All eleven native artifacts per
  platform remain byte-identical to C0-ARCH-03.**
- Version-3 oracle manifests import module definitions without invoking worker
  main or creating readers/clients. Each records 11 imported extension ELFs.
  The required dependency closure has 18 files per platform. Unused optional Tk
  dependencies and a non-loadable Python object remain explicit in the full inventory.
- Bounded archive readers perform no host extraction. Role source/interpreter
  identity, required permissions, normalized names, link resolution, config IDs
  and portable layer hashes are checked. Root attributes are observed in the
  owned stopped filesystem using a scoped rootless storage stat helper.
- Each platform's five inspection containers are checked never-started before
  and after operations, removed, then checked absent. The helper releases its
  temporary filesystem mount. Compiler and manifest-builder processes are the
  intended build-time execution; C0 actor entrypoints remain unstarted.
- A new native ARM64/x64 CI matrix retains portable images and evidence. It uses
  the supported rootless Podman construction path and is **not yet run hosted**.

## Review findings and local corrections

Both [initial reviews](evidence/m4-c0-runtime-images-2026-10-17/README.md)
returned NEEDS_REMEDIATION, while independently reproducing the supplied actual
image sets successfully. Their original source, failures and reports are retained.

| Finding | Local correction and regression |
|---|---|
| COR-001 — BuildKit PATH | Refuse unsupported Docker construction; use the selected rootless Podman path in local tooling and CI without relaxing the native environment contract |
| COR-002 — ARM-host x64 fixture | Isolate interpreter and extension fixtures together, retaining real ELF validation and both-ABI foreign-extension refusal |
| COR-003 / SEC-01 — archive aliases | Reject duplicate normalized portable member paths before configuration/layer lookup |
| COR-004 — access by UID/GID 999 | Require source/library read bits, native/interpreter execute bits, and root-owned readable/searchable directories, including observed root and alias traversal |
| COR-005 — loader search | Preserve RUNPATH/RPATH distinction and precedence; resolve directory links; explicitly refuse active legacy RPATH, malformed links and ambiguous library candidates |
| SEC-02 — link/parent ordering | Preserve link components and follow preceding links before processing `..`, with bounded expansion |
| SEC-03 — root headers | Charge every yielded header and reject duplicate root markers |

The corrected **55 package + 3 auxiliary files pass 283/283 tests under umasks
022/077**, retaining all 272 previous IDs plus eleven regressions. Both rebuilt
image sets pass, with ten stopped inspections removed. Independent re-review is
checking closure against the original findings.

The [first security re-review](evidence/m4-c0-runtime-images-remediation-2026-10-17/first-security-review.md)
closes SEC-01–03 and revalidates all six images, but returns NEEDS_REMEDIATION for
**SEC-04 hardlink-to-symlink source misbinding** and **SEC-05 normalized-root
metadata aliases**. Its in-memory candidate guards reject the defects, preserve
positive controls and leave actual retained content records byte-identical.
The first correctness re-review also requires execute access on required ELF
loaders (remaining COR-004) and identifies the same root-alias issue (COR-006).
Both reports remain unchanged.

### Intermediate validator follow-up

The follow-up carries execute intent along required `PT_INTERP` edges before
visited-node deduplication, refuses link-bearing/inconsistent hardlink targets,
and routes normalized root aliases through the root observation and duplicate
checks. **286/286 tests pass under both umasks**, with all 283 preceding IDs
retained. Only `image_artifacts.py` and `test_image_review.py` change from the
preceding corrected snapshot; all image-construction inputs remain unchanged.

The [final-code revalidation](evidence/m4-c0-runtime-images-final-2026-10-17/artifact-revalidation.json)
checks all six retained images and confirms identical configuration/layer/archive
identities and **byte-identical content records**. Their image IDs below remain
valid. Construction records retain their original source maps; the explicit
two-file continuity record joins them to the new validator rather than relabeling
them. At that snapshot, independent final reviews were pending; subsequent closure
is recorded below.

The subsequent [security result](evidence/m4-c0-runtime-images-final-2026-10-17/first-security-review.md)
closes SEC-01–05 and confirms loader execute checks, but requires **SEC-06**:
all hardlink inode bindings must be checked after parsing, including unused aliases.
The actual supplied image sets remained valid; the additional bounded
host-validator correction is closed below.

## Final review and image identities

### Final review closure

The complete flattened map now requires literal directory ancestry before every
hardlink is validated. This closes the unused-alias and shadowed-record variants
without changing any actual image contents. **288 tests pass under both umasks**;
all previous IDs remain. The final **55 package + 3 auxiliary files** have two
independent GO verdicts with **COR-001–007 and SEC-01–06 closed**.

Correctness independently passes 80 controls and 32 image tests; security passes
43 targeted tests. Both revalidate all six retained images and preserve prior
outputs. See the [closed review gate and evidence](evidence/m4-c0-runtime-images-closure-2026-10-17/README.md).
The underlying native/image-construction inputs remain unchanged. All intermediate
reports retain their recording-time verdicts and identities.

### Retained image IDs

| Platform | Role | Image config ID |
|---|---|---|
| ARM64 | target | `sha256:50de9aac35fac6d4cada173cf06884c43b506e123cb923e851afae93bcb50273` |
| ARM64 | observer | `sha256:f36e584c1808d192263ec9da1071ff16122afff0e67ee3e5ccda4881ec44ab12` |
| ARM64 | oracle | `sha256:24268df2086d9348743fbc309113a46a9afc4a9f607fbc75a0987718ff2db2ed` |
| x64 | target | `sha256:1062239ded1acb7b4ac0a8c2f85920fc336e49182dc4e42e8175745e2f813a9f` |
| x64 | observer | `sha256:2046ca5b52ec7adf8ba4ad644401f49e22955b225377b0024e52b386168cd4cf` |
| x64 | oracle | `sha256:26201a7d895b496c26746271a8ff1fa60687d495739f2cfe0456d129f180c940` |

These local builder-dependent identities are recorded separately from stable
file hashes. Portable archive hashes, configuration/layer identities and the
complete records are in the [corrected evidence](evidence/m4-c0-runtime-images-remediation-2026-10-17/README.md).

| Checkpoint | SHA-256 |
|---|---|
| Corrected source inventory | `14ac223d10a72ff859d653c29bf721e35af320fc3dbfe823a4c8176180576670` |
| Local verification | `9077377d1672e648be7275197b447cbefbb4385587b7929f859add2f32454bda` |
| 156-export local manifest | `cb5cd4fd7a7b3b6e44d2b035f676bcbb7dbce12a0450f7e7d89c932b4c8409e1` |
| Durable local delivery | `c3efab7bda93edf2f1ffea91309bdb8b855fe75b6ffe7e942d6a9aeb2db39197` |
| Final validator inventory | `83288b8fd89fcbd6453626df75b6b48dd02150fda5def59d0942d92dc6483a6b` |
| Final-code artifact revalidation | `40e1106f132e054fa7654a94bba30ff85a3567b738f057527e24bba21704b851` |
| Final validator local exports | `ae7791d6f24165b51a025d5dfc87f0e2e18637e58b4b6ce09ddb699930d9132a` |
| Closed source inventory | `1f0e82a5ca98f02bf36deb4292501a0b97a38ac8bf567fd170fc09c8d4890376` |
| Closed artifact revalidation | `a8f6aee7dee482c03b925bef48a57e2ed5eb5d46edc61ea11f67c9790b5211f7` |
| Final source/static-image gate | `8be6ba0d21a4a6b0a6086b89272ba195781b56ac130b1ed6f8e98051592ffab5` |
| 45-export final manifest | `60091b6c543c5f876b1e94113f57bde70b34a6c50e4c668ba04573a3e407ae01` |
| Final durable delivery | `76b8f7bf582275065dbe7ca343fc245fb1f7779d4d8efc8e7009370900ef599b` |

The six portable images, final tooling/source, gate and artifacts are preserved in
Git-ignored `out/c0-runtime-images-2026-10-17/1f0e82a5ca98f/`: **105 files,
324,452,401 bytes**. Every copied file matches its expected hash. The preceding
103-file construction snapshot under `14ac223d10a72/` is retained as history.

## Next gates

Next: scoped C0-ARCH-04 source publication and actual hosted image-matrix
verification. PR #30's owner merge and merged-main verification are complete.
Native host/backend admission, worker execution, kernel
confinement, hardware breakpoints and timing remain C0-ARCH-05; registry/accepted
dual-architecture publication remains C0-ARCH-06. Full M4 acceptance remains open.
