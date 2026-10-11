# C0 trusted context and oracle/client closure — 2026-10-09

**Current status: independent correctness/security review complete —
GO_FOR_CONTINUED_IMPLEMENTATION.** All three initial boundary findings are closed
on the corrected **40-file / 161-test** snapshot. See the
[review and remediation checkpoint](crawl-jobs-v2-m4-c0-context-oracle-review-2026-10-09.md).
The original implementation and its verification below retain their recording-time
39-file / 149-test bindings; source review does not establish runtime feasibility.

Work is on `feature/crawl-jobs-v2-c0-context-oracle`, based
on owner-merged `2ff03afa8950500de36762bdc5d41de8f2736030`. No commit or push is
part of this increment. The initial package had **39 files and 149 passing tests**.

## Completed baseline and vault handoff

PR #23's actual merge has the reviewed `a1fbc6f4…` tree. Its own main-push workflows
passed on attempt 1: fourteen protected contexts, 23 jobs, 113 C0 / 31 script /
183 harness tests, all 480 race roots accounted for (479 passes and the existing
optional native-Lua skip), all eleven M4 roots and 74-file / 20-recipe image
evidence. Main CI gate:
`8f2afe16f2482c79ab6a8c5776e7140beb38c82696d4e5d4a2f943f0d9a18213`.

The owner requested the Obsidian update first. MCPVault created a dated C0
publication/merge checkpoint and updated the master roadmap, implementation
tracker, chronological journal and documentation index. Updates were read back
before implementation. Their imported repository documentation remains a dated
snapshot; the current checkpoint links the later verified repository evidence.

## Implemented components

### Independently authenticated Linux context

- `linux_observation.py` reads fixed, bounded procfs/sysfs observations.
- `context_worker.py` is a read-only Linux/AArch64 host endpoint with fixed
  root-owned anchor and service-owned mode-0600 authentication-key paths.
- `runtime_context.py` binds fresh HMAC-SHA256 challenges, source/preparation/
  daemon identities, the independently provisioned boot/namespace anchor and
  a Linux-clock validity window. It refuses wrong placement, stale/replayed/
  modified responses, missing observations and foreign leases.
- `DockerBackend.acquire_runtime_context()` checks the actual selected daemon
  around acquisition. Actor data cannot supply the expected host context.

Source provisioning is an explicit trust boundary: the operator must attest the
host anchor in the Docker daemon's actual Linux environment and provide its
bounded transport. This source change does not establish or deploy that service.
No extra Docker host namespace, privilege, mount or kernel-policy fallback is
introduced. The key is not exported in reports or sent to actors.

### Native Linux expiry enforcement

The handshake now sends **case ID, scope nonce and exact uint64 Linux expiry**.
Target/observer enforce that expiry in their own kernel clock, including a check
after each accepted control-byte read. Backend admission joins the transmitted
window to the context lease and rechecks source/peer readiness at dispatch.
Controller-local timing alone cannot extend the native admission boundary across
host/VM scheduling pauses. Old preparation objects and native binaries require
replacement; new source and oracle ELF identities are mandatory.

### Read-only oracle and client observations

The reserved `/oracle/worker.py` entrypoint now has an implementation and a pinned
Python image recipe. The oracle's interpreter ELF, source bundle, process,
confinement, namespace, resource and clock observations are joined to the same
context and original owned target. The oracle retains its private PID namespace.

Its chained, bounded INIT / HELD / FINAL / CLOSE protocol independently admits
and reads `/witness/state`; complete ordinal/transcript/state checks run inside
the worker. P3 clients read only the fixed Unix socket and 1,024-byte synthetic
frame. The worker preserves partial data, exact completion time, EOF, transport
reset/error, late completion and unresolved closure.

Client receipt before the lower cut bound is acknowledged; receipt inside the
causal cut interval remains unordered. A later full reply is retained even if
the trial was pre-ACK at the cut. These synthetic receipts are not automatically
Redis ACK, durability or fault-acceptance evidence.

`OracleSession` requires owned admitted streams, source/identity/chain matches
and the actual HELD digest/ordinal. The backend requires a live initialized
oracle before G and a live matching oracle result before R, with no Docker
inspection in the held/resume path. Final closure joins stable inspections of
the original target/oracle generations as exited with PID zero. Local EOF alone
does not prove remote exit; reset/partial/unknown paths cannot receive false
closed-no-reply credit. Cleanup and independent absence proof remain required.

The oracle profile permits CPython's read-only file-backed executable mappings;
anonymous executable mappings, W+X, executable `mprotect`, ptrace and namespace
setters stay denied. Actual oracle OCI runtime admission still needs validation.

## Verification and version binding

| Check | Result |
|---|---|
| Complete C0 suite | **149 passes**, no skips/errors/expected failures or ResourceWarnings |
| Existing coverage | All 113 baseline test IDs retained; 36 new IDs |
| Script suite / independent digest vectors | **31 passes / PASS** |
| Native artifact-only build | GCC 14.3.0, AArch64, strict warning/error flags; static ELF/manifest/compiled-BPF joins pass |
| Oracle artifact-only build | Pinned Python 3.13.15/AArch64 manifest; source bundle and interpreter identities verified |
| Runtime execution | No target, observer or oracle worker executed in a real C0 experiment; no tracing |

The initial regression run exposed two old positive native-peer fixtures lacking
the new oracle-ready prerequisite. Explicit synthetic readiness was added only
to those controls; real-pipe bridge tests cover actual oracle protocol admission,
missing/lost oracle refusal, bad held data, resume gating, coordinator progression,
receipt-mutation isolation and restarted/unsettled remote-stop refusal. Original
failed logs are retained.

| Identity | SHA-256 |
|---|---|
| 39-file source inventory | `6dbc77112abc830467ee952eef2d30f54e730c8718093788e4dd486564c6c750` |
| Local verification | `9ad8a80a1e952f98c4c08c0a960ff8b9ed1c3cc38e2a7c131eb168bb547b6842` |
| Build verification | `327af5442c6d6594295fe53984f1f96a3be367083786998241f0f3fd3cc6fbb9` |
| Context source bundle | `f57f0c4d1ce76f227647596443976adc2a4b4abad7d293797817d29dd597fdbb` |
| Oracle worker source bundle | `120133859baf992d3f25028cdb48a7b7bd1f5852190bd15e5b0ee6679bca29a0` |
| Target ELF | `819cde0b38e12b235acbef2b3741551af86683ff2c3ec5186b5a45b710e5da45` |
| Observer ELF | `e407a02c67ac7a0cd192e612afe5ad643701c16821f6854e3a11b040e55cfb61` |
| Oracle interpreter ELF | `cb4548d280f38c2b1df4cba45f12c90a034729e08fce5bf1a223c58add0e21e3` |

See the [exact evidence index](evidence/m4-c0-context-oracle-2026-10-09/README.md)
and [package integration details](../tests/crawl-jobs-v2-observer/README.md).
The earlier 112-test independent GO remains bound to its original bytes. Both
initial reviews of this increment returned `NEEDS_REMEDIATION`; they are bound to
review intake `be961ae84551e2f3f4b1048a4a70f5f12b7356b312276fac9a26995d81bf5e59`.
Correctness report: `80e03a35f09f9ba53c76746b581557f8aea5e02092587c5c8374b9ffbae0215d`.
Security report: `814645cde86463ad003b78102619d80673dbe341c5a93cf15672ebee51fccce2`.
Their successor reviews both return GO on source inventory
`99d9dd6f2c475cd83960c66028016d7d508acadd3563f2e440c3bf6ff012c4c3`;
combined verdicts `cf04c87324bf8a02e37eb2e3099443c49ee0c25cf545d85686f60c1ef33e2ad1`.

## Remaining gates

1. Obtain the owner publication decision for the [prepared 53-path candidate](crawl-jobs-v2-m4-c0-context-oracle-publication-preparation-2026-10-09.md).
   Preparation is complete; reviewed package bytes are unchanged and fresh 161 C0 / 31 script checks pass.
2. Provision and admit the host context anchor/key/transport in the correct Linux
   domain; prepare/review/admit the exact runtime images and oracle sandbox.
3. Complete trial dispatch, per-trial receipt/timing assembly, whole-trial
   confinement/resource monitoring and cleanup/absence joins.
4. Obtain separate exact-artifact and execution decisions before native calibration.

The normative protocol, canonical Lua and earlier accepted execution records
remain unchanged. Full M4 is open. Private logs, intermediate builds and original
failure records are retained in
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-c0-context-oracle-2026-10-09/`.
