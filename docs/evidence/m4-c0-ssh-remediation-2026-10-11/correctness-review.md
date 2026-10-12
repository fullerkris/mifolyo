# Pinned-SSH C0 remediation — independent correctness re-review

**Verdict: GO_FOR_CONTINUED_IMPLEMENTATION**

COR-SSH-002 and COR-SSH-003 are closed on the new frozen source. QA-ENV-001 is closed, and the earlier COR-SSH-001 selector-allocation correction is preserved. No new blocking correctness finding was identified. The changes are narrowly scoped and the new regressions exercise real local process/pipe behavior rather than only mocking cleanup success.

This GO covers **reviewed source and static artifact continuity only**. It is not publication/merge authorization, installation acceptance, permission to run native actors, or runtime/calibration acceptance.

## Source and evidence integrity

- Snapshot: `/tmp/opencode/m4-c0-ssh-remediation-2026-10-11/snapshot`.
- Inventory SHA256: `2ecf2c68b6a3293cc3b34314e23022b3f35c2eda69f9f8efa0391728804a8edf`.
- Remediation record SHA256: `be92677e3a523e0659800cbd6cf59944f6f1ad23994cd9175fcc485f1308d99b`.
- All **45** package files and the copied runner were hashed **before and after** review. No package byte changed; no extra package file/bytecode appeared.
- Runner SHA256 remains `9afb715125f734f5b905da63dde5c9787fc5997b2e5a73a0bf62d781b23aef84`.
- Compared with the original `f53058a9…bc227` snapshot: only `context_transport.py` and `test_context_provisioning.py` changed. Reviewed both complete deltas and the corrected transport in its unchanged context/backend contracts.
- New transport file SHA256: `b8d5d1f1dd568e66115ee6745cb385965f5e7dfac63e7b3d9f0c0979020cc1eb`.
- Recomputed transport bundle: `20b53849d3617f1a7f1dc2897cd2ab3fed48990b8df188317c8c136d8276869a`.
- Original snapshot/runner and **all 21 files in the original correctness evidence manifest**, plus `SHA256SUMS` itself, remained byte-identical. Initial report/probe bytes were not overwritten. Its manifest SHA256 remains `bd6d0da1a72840f6315c992de2c61dddbd1226547a910b44c5c17ba0f2ecc0ea`.

This review supplements the original full provisioning/transport correctness review; it does not silently replace that historical NEEDS_REMEDIATION report. New retained outputs are only in the new mode-0700 `correctness/` directory. The requested suite used its owned temporary fixtures under `TMPDIR=/tmp/opencode`. No repository/frozen source, documentation, or Obsidian mutation and no subagent spawning occurred.

## Closure decisions

| Finding | Status | New snapshot location | Independent evidence |
|---|---|---|---|
| **COR-SSH-001** — selector allocation after spawn leaked child/pipes | **FIX PRESERVED** | `context_transport.py:64–71,119–122` | Selector still precedes spawn; spawn/setup remain guarded. All three original failure-path regressions pass. Added control expires the deadline between selector creation and spawn: no child created, selector closed. |
| **COR-SSH-002** — empty selector before setup misclassified as pipe EOF | **CLOSED** | `context_transport.py:66,73–77,106–118` | `setup_complete` becomes true only after every pipe is nonblocking and registered. Early non-killing reap requires that flag and a drained selector. Otherwise group kill precedes wait. Original two counterexamples no longer leak their descendants. |
| **COR-SSH-003** — reusable source lost deployment/transport packet checks | **CLOSED** | `context_transport.py:157–180` | Private copied bindings retain source/deployment/transport identities. Each acquisition validates and snapshots the packet, compares all bindings, then calls the original acquisition path. Original copied-packet mismatches all reject; source/mutable receipt cannot substitute different packet bindings. |
| **QA-ENV-001** — simulated 0755 code directory inherited umask 077 | **CLOSED** | `test_context_provisioning.py:229–258`, especially `234` | Explicit chmod establishes the fixture's promised mode inside its private parent. Affected test passed in the full umask-022 run and a separate focused umask-077 run. Production permission validation was not relaxed. |

### COR-SSH-002 replay and controls

Replayed the original `probe_cleanup.py` sequences: exited/unreaped leader, pipe-inheriting benign Python descendant, `waitid(WNOWAIT)`, and first `set_blocking`/registration fault injection. The original registered-pipes timeout control is retained.

| Replay case | Leader reaped | Parent pipes + selector closed | Descendant still live at settled check |
|---|---:|---:|---:|
| First `set_blocking` failure (`EIO`) | Yes | Yes | **No** |
| First registration failure (`ENOSPC`) | Yes | Yes | **No** |
| Registered-pipes timeout control | Yes | Yes | No |

The immediate process-state sample can catch SIGKILL in flight; the existing subsequent settled-state assertion confirms termination. A final owned-PID check found all three descendants absent. This is local owned-process evidence, not remote-stop proof.

The new package tests at `test_context_provisioning.py:535–599` also passed: they record **kill before wait** and observe actual inherited-pipe EOF through a retained duplicate reader. Additional independent controls show normal EOF/exit succeeds without a kill, whereas EOF from a still-live leader still rejects and kills it within the deadline/cleanup path.

### COR-SSH-003 replay and controls

Replayed the original `probe_preparation.py` construction-with-A/acquire-with-B sequence. Changing any of `runtime_context_deployment`, `runtime_context_transport`, or `runtime_context_source` now rejects with `CONTEXT_PROVISIONING_BINDING`. The corresponding fresh-factory mismatches continue to reject.

The full suite exercises copied and in-place packet mismatch, refusal before source authorization/exchange, mutations of the public provisioning receipt, fresh challenges on valid reuse, and successful reuse when only a non-provisioning artifact changes.

The six focused boundary controls additionally established:

1. Caller packet mutation during the authorization callback cannot relabel the in-flight request/lease: both stay bound to the admitted acquisition snapshot. Viewing that lease with the mutated packet rejects.
2. The same holds for caller mutation during simulated exchange.
3. The next acquisition of either mutated packet rejects **before authorization, exchange, wire-budget consumption, or new challenge consumption**.
4. A rejected mismatch does not poison later matching reuse, including another case; three successful acquisitions used three distinct challenges.
5. Deadline expiry between selector creation and spawn closes the selector without creating a child.
6. Normal EOF/exit and closed-output-pipes/live-leader paths retain their respective success and refusal/cleanup behavior.

The private subclass remains a `ContextSource`, so the existing backend type guard, source authorizer, daemon checks, signed protocol, lease expiry and pre-actor requirement continue to apply. No authority is taken from the mutable public receipt.

### Replay fidelity

Original probes are preserved at `/tmp/opencode/m4-c0-podman-arm64-2026-10-11/correctness/`. Copies in the new directory keep the same input/counterexample sequences. Relative `__file__` routing selects the new source/output directory. Changes are the expected post-fix assertions/result labels and an absence-tolerant PGID observation, necessary because successful cleanup can already remove the descendant. `probe-replay-deltas.patch` records the exact differences. Boundary controls are separate from those copies.

## Independent test run

Executed the complete frozen suite **once**, not copied from the main-session reports:

```sh
umask 022
TMPDIR=/tmp/opencode python3 -B -W error::ResourceWarning \
  scripts/test-crawl-jobs-v2-observer.py \
  --report /tmp/opencode/m4-c0-ssh-remediation-2026-10-11/correctness/c0-tests.json
```

**202/202 PASS**, 13.394 seconds, local CPython 3.14.7. No skipped, failed, errored, expected-failure or unexpected-success tests; no ResourceWarnings, ignored exceptions or tracebacks in the retained logs. Inventory/started/passed IDs are identical; all prior **197** IDs remain, with exactly the stated **five** new IDs. The report's 45 source hashes and runner hash match the new inventory.

Separately ran only `test_context_provisioning.ProvisioningTests.test_ssh_entrypoint_checks_exact_code_inventory_and_interpreter` under **umask 077**: **1/1 PASS**. This was not a second full-suite run. Both counterexample replays and all six additional focused boundary scenarios passed.

The main session's red run and full 022/077 results were inspected and hash-checked against `remediation.json` as supplementary history. They were not substituted for the independent run. The source/ID-only suite report is deterministic and therefore has the same SHA256 as the matching main-session report; the independently generated execution log is retained separately.

## Static artifact/input continuity

No rebuild was needed or performed for this Python transport/test-only delta. `check_artifact_continuity.py` verified actual bytes, not just the remediation record's PASS flags:

- Rehashed all **nine native** and **ten oracle** build inputs in both snapshots: exact inventory equality.
- Rehashed the **11 raw native artifact files**: all equal the original independent `artifact-checks.json` identities.
- Rehashed raw oracle manifest/interpreter: both equal the prior independent checks. The native target, observer and raw oracle interpreter still parse as ELF64 little-endian **AArch64 183 / ET_DYN 3**, with unchanged lengths/hashes.
- Regenerated the target instruction manifest with the frozen pure generator; joined its bytes to the actual target ELF again.
- Re-extracted the observer's actual compiled filter symbol bytes, compared them with retained BPF and current generator, and rechecked all three profile files.
- Rechecked actual toolchain target/observer hashes and nine source joins; rechecked oracle payload source hashes, aggregate source identity and interpreter join.
- Recomputed the host-source, transport-source and oracle-worker aggregate identities. Only the transport bundle changes to `20b53849…6869a`; host source and oracle worker remain unchanged.

Prior independent detailed ELF/symbol/instruction/header checks and **1,482 userspace BPF cases** therefore retain continuity through identical checked inputs and artifact bytes. Those 1,482 cases were not claimed as newly executed here; the additional current joins are recorded in `artifact-continuity.json`.

| Artifact | Unchanged SHA256 |
|---|---|
| target | `819cde0b38e12b235acbef2b3741551af86683ff2c3ec5186b5a45b710e5da45` |
| observer | `e407a02c67ac7a0cd192e612afe5ad643701c16821f6854e3a11b040e55cfb61` |
| oracle interpreter | `cb4548d280f38c2b1df4cba45f12c90a034729e08fce5bf1a223c58add0e21e3` |
| oracle manifest | `3f51809ebddbad24cbe03611e67b678df003121b77aff0743de0251baf538131` |

## Remaining boundaries / non-blocking limitation

- No live SSH authentication, real host collector, target, observer, oracle worker, raw ARM64 oracle interpreter execution, tracing, native calibration, Docker/Podman workload or host-policy mutation occurred. The suite's real SSH invocation is offline `ssh -G` configuration inspection only.
- Live host installation/account/ancestor permissions, effective sshd configuration, out-of-band key provenance, collector/daemon kernel placement, runtime OCI enforcement, hardware tracing feasibility, timing, whole-trial confinement and independent remote cleanup remain unaccepted.
- No fresh observation of build-engine/rootless/network/container-absence state or independent extraction of compiler/library/base-layer bytes was made. This is static continuity to the prior reviewed reconstruction, not a new runtime build attestation.
- The existing **non-blocking private-key preflight limitation** is unchanged: the staging validator checks OpenSSH envelope/size, not decoded private-key validity or private/public pairing. The deliberate non-key fixture still passes private-bundle validation. No live authentication was attempted and this is not an authentication-bypass finding. Installation must not treat that staging receipt as proof of authentication.
- Planned x64 implementation remains outside this ARM64 increment; absent x64 code is not counted as a defect.
- In-process private Python fields are not a sandbox against hostile callers executing arbitrary Python. The corrected public acquisition behavior, not protection from deliberate private-field mutation, was reviewed.

## Exact evidence hashes

| File in this directory | SHA256 |
|---|---|
| `source-before.json` | `af993378a03d8f5a92ae623839413e345a58f772057ce466aee7f8c448de7c14` |
| `source-after.json` | `c741481f2bf4d211022147efc7e45b165677b5f40746d44e26a82ca341382b39` |
| `c0-tests.json` | `1d8d1cd2b3cecf39bbc61652968971a40c853902c2d08df91cb42502fa6494fc` |
| `c0-tests.log` | `d9a246461a0b0adb0c0f1a491b0f08851aad3ab077902b46dea11c4144304d34` |
| `test-verification.json` | `bbf042f8a04a7438664238552d28289b0c8c2406faac163161cfe0632eb7f1d3` |
| `probe-cleanup.json` | `198f8b84e868a92e827958cc7449332e5f8899aa0b874f62685c738aecc79c9d` |
| `probe-preparation.json` | `fe12c41473d686dd52270499d29de615b6fa1eb38387753f5e9e18dce2232126` |
| `probe-boundaries.json` | `bad48ce2b340eb602c6a96ab81d602fdcf0070f99970f972059420f148eb1c64` |
| `qa-env-077.json` | `e18f02775a052f83059e2f08ee936cd2c953bb6aefc00fd3580b99e059b4d764` |
| `qa-env-077.log` | `52536f4378bdcb072740e499d5156e4fbb3207d104f4e7b02d9e7950e874fdd9` |
| `artifact-continuity.json` | `bc0c6a80b784ae4ef56c1aa21d6e48a692bc48d9135965877eb025e4f420c4f2` |
| `probe-replay-deltas.patch` | `beb6cc9dcdc20175d31067254a3df2befc649d87185720f63c7b7a446b03ce5a` |
| `owned-process-postcheck.json` | `a3c039f15ac772031f0c68f7a1ff10d940d404c0cfc3447f4306de0bfe5f1cf5` |

Finalized `review.json` and `review.md` hashes, along with all retained scripts/logs, are listed in `SHA256SUMS` and returned to the parent session.

## Next step

The demonstrated remediation is complete for this correctness gate. Continue the planned implementation using the new transport identity and regenerated preparation bindings, while keeping publication and actual host/native execution behind their separate approval and acceptance gates.
