# M4 init admission diagnosis and correction — 2026-09-22

**Result:** the pre-start failure is reproduced and corrected. Independent
correctness/security reviews and revised Linux/arm64 image validation pass.
The corrective checkpoint still needs protected CI and fresh exact-artifact
approval before another smoke attempt. The [implementation plan](crawl-jobs-v2-plan.md)
owns subsequent status.

## Diagnosis

The first [approved attempt](crawl-jobs-v2-m4-smoke-report-2026-09-22.md) failed
before init started and retained only `failure_phase=init`. Its unchanged report,
intent, events and consumed approval remain historical evidence.

A fresh, metadata-only create/inspect/remove diagnostic on Docker Engine 29.5.2
(Docker Desktop 4.75.0, Linux/arm64) reproduced the admission failure. For the
requested `--cap-add CHOWN`, Docker reports:

```json
{"HostConfig":{"CapAdd":["CAP_CHOWN"]}}
```

The controller at `340906c` required exactly `["CHOWN"]`. Replaying the actual
captured metadata against that Git revision rejects `ISOLATION`; changing only
the observed spelling to `CHOWN` makes it pass. Both independent reviewers
repeated that offline counterfactual. No other isolation predicate needed changing.

The diagnostic fixture was `b65a8abbc5f42e7b7481ec8afc76d293`. It started no
container, provisioned no credentials, and removed the init container and both
fresh volumes. Its safe metadata projection, excluding environment and other
unneeded inspect fields, is the checked-in regression fixture.

## Corrective scope

- Init permits exactly one added capability, spelled `CHOWN` or `CAP_CHOWN`.
  Other roles permit only no added capabilities. Duplicate/extra/unknown caps
  reject; `CapDrop=["ALL"]` and all other isolation checks remain enforced.
- Admission failures identify only a closed category and failed field names.
  Main, revocation and cleanup reports omit inspected values and raw exception
  text. Serialization revalidates mutable diagnostic objects.
- Eight new regressions cover captured metadata, both spellings, privilege
  broadening, other isolation controls, redaction and metadata-only preparation.
- Image preparation now creates, inspects and removes stopped specifications for
  init, executor, Redis and revocation before its existing image checks. It
  requires `Status=created`, `Running=false`, integer PID zero and proven cleanup.
  A failed admission/cleanup prevents subsequent checks and plan/recipe export.
  The existing protected `required-build` job invokes this same preparation path.

## Independent re-review

| Reviewer | Decision | Independently performed evidence |
|---|---|---|
| Correctness | GO for scoped publication and image preparation | 53 harness tests, 21 script tests, Go wire/race, generator/digest checks, exact old-code replay, fake-backed helper export/cleanup and diagnostic checks |
| Security | GO for scoped publication and image validation | 16 targeted tests, 584 capability/role combinations, credential-canary redaction probes and 11 metadata-preparation failure scenarios |

Neither reviewer found an actionable new issue. Their review used source and
offline/local fake-backed checks; coordinator-recorded Docker observations were
inspected, not independently rerun by the reviewers. No review grants smoke-run
or merge authority.

Exact reviewed implementation/test bytes:

| File | SHA-256 |
|---|---|
| `tests/crawl-jobs-v2-redis/controller.py` | `e1695bf6918cb6140becadb5de798820fe40542180fb4da585a577660bf8a08e` |
| `scripts/prepare-crawl-jobs-v2-images.py` | `955af3ebe688bebc4d350edcd7780a058f2ed2f2fc6cee5a663024cb5f7dd8aa` |
| `tests/crawl-jobs-v2-redis/test_container_admission.py` | `aa1183d603aa410f378c4cd2239fa6799dbe34fc77fc9843b47a3283ed4308c0` |
| `tests/crawl-jobs-v2-redis/testdata/created-init-docker-29.json` | `c998848e5136205930ce0657c2299151170b45263638fcb0af1f9ab98e8eceba` |

The harness README was also reviewed at
`f46d97ac15675431ac1086f0ca2987f38a322cb033a085e75f54144c11e81acd`;
subsequent documentation updates record this outcome. Runtime bytes stayed frozen.

## Revised immutable image validation

The pinned Python 3.13.15 arm64 build completed with network disabled. Validation
passed against the rebuilt image, with exact source identity and a 55-file
inventory. The normative contract, Lua sources and bundle seal are unchanged.

| Artifact | Identity |
|---|---|
| Harness image | `sha256:2059066be4f192b84d4932050d1f811ed2bacf275e7d7cc0179f3e709d50e12c` |
| Redis 7.4.11 image | `sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c` |
| Plan | `d06a4ef887125883ecb1f0924f9192f4070bdb01c003d17ab718ca4baa451fbb` |
| Recipe | `9b0adc08f054775c24922a84012d2c4dbd950f630e151226536bd2e619ff81ab` |
| Image-validation report | `a7d25edcedd8249e409eab534c598af3408646d8d77386014201304ae8c6d7d4` |

All four stopped-role admissions passed for fresh fixture
`ac1d38f874c168320ea639276211a772`; none of those containers started and all six
resources were removed. Separate image checks ran only Redis `--version` and
isolated Python validation. Observed init/executor memory peaks were respectively
48,914,432 / 49,053,696 bytes under their 128 / 256 MiB limits, with zero exits,
no OOM, and verified cleanup. Separate label-filtered listings found no remaining
case or image-check resources.

[Exact artifacts](evidence/m4-init-fix-2026-09-22/) also preserve the original
diagnostic and the preliminary corrected metadata-only check. These results do
not establish successful init-stage execution, Redis startup, BOOT, persistence,
ACL behavior or M4 acceptance.

## Verification and next gate

Coordinator local checks passed: 53 harness tests, 21 script tests, Go
`TestM4OfflineArtifacts` with `-race`, generator `--check`, independent digest
verification, artifact/reviewed-byte cross-checks and scoped diff checks.

The owner authorized the scoped correction/evidence commit and push plus draft
PR #10 update. After protected checks pass on the published correction, request
a new approval binding the exact commit and artifacts above, platform, operator,
300-second case/60-second cleanup bounds and expiry. Preserve the consumed old
approval. No smoke retry occurred during this diagnosis/correction task.
