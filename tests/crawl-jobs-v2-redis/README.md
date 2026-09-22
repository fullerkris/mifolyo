# Crawl Jobs V2 M4 fixture harness

This non-shipped harness has an offline compiler (`harness.py`) and a bounded
first-case execution controller (`controller.py`). The compiler starts nothing.
The controller can create disposable infrastructure only through its explicit
`run` command with a separately reviewed, digest-bound execution approval.
Python 3.10+ and its standard library are sufficient for local tests.
Implementation is locally tested; real-Redis execution and acceptance are pending.

**Final independent re-review (2026-09-21): GO for image preparation only.** The
four original findings and a closed-peer follow-up are fixed and re-reviewed.
See the [closure report](../../docs/crawl-jobs-v2-m4-rereview-2026-09-21.md).
The [original NO-GO report](../../docs/crawl-jobs-v2-m4-review-2026-09-21.md) and
reproduction scripts remain dated evidence. Image and exact-revision CI gates
now pass; see the [bounded approval record](../../docs/crawl-jobs-v2-m4-ci-approval-2026-09-22.md)
for its recorded scope. The subsequent single smoke attempt
[failed in init](../../docs/crawl-jobs-v2-m4-smoke-report-2026-09-22.md), before Redis
startup; cleanup was verified. The one-case approval is used and must not be reused.

**Init correction (2026-09-22): GO for scoped publication and revised-image
validation.** Docker's `CAP_CHOWN` spelling is now admitted only as the equivalent
singleton init capability. Captured-metadata regressions, value-free diagnostics
and stopped-role image-preparation checks pass independent review. The rebuilt
image passes; new checkpoint CI and fresh approval are next. See the
[diagnosis/review report](../../docs/crawl-jobs-v2-m4-init-fix-2026-09-22.md).

## Implemented commands

From the repository root:

```bash
python3 -B tests/crawl-jobs-v2-redis/harness.py inventory
python3 -B tests/crawl-jobs-v2-redis/harness.py prepare \
  --inputs /absolute/path/to/reviewed-inputs.json \
  --expected-input-sha256 <sha256-of-exact-input-file>
python3 -B tests/crawl-jobs-v2-redis/harness.py validate \
  --plan /absolute/path/to/offline-plan.json
python3 -B tests/crawl-jobs-v2-redis/harness.py validate \
  --plan /absolute/path/to/offline-plan.json \
  --setup /absolute/path/to/offline-setup-projection.json
```

Compiler output is JSON on stdout; validation errors have a value-redacted message and
nonzero exit code. Commands write no files. The input hash binds bytes; supplying
a matching hash is not owner approval or proof of artifact provenance.

`inventory` checks the actual 43 canonical sources, contract, generated Go pins,
and shared fixture before reporting all 52 gate variants as `not_run`. It also
extracts section 17 requirements verbatim with stable content-bound IDs, empty
test/evidence mappings and `unimplemented` status. This is a coverage backlog,
not a report of tests that ran.

## Closed input schema

`prepare` accepts **exactly** these fields; unknown, missing, duplicate or
coerced fields fail:

| Field | Required value |
|---|---|
| `format_version` | Integer `1` (not boolean) |
| `scenario` | `ledger-smoke`, `administrative-fresh`, or `administrative-migration` |
| `redis_version` | Exact Redis 7 numeric version, at most 64 ASCII bytes |
| `redis_image` | Nonzero lowercase `sha256:<64 hex>` declaration |
| `harness_image` | Nonzero lowercase `sha256:<64 hex>` declaration |
| `standin_image` | Nonzero lowercase `sha256:<64 hex>` declaration for all seven unavailable participant roles |

No endpoint, marker, contract override, script source/path, render rule, secret,
or operator-selected setup key is accepted. Files/JSON are bounded at 2 MiB;
duplicate keys, floats, nonfinite numbers, invalid UTF-8 and lone surrogates fail.
There are no external JSON Schema/runtime dependencies: these closed schemas
and exact-reconstruction checks are enforced by the compiler and adversarial tests.

The compiler recomputes test descriptors, normal guard/compatibility RECORDs,
image-role mappings and source/config identities. All four evidence fields are
unmeasured test descriptors in this offline recipe. Real measured inputs and
BOOT evidence must be added through reviewed execution recipes later.

Every result is `purpose=conformance_only`, `execution_authorized=false`,
`release_eligible=false`, `measurement_status=not_measured`, with image and
isolation verification `pending`. A syntactically valid image digest does not
prove an OCI artifact exists, was built from this code, or is reviewed. Those
checks require the pinned target-image preparation gate. The unit-test image
hashes are fabricated lexical controls, never execution inputs.

`validate` rebuilds the entire plan using its closed inputs and current source
pins. Changed descriptors, flags, RECORD order, unknown fields, stale compiler,
config or contract bytes fail even if a caller recomputes their individual hashes.
An approved executor must additionally pin the exact reviewed plan digest;
offline self-consistency alone is not authority.

## Setup projection and ACL compiler APIs

`ledger_setup(plan, redis_time_ms)` produces an **offline projection** of the
four permitted direct ledger authority writes: active compatibility, contract,
stored guard and empty-legacy retirement. It emits named synthetic empty
artifacts, exact types/field bytes/digests, persistent expiry rules and required
candidate/freeze/legacy absence. It never writes durability or asserts BOOT
approval. The supplied time is an input, explicitly `time_observation_verified=false`;
the future runner must capture Redis TIME before materializing real setup.

`validate_setup` rederives that exact inventory and every byte. It rejects
additional or missing keys, duplicate writes, changed type/expiry/evidence,
zero guard evidence and plan substitution. This API intentionally rejects both
administrative profiles: their success markers must be produced by canonical
INSTALL/RETIRE/PROMOTE, with separately reviewed synthetic legacy fixtures.
It is not yet a general run/job/stage/maximum-shape setup engine.

`ledger_acl_selectors(wire_keys, data_read, data_write)` compiles a concrete,
bounded **ledger selector fragment**, excluding wildcard/ACL-token injection,
authority-as-data grants, legacy keys and unlisted wire keys. An EVALSHA-only
selector admits the wire inventory; separate selectors permit candidate/freeze
TYPE only, other authority reads, and bounded data commands. It contains no
credential or broad root command grant. The future case compiler must prove its
exact derived-key membership; this helper cannot infer it from arbitrary keys.

This fragment is not a complete ACL file. Provisioning, exact source loading,
BOOT, observation, administrative and revocation roles still need reviewed
integration. In particular, a successful static selector test is not proof of
Redis 7 command admission. The first approved real-Redis experiment must prove
outer EVALSHA and inner-command behavior together.

`redis.conf` pins the intended configuration bytes, not a Redis image. It uses
port `0`, a private Unix socket, a required externally provisioned ACL file,
400 MiB maxmemory, AOF/always/no-truncation/noeviction, and disables blanket flush,
module loading and debugging. The first-case controller verifies 528 MiB Redis
container memory and the actual image/configuration before its probe.

## First execution slice: `ledger-smoke-v1`

The implementation covers canonical BOOT and empty-inventory
`CJ2_MAINTAIN_RATE_SCOPES`. It does not implement the full 43-operation matrix.

| File | Responsibility |
|---|---|
| `runtime_case.py` | Closed first-case recipe, six role ACLs, exact source allowlist and BOOT/active wire construction |
| `resp.py` | Unix-socket-only RESP2; 2 MiB request/256 KiB reply bounds, command deadlines, redacted errors, no automatic write retry |
| `executor.py` | Fixed init/readiness/probe/resume/measure/revoke stages inside networkless containers |
| `controller.py` | Reviewed-commit/image admission, exact owned Docker resources, crash/restart sequence, evidence and cleanup |
| `Dockerfile.execution` and its ignore file | Narrow test-image context; digest-pinned Python base supplied during reviewed image preparation |

The read-only recipe command starts nothing:

```bash
python3 -B tests/crawl-jobs-v2-redis/controller.py recipe
```

It includes code/configuration fingerprints, the exact wire/storage inventory,
role selectors and limits. The ledger role is read-only for this empty-inventory
case. BOOT alone can mutate durability; setup can write only the four authority
records and the temporary persistence probe. The loader loads only the two pinned
canonical sources. The revoker has `ACL DELUSER`, not permission to recreate users.
Setup/loader/BOOT credentials are revoked before measurement.

### Lifecycle

1. Validate the exact approved plan/recipe/revision, expiry and architecture.
   Require every source input tracked and clean at the reviewed commit. Inspect
   already-local Linux image IDs; refuse missing images and never pull/build.
2. Persist an `INCOMPLETE` intent with exact resource names before any Docker
   mutation. Create two uniquely named/labeled volumes and a restricted init
   helper; verify emptiness before writing hashed ACL credentials/configuration.
3. Remove the init helper. Start a networkless executor and Redis, each with
   explicit UID, memory/CPU/PID/capability/read-only/restart/mount settings.
   The executor mounts only the private control volume read-only; it receives no
   data volume, host bind, Docker socket, production credential or external route.
4. Verify actual Redis version/configuration/AOF/standalone/memory state. Write
   one bounded acknowledged probe at the pre-BOOT contract key. SIGKILL Redis,
   restart the same container/volume and require a new run ID with exact probe
   bytes. Remove the probe before canonical BOOT approval and its exact replay.
5. Capture Redis TIME, compile/install/verify the exact ledger setup, and revoke
   setup/loader/BOOT access. Run active empty maintenance twice; compare complete
   key inventories and persistent-state hashes before/after each call. Check 21
   candidate/freeze content-read, write, delete, expiry and rename denials.
6. Stop, wait for and remove the actual executor container before revocation.
   Start a fresh, differently named, networkless cleanup helper with only the
   private control volume. Revoke every role; use a bounded receive-only probe
   to observe held-session EOF/reset, then require rejected fresh authentication
   while Redis remains reachable. The probe never sends PING to an already closed
   peer; data, permission errors and timeouts are not disconnection proof.
   Remove and verify only exact owned resources, with workers before Redis and
   volumes. Failed quiescence prevents revocation; any unproved cleanup
   invalidates the case result.

The init helper alone uses UID 0 with only CHOWN capability to initialize fresh
volume ownership. Redis and the executor use `65534:65534` with all capabilities
dropped. Both have network mode `none`. The controller communicates only with
the local Docker Unix socket, never a caller-selected Docker host.
Admission accepts only `CHOWN` or Docker's canonical `CAP_CHOWN` spelling for that
single init capability; no generic prefix normalization or additional capability
is allowed.

Execution is bounded at 300 seconds (approval may choose less), capped by the
approval expiry. Approval is revalidated after revision checks and intent
journaling; every Docker dispatch checks both monotonic and wall-clock limits.
Each exec process also has a hard stage timer (at most 30 seconds) covering stdin,
validation, Redis I/O and output. Startup polling stays inside one timed read-only
stage, rather than retrying an ambiguous `docker exec` from the controller.

Cleanup has a separate 60-second budget and may create its cleanup-only helper
after ordinary execution approval expires. It never restarts Redis or the old
executor. Killing the attaching CLI is not treated as worker termination.
SIGINT/SIGTERM enter cleanup, and later signals cannot skip it. An uncatchable
controller/host failure can leave resources: the durable
intent identifies them, and absence of a completed teardown report means no
valid evidence. This slice provides no automatic recovery/prune command.

### Image and execution approval gates

The corrected immutable arm64 image preparation passes; see the
[init correction report](../../docs/crawl-jobs-v2-m4-init-fix-2026-09-22.md) and
[new exact artifacts](../../docs/evidence/m4-init-fix-2026-09-22/README.md).
Preparation first verifies all four stopped container specifications using the
runtime admission path, checks created/PID-zero state, and proves cleanup. These
metadata containers never start; failure prevents further checks or artifact export.
The Dockerfile defaults to a reviewed immutable Python 3.13.15 index. Rebuild from
the reviewed tree using that base or an explicitly reviewed immutable override.
Inspect the resulting daemon-local image ID and the selected Redis image ID;
put those exact nonzero `sha256:...` IDs in a new offline plan. This first case
requires `standin_image == harness_image`. Tags/remote references are not runtime
identities, and this controller never starts participant stand-in services.

The execution approval is external, not generated or checked in by the harness.
It has exactly these fields:

| Field | Constraint |
|---|---|
| `version`, `case`, `approved` | Integer `1`, `ledger-smoke-v1`, boolean `true` |
| `operator` | Reviewed operator label, 1–64 ASCII letters/digits/underscore/dot/hyphen |
| `commit` | Exact clean, tracked 40-hex Git revision |
| `plan_sha256`, `recipe_sha256` | Exact canonical offline-plan and current recipe digests |
| `expires_at_ms` | Valid through the whole execution budget, no more than 24 hours ahead |
| `max_seconds` | Integer 1–300 |
| `architecture` | `amd64` or `arm64`, matching both local images |

After a separate execution approval, the available command is:

```bash
python3 -B tests/crawl-jobs-v2-redis/controller.py run \
  --plan /absolute/path/to/reviewed-plan.json \
  --approval /absolute/path/to/approved-execution.json \
  --expected-approval-sha256 <sha256-of-exact-approval-file> \
  --evidence-dir /absolute/path/to/existing-private-evidence-directory
```

This command ran once and failed in init; see the dated report. A matching input
hash does not create owner approval or permission to retry. The corrected
source/image/recipe needs new exact-revision CI and fresh approval; the consumed
approval for `340906c` cannot authorize these revised bytes.

The controller writes exclusive mode-0600 intent/final JSON files outside the
fixture volumes. Reports bind identities, actual container checks, probe/BOOT
evidence, setup/time observations, state comparisons, denials, revocation and
destruction receipts. Credentials travel via bounded stdin, never command-line
arguments, environment variables or report fields. Errors retain only closed
failure codes and admission-check names, omitting inspected values and raw server/
Docker diagnostics. `m4_accepted` is always false: even a successful future smoke
case cannot certify the remaining matrix. Injected fake backends always emit
`evidence_kind=simulated` and `case_evidence_valid=false`.

## Verification

```bash
python3 -B -m unittest discover -s tests/crawl-jobs-v2-redis -v
```

The tests cover artifact drift, zero/forged evidence, illegal setup, role and
ACL injection, schema/size violations, deterministic output, CLI redaction and
independent Python guard formulas. Execution tests additionally cover admission,
fragmented/oversized/truncated RESP, probe loss, boot/replay, role revocation,
ambiguous Docker creates, failed cleanup, interruptions, evidence files and
redaction using fakes. `test_review_regressions.py` covers the four review
counterexamples, worker quiescence ordering, a real local Python stage-timer
exit, process-group aborts, strict peer-disconnection proof and expiry between
create/start. A real local Unix socket-pair regression covers orderly peer close
before the probe. Two target-kernel network regressions and eight container-admission
regressions bring the local suite to 53 tests; it starts no Docker or Redis.
`test_container_admission.py` replays captured Docker metadata, rejects broader
capabilities, checks bounded diagnostics, and verifies metadata-only preparation
and failure cleanup. In `services/spider`:

```bash
GOPROXY=off GOTOOLCHAIN=go1.25.13 go test -mod=readonly \
  ./internal/database/crawljobsv2 -run '^TestM4OfflineArtifacts$' -count=1
```

That test consumes Python-generated artifacts through the ordinary Go codecs
and active transport, verifies zero rejection, and compares the 52 variants
against the independent literal Go wire oracle. It also compares every BOOT and
active-maintenance EVALSHA part and RESP size with the Go constructors. Ordinary structural acceptance
of test hashes demonstrates why final release provenance is a separate gate.
The Python suite runs in protected `required-tests`; the Go cross-check runs in
normal Spider tests. Docker copies these files into the **builder only**; the
runtime image copies only the Spider binary and existing configuration.

## Remaining before execution

The [implementation plan](../../docs/crawl-jobs-v2-plan.md) owns progress.
The corrected first-case execution layer and target-discovered corrections have
scoped independent GO. The init admission defect is corrected and re-reviewed;
rebuilt-image content, memory and stopped-role isolation checks pass. Complete
protected CI for the correction and obtain fresh approval before another run.
Actual Redis ACL/OS lifecycle validation has not been reached. Candidate-presence
fixtures, administrative transitions, job/lease/stage/commit behavior, maximum
shapes, crash-boundary coverage, benchmarks and final release/image admission
remain subsequent gates. Fake results never substitute for those observations.
