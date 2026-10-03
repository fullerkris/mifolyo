# Shared group capacity: selected arm64 image preparation — 2026-10-01

**PASS; selected-case image/source/admission checks and independent cleanup
verification complete.** Preparation explicitly selected
`ledger-shared-group-capacity-v1` against the reviewed 107-file lifecycle source.
The four metadata-role containers remained stopped; only networkless image checks
and Redis `--version` ran. **No acceptance Redis server or shared-capacity case
was started.** Full M4 remains open.

## Exact artifacts

| Artifact | Identity |
|---|---|
| Case | `ledger-shared-group-capacity-v1` |
| Harness / stand-in, Linux/arm64 | `sha256:03bf14679ffdda67c7940ffca0cfd52d38fcf5b9d8e0ca4f173b3fbe2e76d84d` |
| Redis 7.4.11, Linux/arm64 | `sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c` |
| Plan | `526593999b3eefe48bf0492cb5221603d5475ab1686460944253c44668b0f4eb` |
| Recipe | `05bfdbbea0bf5166b332e8e68225d466dcd1b076bf171fca30d062b946d88dc4` |
| Inputs | `7687a63ebbff0aa8ec693493b0cd943cd2d4bddb80183b056086c7257e05a401` |
| Image validation | `52000eff058bfa81ae6f1b5722ba7e1225ab4886daefc0ab2ac47e69b48d9875` |
| Independent image postcheck | `00bc44c893d8197bec2081c848bf8e9f2bdedf694bac193f4e710a2594f039ff` |
| Build record | `b9a1fbc768b7167ba1d3706edeb07ba11b26311cfc9c9012ba4795c32ef6086b` |

The source inventory remains
`373f143179c558781922dedd2a90e9f65b461532610ca04e659fdcbe3fdea9d4`.
Both source reviewers returned GO for image/CI preparation; the verified lifecycle
has 171 harness/21 script tests and 81 canonical Lua invocations under race.
The [source checkpoint](crawl-jobs-v2-m4-shared-group-lifecycle-2026-10-01.md)
retains those exact review/test bindings.

## Build and selected preparation

The build used the cached immutable Python 3.13.15 arm64 platform manifest:

```text
docker.io/library/python@sha256:ad4c34ff79289506e235b40dce75d629e25f226b597a2455804220f037e07531
```

Its local base image is
`sha256:adc3d531c29fbd69fa9ca49cd8e241c68aaf4e1dd7462be6c2432044b7a39ea8`.
The reviewed multi-platform index remains
`sha256:2325bb286ec344af3e5898cc224b5844e2707ac6e26b1632516fd3edc84a5e26`.
Build flags include `--network none --pull=false --platform linux/arm64` and the
explicit immutable base override. Build command wall time was 1,939 ms.

`prepare-crawl-jobs-v2-images.py` received **`--case ledger-shared-group-capacity-v1`**
and the exact image IDs above. Preparation completed in 17,128 ms, exit 0.
The emitted plan recompiles exactly from its inputs, and the emitted recipe
matches the reviewed current source. The stand-in and harness identities are equal.

Both init and executor image checks report:

- Exact **74-file `/app` inventory**, no extra files or symlinks.
- All **19 case recipes** and normative/source identities matching the host.
- Python **3.13.15**, admitted process/network/capability receipts and successful
  bounded request validation for every registered case.
- Exit 0, no OOM kill and verified image-check cleanup.

Observed image-check memory peaks were **48,857,088 bytes** for init and
**48,828,416 bytes** for executor, below 128 MiB and 256 MiB limits respectively.
These are image-check observations, not maximum-shape case memory or latency
acceptance. The Redis check only reported version 7.4.11.

## Stopped-role admission and cleanup

Metadata fixture: **`15019fa1d2e097541bf5643d6ac8ad07`**.

The init, executor, Redis and revocation containers were each inspected in
`created` state with PID 0. Init's sole additional capability was Docker's
equivalent `CAP_CHOWN` spelling; other roles had none. Closed images, commands,
users, network/storage policy and volume attachments passed admission.

The independent postcheck verified all six exact metadata resource names absent:
four `cj2-prestart-check-15019fa1d2e097541bf5643d6ac8ad07-<role>` containers and
the corresponding `-data` / `-control` volumes. Six fixture/case/image-check label
queries—containers and volumes for each selector—were empty. Absence is reported
by exact names and labels; no unrecorded container-ID checks are claimed.

The preparer separately verified cleanup of its three image-check containers.
No actual acceptance case or credential approval was created. All 107 reviewed
source files, the prior 40-file local batch and 372 unrelated files remained intact
through preparation.

## Next gate

The selected artifacts are ready for **scoped publication and exact-revision
protected CI**. The branch is `feature/crawl-jobs-v2-shared-group-capacity`, based
on merged `7da55b1`; the source, earlier foundation/lifecycle evidence and these
preparation records remain local and uncommitted.

After publication/CI, fresh exact-artifact approval and a separate execution
decision are still required for one bounded invocation. Prior approvals remain
consumed. The preparation does not establish actual shared-capacity enforcement,
per-owner credential isolation, simultaneous physical workers, origin-only
blocking, global saturation, durability, performance or full M4 acceptance.

See the [exact evidence index](evidence/m4-shared-group-preparation-2026-10-01/README.md)
and [implementation plan](crawl-jobs-v2-plan.md). Private originals, build/preparation
logs and postchecks remain under
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-shared-group-image-prep-2026-10-01/`.
