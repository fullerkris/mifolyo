# Crawl Jobs V2 M4 remediation and independent re-review — 2026-09-21

**Final verdict: correctness GO and security GO for image preparation only.**

The four findings in the [original review](crawl-jobs-v2-m4-review-2026-09-21.md)
are closed. A related HIGH closed-Unix-peer regression discovered during the
first re-review was also fixed and independently re-reviewed. No actionable
finding remains open from these scoped reviews.

This is not a Docker build, protected-CI result, real-Redis acceptance, production
release or crawl authorization. The mutable status/checklist remains in
[`crawl-jobs-v2-plan.md`](crawl-jobs-v2-plan.md).

## Scope and review sequence

- The owner requested fixes, regression coverage and independent re-review.
- Review covered actual worktree bytes based on HEAD
  `b931f36ada3ed9a4495b8661d1444f10ff73265f`; the changes are not represented as a
  published checkpoint.
- Separate Code Reviewer and Security Engineer sessions continued independently:
  `ses_f3b7cafbeffeJAND71F60uoBfA` and `ses_f3b7caf9bffeHANgja5tQcdLmc`.
- The first correction passed 41 local Python tests. Correctness closed COR-1
  and COR-2 but found a HIGH follow-up in the held-session probe; security closed
  SEC-1 and SEC-2. The combined verdict therefore stayed NO-GO at that point.
- A receive-only probe and real local Unix socket-pair regression resolved the
  follow-up. Both reviewers then returned scoped GO on the final bytes below.
- The original report and all three dated reproduction scripts remain unchanged.

## Findings closure

| Finding | Final status | Corrected behavior and evidence |
|---|---|---|
| COR-1 — HIGH: executor continues after attaching CLI timeout | Closed | Hard timer covers worker stdin, validation, I/O and output. Controller stops/waits/verifies zero PID/removes the actual worker before creating a fresh, differently named revocation helper. No controller retry of an ambiguous readiness exec. Workers are removed before Redis/volumes on failure. |
| COR-2 — MEDIUM: exited leader skips group termination | Closed | Abort attempts termination of the owned process group before polling/reaping the leader. Timeout/output-limit/interruption regressions pass; an independent real local-process probe observes EOF after a pipe-holding child is terminated. |
| SEC-1 — MEDIUM: ambiguous errors certify termination | Closed | A bounded receive-only check accepts only receive EOF/reset. Buffered/new data, Redis error bytes, timeouts and local socket errors cannot prove termination. Fresh authentication must still fail with `WRONGPASS` while the server is reachable. |
| SEC-2 — MEDIUM: dispatch after approval expiry | Closed | Approval is revalidated after revision checks and intent journaling. Every Docker dispatch checks approval wall time and a capped monotonic deadline. Mutation boundaries are rechecked. Cleanup retains its separate budget and cannot restart Redis or the old worker. |
| Closed-peer follow-up — HIGH, found during re-review | Closed | Sending PING to an already-closed Unix peer could raise EPIPE before reading available EOF. The new probe sends nothing, observes EOF/reset directly and rejects ambiguous outcomes. A real AF_UNIX socket-pair regression completes revocation and validates the resulting receipt. |

### Worker lifetime and cleanup

`controller.py` now uses a stop/wait/PID-zero/remove sequence against the owned
executor. Unknown termination prevents revocation and invalidates the case.
The cleanup helper has a different pre-journaled name, the admitted harness
image, non-root UID, no capabilities and only the read-only control volume.
This avoids allowing a delayed exec request to enter a restarted old container.

`executor.py` applies a hard process timer of at most 30 seconds to the complete
stage, including blocked input, validation and output flushing. Redis startup
polling is read-only and stays inside that single stage. Local process probes
independently verified exit code 124 for blocked stdin, validation and output.

The independent security review exercised helper admission and failure paths,
including ownership mismatch, nonzero worker PID, failed or ambiguous helper
creation, exhausted cleanup budget and failed revocation. These did not produce
valid acceptance evidence. A separate teardown budget does not grant new ordinary
execution authority after approval expiry.

### Disconnection proof

`resp.Client.observe_peer_disconnect()` performs no write. Only EOF or a
`ConnectionResetError` originating from `recv` is accepted. A reset from
`settimeout`, EPIPE, ENOTCONN, EBADF, timeout, buffered data and received data all
reject. The socket closes in `finally`; local closure is not used as evidence
that the server terminated the session.

Independent local Unix socket-pair checks covered orderly close, live/idle
connections, live data, data followed by close and prebuffered data. The positive
revocation test runs the real executor/RESP code with local IPC and synthetic
Redis administrative replies. It establishes local socket behavior, not actual
Redis `DELUSER` acceptance.

The old security reproduction was PING-driven. The current regression bridge
adapts only its fake delivery timing so the same `NOPERM` bytes reach the
receive-only probe. It does not reinterpret an empty obsolete fake buffer as
peer EOF. The archived script itself was not edited.

## Verification

| Check | Recorded result |
|---|---|
| Author's final Python suite | PASS: 43 tests, 20.454 s |
| Independent correctness Python suite | PASS: 43 tests, 19.991 s |
| Independent security targeted suite | PASS: 12 tests, 3.456 s |
| Independent Go artifact/wire race check | PASS: 3.093 s |
| Independent local process-group and three worker-timer probes | PASS |
| Independent receive-only socket and error-injection probes | PASS: EOF/reset positives and live-data/timeout/local-error negatives |
| Strict bundle pins and independent digest verifier | PASS: 43/43 canonical source identities; normative document/source identities unchanged by remediation |
| Scoped secret/output assessment | No matches in the five final changed source/test/README files; controlled error messages and redaction tests pass; not a history/environment audit |

The harness suite now contains 26 prior tests plus 17 review regressions. Current
verification commands from the repository root include:

```bash
python3 -B -m unittest discover -s tests/crawl-jobs-v2-redis -v
python3 -B scripts/generate-crawl-jobs-v2-bundle.py --check
python3 -B scripts/verify-crawl-jobs-v2-digests.py
```

From `services/spider`:

```bash
GOPROXY=off GOTOOLCHAIN=go1.25.13 go test -mod=readonly -race -timeout 120s ./internal/database/crawljobsv2 -run '^TestM4OfflineArtifacts$' -count=1
```

`test_review_regressions.py` exercises the corrected outcomes and bridges the
historical counterexamples. Historical reproduction programs assert the old
defects, so successful remediation can make their old assertions fail; they are
not current acceptance tests. Their original hash inventories intentionally
detect subsequent implementation/status edits.

## Final reviewed identities

The reviewers independently checked the same final hashes; the coordinator
rechecked implementation/test hashes before recording the verdict. Paths below
are relative to `tests/crawl-jobs-v2-redis/`.

| File | SHA-256 |
|---|---|
| `controller.py` | `a447f05228353cc73179fc4fc12ec8fa19602b289b483bc576695f923ed039ef` |
| `executor.py` | `2fe6410a88d9ad7f3f6bdcd26d65dd6dc4fbfb53c72c742423d3c6568a0dd518` |
| `resp.py` | `0643bf1d24b0855119459cb1098bc70c69aede3f53710d71262d9937c8b81d84` |
| `runtime_case.py` | `f94aed1ddd2754c4c430432f8469b0b5470bc42e7d66d66a565e120ae5b8b846` |
| `test_execution.py` | `07c65957a41254d94971ff77f16df17d42d9ca134929886696097e5ba7ab7058` |
| `test_review_regressions.py` | `066b65ceec270a5f0c1f8541a8ceb015aa9fd014c97db08f7abdb794d7770ec3` |
| `README.md`, before final verdict/status update | `a46ee4e6ea9b36b114ce1b4bb8a171abd32c2dce43e6655f6287e68efcdd1ed5` |
| `review/reproduce_rereview_correctness.py` | `033cedb981b72c7a2442640b5dcd9b14ed5397661787b3b63f6df45388353699` |

The corrected execution recipe digest is
`e001d497c1bdccb061a19f265ba854812d1da4e5e551bc0d9873ae9773fefc4d`.
The read-only recipe command still reports `execution_authorized=false`. Any
future run requires a new approval bound to these actual code/recipe artifacts;
prior recipe approvals cannot be reused.

## Remaining gates

Proceed to reviewed immutable image preparation and applicable protected CI.
After the separate execution approval, validate target Docker/Redis semantics,
including worker stop/wait,
role ACLs, session revocation, AOF/restart behavior and resource limits. The
previous 109.71 MiB local traced-allocation observation is not a target Linux
measurement; the 128 MiB init-helper limit still needs explicit validation.

Broader protocol/evidence/benchmark review, a clean tracked reviewed revision,
exact image/artifact identities and a separate execution approval remain
required. No Docker/Redis service, image build, network listener, public crawl,
retained-data operation or Git publication was performed in remediation/re-review.
Local Unix socket pairs and short Python child processes were used for tests.
