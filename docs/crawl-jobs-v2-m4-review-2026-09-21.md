# Crawl Jobs V2 M4 independent review — 2026-09-21

**Verdict: NO-GO for advancing this revision to image preparation or a real-Redis run.**

Two fresh reviewers independently audited correctness and security. They found
one HIGH and three MEDIUM defects. The coordinating agent replayed all four
counterexamples and confirmed the reported behavior using the reviewed code and
local fakes. The implementation has not been fixed by this review.

This file preserves the review evidence. The mutable status and remediation
checklist remain in [the F3 implementation plan](crawl-jobs-v2-plan.md).

## Scope and identity

- Case: `ledger-smoke-v1`, including controller, executor, RESP transport, ACLs,
  offline artifacts, configuration, packaging and existing tests under
  `tests/crawl-jobs-v2-redis/`.
- Related scope: the Go artifact/wire cross-check, Spider builder inputs,
  normative sections 5.1/17.7 and the M4-P2 plan.
- Reviewers: separate **Code Reviewer** and **Security Engineer** subagents, not
  the implementation agent. Their sessions were
  `ses_f3b7cafbeffeJAND71F60uoBfA` and `ses_f3b7caf9bffeHANgja5tQcdLmc`.
- Base HEAD: `b931f36ada3ed9a4495b8661d1444f10ff73265f`. Review covered the actual
  modified/untracked worktree, not merely the contents of that commit.
- The exact 28-file reviewed SHA-256 inventory is embedded in
  [`reproduce_correctness.py`](../tests/crawl-jobs-v2-redis/review/reproduce_correctness.py).
  Its canonical inventory digest is
  `feab36975d5f58cabe13e86b014256280c07912138363e8f36ed24e2e3a7f56d`.
  Both the reviewer and coordinator checked those hashes before recording this
  report/status updates. Later planning-document edits intentionally differ from
  that snapshot; do not repin the inventory to conceal drift.

Critical reviewed implementation identities:

| File in `tests/crawl-jobs-v2-redis/` | SHA-256 |
|---|---|
| `controller.py` | `dd044b259fd1da45365c3c14b7d322420fb5b44eb29fc08c86e7ef1d826f9fff` |
| `executor.py` | `81c246dd5bd29b1ec2e5ae62bc64ad9128fb3a9b176be97d13900c2bc873e603` |
| `runtime_case.py` | `d8c00fbf04367a5d765ad0f91c385c6c93ef251d7d69f1bed51b171b6c5babbf` |
| `resp.py` | `b702c4a1190bcbecc299eb7e9637b7cbe898ef8358cf16a34a277f84d5402fc2` |

## Findings

| ID | Severity | Finding | Status |
|---|---|---|---|
| COR-1 | HIGH | A timed-out Docker CLI does not stop the in-container executor before revocation starts | Open |
| COR-2 | MEDIUM | Surviving subprocess-group members escape termination when their leader has exited | Open |
| SEC-1 | MEDIUM | Permission errors and timeouts are accepted as proof of session termination | Open |
| SEC-2 | MEDIUM | Slow preflight/Docker operations can cause a Redis start after approval expires | Open |

### COR-1 — Timed-out executor can overlap revocation

**Locations:** `controller.py:75–81`, `219–225`, `391–402`;
`executor.py:307–320`.

The command timeout kills the local `docker exec` CLI process group. It does
not terminate or wait for the in-container exec process. The worker has only
per-Redis-command timeouts and no overall stage deadline. On a timed-out
`resume`, the controller immediately starts the `revoke` stage in the same
executor container; the earlier stage can still be running. Container removal
occurs afterward.

Docker's distinction between attaching client and exec lifetime is supported by
[Moby v27.5.1's exec router](https://github.com/moby/moby/blob/v27.5.1/api/server/router/container/exec.go),
which calls `ContainerExecStart` with `context.Background()`. This is source
evidence, not observation of the eventual target daemon.

The local probe invokes the actual `controller.execute()` with a backend that
models that independent lifetime. It observes revocation entering while the
timed-out `resume` remains alive. The final report stays `FAIL`; this finding
does not allege a false-PASS result. The defect is continuing execution beyond
the command boundary and overlapping writer activity with teardown.

**Required remediation:** impose a stage-side deadline and explicitly terminate
and wait for the active executor stage before revocation. Do not rely on killing
the attaching CLI. Preserve a separate, usable teardown path after stopping the
worker.

**Regression gate:** a blocked stage stops before revocation, issues no later
Redis commands, and cannot overrun the separate cleanup budget.

### COR-2 — Exited leaders leave subprocess children alive

**Location:** `controller.py:75–81`.

The exception handler calls `os.killpg()` only when `process.poll() is None`.
An exited leader can leave children in its group holding stdout/stderr open.
The selector then times out, but the process-group termination branch is skipped.

The local OS-boundary fake calls the actual `controller.command()` with an
exited leader and child-held output pipes. It raises `COMMAND_TIMEOUT` and
records **zero** calls to `killpg`.

**Required remediation:** terminate the owned process group on abort independently
of whether its leader has exited, before final reaping, tolerating an already
absent group. Keep lifecycle/identity handling safe when the leader exits.

**Regression gate:** cover surviving children after leader exit for timeout,
output-limit failure and interruption, in addition to the existing sleeping-
direct-child test.

### SEC-1 — Unproved session termination is certified

**Locations:** `executor.py:162–179`, `resp.py:75–80,90–92`,
`controller.py:258–263`.

After `ACL DELUSER`, the held-session probe accepts any `RedisError` or
`TransportError` and reports `held_session=terminated`. A Redis `NOPERM` reply
is not proof that the peer disconnected. A timeout is also not observed EOF or
reset. The transport collapses these incomplete-command conditions, and the
controller accepts the resulting termination claim.

The probe uses the actual revocation function, RESP encoder/parser and controller
validator with in-memory sockets. It injects acknowledged deletion plus rejected
fresh authentication, then separately supplies a held-session `NOPERM` or timeout.
Both are accepted as termination even though no peer EOF/reset was observed.

This does not assert that real Redis `DELUSER` has broken semantics or establish
a production access bypass. It proves that the harness's evidence oracle cannot
detect the ambiguous/negative condition it is supposed to reject. Locally closing
the socket after an error is not proof of server-enforced termination.

**Required remediation:** distinguish observed peer disconnection from ambiguous
transport errors and Redis error replies. Accept only the appropriate positive
disconnection evidence; preserve the fresh-authentication and server-reachability
checks.

**Regression gate:** held-session `NOPERM`, `NOAUTH`, generic Redis errors and
timeouts invalidate proof. Observed EOF/reset plus rejected fresh authentication
can pass. Server unreachability cannot stand in for failed authentication.

### SEC-2 — Approval expiry is not a Docker dispatch deadline

**Locations:** `controller.py:166–175`, `277–301`, `305–330`;
`runtime_case.py:95–97`.

Approval is validated before revision verification. Afterwards the controller
starts a fresh `monotonic() + max_seconds` budget that is not capped at approval
expiry. `create()` checks `remaining()` once, before an inspect/create/inspect/
start/inspect sequence. Docker commands use the later lifecycle deadline.

The local fake invokes the actual controller and `Docker.timeout()` calculation:

| Event | Elapsed time |
|---|---:|
| Valid approval checked; execution budget 120 seconds | 0 s |
| Three successful revision checks finish | 80 s |
| Redis absence inspection completes | 100 s |
| Redis creation completes | 120 s |
| Approval expires | 125 s |
| Created-container inspection completes and Redis starts | 140 s |
| Next expiry check rejects the run | 140 s |

Each modeled Docker delay is under its actual allowed timeout. At the expired
start, the timeout calculation still grants 30 seconds. This requires slow local
operations, not a hostile daemon, forged approval or clock rollback. The final
report is `FAIL` and simulated cleanup succeeds, but those outcomes do not undo
dispatching the start outside the approval window.

**Required remediation:** revalidate approval after slow preflight, cap the
backend deadline at both the execution budget and approval expiry, and enforce
the deadline immediately before each mutation dispatch. Cleanup must retain its
separate deadline so expiry does not prevent revocation/destruction.

**Regression gate:** delay revision checks, journaling, inspections and creates;
no subsequent start/new fixture mutation may be dispatched after expiry. Include
expiry between create and start, and prove cleanup still runs.

## Reproduction commands

From the repository root; these use local fakes, not Docker or Redis:

```bash
python3 -B tests/crawl-jobs-v2-redis/review/reproduce_correctness.py timeout-exec-order
python3 -B tests/crawl-jobs-v2-redis/review/reproduce_correctness.py timeout-process-group
python3 -B tests/crawl-jobs-v2-redis/review/reproduce_security.py --case revocation
python3 -B tests/crawl-jobs-v2-redis/review/reproduce_security.py --case expiry
```

All four were replayed by the coordinator. These scripts assert that the
reviewed defect is reproduced; exit success is **not** an acceptance-test pass.
They should stop reproducing after remediation and then be complemented by
normal regression tests for the corrected behavior.

The separate `inventory` mode of `reproduce_correctness.py` verifies the original
28-file snapshot. It intentionally detects later status-document or code edits.
Do not expect that full snapshot check to pass after recording this review in
the mutable plan.

Reproduction artifact SHA-256 values at review completion:

- Correctness: `95941d14af1b28e41c590a0269273d5b12d7a32377f3a3fa307f675b06feab56`
- Security: `17fe68acc898b833eac3751e4973873c12943fa036993a835ed36552fd1b415a`

## Verification and limits

The correctness reviewer recorded:

| Check | Result |
|---|---|
| Existing Python harness/execution suites | PASS: 26 tests, 12.378 s |
| Go artifact/wire cross-check | PASS: 1.949 s |
| Same Go cross-check under `-race` | PASS: 3.180 s |
| Strict bundle-pin check | PASS: 43/43 source identities |
| Independent digest verifier | PASS |

The security reviewer also passed the four targeted existing tests for cleanup
ownership/daemon errors, approval rejection, RESP redaction/bounds and ACL
separation. Passing existing tests did not cover the four counterexamples.

The scoped secret assessment covered thirteen harness/config/test/doc files,
with no value-bearing output, environment/credential-store inspection or history
audit. No production secret/private key was identified. One URI-shaped candidate
was a negative-test input with no userinfo. The coordinator repeated that scan
and confirmed its filename/context-only output.

A local profiling observation measured 109.71 MiB of simultaneously traced Python
allocations during real `executor.validate_request()`, before untraced/interpreter
overhead, versus the 128 MiB init limit. Validate target Linux peak memory during
reviewed image validation. This is **not** a confirmed cgroup OOM or an additional
implementation finding; do not infer a safe memory limit from the local result.

Neither review established target Redis ACL/session semantics, AOF durability,
Docker isolation/mount behavior, target-image memory usage, actual teardown,
image-build results or protected CI. No services, retained-data queries, public
crawls, implementation fixes, commits, pushes or PRs were part of the review.

## Exit decision

Fix COR-1/COR-2 and SEC-1/SEC-2, add the specified regressions, replay the
counterexamples and obtain independent re-review of the changed bytes. Only
then advance the reviewed revision through image preparation/protected CI and
the separate exact-artifact real-Redis execution approval. Full M4 acceptance
and the later operation/maximum-shape matrix remain separate gates.
