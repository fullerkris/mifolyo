# C0-ARCH-05 replacement transport — independent correctness review

**Scoped final verdict: GO_FOR_CONTINUED_IMPLEMENTATION**

**Concrete correctness findings: none identified in the reviewed scope.**

This is a fresh review of the fixed Unix-socket metadata transport, its protocol
bounds/deadlines, its integration into read-only host preflight, and preservation
of non-authorizing result semantics. The previous GO on inventory `4046bd...` was
not carried forward as approval of this different implementation.

The small, fixed request surface, explicit transport ownership, strict projection,
and separation of metadata from admission are sound foundations for continuing
implementation. This is not native-host admission, runtime calibration approval,
an approval of the unchanged runtime Docker adapter, or a full security verdict.

## Exact frozen source and preservation

- Snapshot: `/tmp/opencode/c0-native-preflight-2026-10-17/transport-snapshot`.
- Inventory SHA256:
  `faf8d794802f7a3b7603bea199689014cef21d415764571725ff9e3b66414baf`.
- Base: `babb2180d05698e64b7d9baf37f9cab14b3c0f7f`.
- **62/62 files verified before and after**: 59 package files, two scripts and
  the checkpoint document; no extra files in the frozen snapshot.
- Compared with the intermediate remediation snapshot: two added files
  (`engine_metadata.py`, `test_engine_metadata.py`), four modified files
  (`host_preflight.py`, `test_host_preflight.py`, README, checkpoint), and 56
  unchanged files. All original **55 context files remain byte-identical to base**,
  including `docker_backend.py`, the shared runner and actor code.
- `engine_metadata.py` SHA256:
  `fe9d948893742d2a326372db709e8406760689371c6c7753e68c7b7f86d76977`.
- `host_preflight.py` SHA256:
  `90cdcdfafc2d220c3fcb19ad0ba0f5de26cf9f1e7739994747874359d0dee4b8`.
- Both prior correctness directories match their retained manifests. Initial
  failed probes/reports and the intermediate GO remain untouched. All three
  source snapshots still match their original inventories.
- Evidence: `audit-before.json`, `audit-after.json`, `transport-diff.patch`.

## Protocol and ownership review

Reviewed `engine_metadata.py:18-127` and the integration in
`host_preflight.py:19-23,31-52,74-86,141-225`.

- Only the literal operations `info` and `version` can reach the transport. The
  request is a fixed GET with fixed headers, through AF_UNIX/SOCK_STREAM at
  `/var/run/docker.sock`. No caller-selected URL, method, path, header, proxy,
  redirect, authentication input or DNS lookup is introduced.
- No CLI or shared-command call is on this metadata path. There is no child
  process tree for preflight to reap. A socket context manager owns each opened
  connection across connect/send/read/protocol failures and successful completion.
- Framing is deliberately a restricted HTTP subset: status 200, JSON content
  type, identity encoding, and either a single Content-Length, simple chunked
  encoding without extensions/trailers, or EOF-delimited body. Conflicting or
  duplicate headers, bad framing, malformed chunks, truncation and additional
  data after a framed response reject. A complete body does not bypass required
  EOF or deadline checks.
- Header bytes/count, body bytes, chunk count and total wire bytes are bounded.
  Tests exercised 8 KiB header and 256 KiB body boundaries through the transport,
  the exact 64-header boundary, and wire-counter enforcement directly near its
  limit. Wire accounting is defense in depth in addition to the framing limits.
- The remaining shared deadline is used before connect, sendall and every recv;
  late receive/EOF and final body completion are checked. The three metadata
  requests do not each receive a fresh budget.

## Metadata integration and authority

- API wire mappings use `CpuCfsPeriod`/`CpuCfsQuota`, rather than the old CLI
  template capitalization. Verified against the public API 1.56 schema; see
  `api-schema-notes.md`.
- The API body is bounded at 256 KiB and parsed as UTF-8 JSON with duplicate-key,
  float/constant and invalid-shape rejection. Only the selected fields survive
  projection, which retains the separate 16 KiB selected-metadata bound and the
  existing typed checks. The final report remains bounded at 8 KiB.
- Private/unselected API configuration can enter the bounded in-memory body but
  is not exported or included in a raw-body hash. Independent controls changed
  unselected proxy/configuration fields between the two info samples and proved
  the exported report stayed byte-identical. Selected-field drift still blocks.
- A CLI installation is no longer required for this metadata-only probe. This
  does not establish prerequisites or approval for the separate runtime adapter.
- All candidate/refusal controls preserve `host_admitted=false`,
  `execution_authorized=false`, `runtime_evidence=false`, `native_trials_run=0`,
  all pending obligations, and simulated labels for injected probes.
- COR-02's exact NNP token remains accepted through the API path. Duplicate NNP,
  unsupported hyphenated/profile variants and rootless/userns controls still
  reject. The previous subprocess-specific COR-01 timeout path has been removed;
  socket errors and expiry preserve structured refusal without starting children.

## Verification evidence

### Independently executed

1. **30/30 focused tests PASS** (`focused-tests.log`): 16 preflight and 14 new
   transport tests, including the supplied real socketpair controls.
2. **15/15 independent controls PASS** (`independent-tests.log`,
   `independent-evidence.json`), covering:
   - every two-part split of representative length/chunked/EOF responses;
   - every truncation of representative length/chunked responses;
   - duplicate/conflicting headers, pipelined extra responses and trailer refusal;
   - header count, wire accounting, whole API body and selected-field bounds;
   - expiry at connect/send/recv/EOF and no allocation on an expired deadline;
   - cumulative deadline across requests, on both architecture fixtures;
   - both architecture fixtures and all three supported framings through preflight
     and CLI JSON/exit behavior;
   - UTF-8/JSON/schema/CPU wire-name validation, privacy projection and every
     selected info-field drift;
   - exact NNP behavior and removal of the CLI executable requirement;
   - complete CLI flows over **owned real socketpairs** on both architectures:
     success, truncated chunk terminator and a complete response with withheld EOF.

For the real-socket CLI cases, success used three fixed requests and returned
candidate/exit 0. Truncation and late EOF returned blocked/exit 1 with empty stderr.
The late-EOF cases completed in approximately **0.201/0.203 seconds** against a
0.2-second shared deadline. Owned client descriptors were closed before result
checks; all peer threads and remaining test-owned descriptors were cleaned up.
No filesystem socket was connected: the adapter only recorded the fixed address
and exchanged bytes with a previously created socketpair.

### Supplied full-suite result validated, not independently rerun

`transport-tests-022.json` and `.log` report **318/318 PASS**. Verified all of:

- all **304 prior test IDs retained**, with exactly 14 new `test_engine_metadata`
  IDs; no failed, errored, skipped, expected-failure or unexpected-success entries;
- inventory, started/passed lists and all 318 individual log results reconcile;
- all **59 package source hashes** and the test-runner hash match this snapshot.

Bindings:

- JSON SHA256: `2113de12e8e359b884e9d1ac7e884866663d1bcced51f1680aa93c63f5dee6bf`.
- Log SHA256: `7bd62e246d4a7c1fdb793e11ecf9af761fc0632eafd441d8715c2cdfc450fe65`.
- Runner SHA256: `9afb715125f734f5b905da63dde5c9787fc5997b2e5a73a0bf62d781b23aef84`.

Retained test IDs are not being equated with unchanged test implementations: the
historical child-timeout test now explicitly tests the replacement no-child/socket
semantics. The original actual-child regressions and their results remain frozen
in the prior artifacts; they were not rewritten or represented as socket tests.

## Security-review relationship and unchanged gates

The replacement removes the preflight route to the CLI configuration/plugin
behavior and shared-runner descendant lifecycle described by S1/S3. Socket
failure handling also replaces the S2/COR-01 subprocess timeout surface.
This review does **not** claim the unchanged shared runner is fixed, nor certify
the runtime adapter's CLI environment/lifecycle behavior. Those concerns remain
explicit execution-assembly gates in the checkpoint and require their own review
before runtime use. Independent security review remains separate.

The documentation correctly preserves the initial failed reviews, intermediate
GO and subsequent replacement chronology. It does not promote the historical
SSH discovery or the replacement's offline tests to native acceptance. Eligible
replacement x64/ARM64 host details remain pending, as do daemon/kernel/namespace
pairing, collector and image/actor admission, closed trial orchestration, timing,
whole-trial confinement, failure/cleanup evidence, predecessor gates and final
execution-packet approval.

No live Docker, SSH, ptrace, or native C0 actors were used. No repository or
snapshot was mutated. ARM64 results are simulated architecture controls, not
native ARM64 execution. Continue implementation under the unchanged gates;
this verdict authorizes no native calibration.

## Re-run commands

From `/tmp/opencode/c0-native-preflight-2026-10-17/transport-correctness`:

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -B audit.py --phase before
PYTHONPATH=/tmp/opencode/c0-native-preflight-2026-10-17/transport-snapshot/tests/crawl-jobs-v2-observer \
  PYTHONDONTWRITEBYTECODE=1 python3 -B -m unittest -v test_host_preflight test_engine_metadata
PYTHONDONTWRITEBYTECODE=1 python3 -B independent_controls.py
PYTHONDONTWRITEBYTECODE=1 python3 -B audit.py --phase after
```
