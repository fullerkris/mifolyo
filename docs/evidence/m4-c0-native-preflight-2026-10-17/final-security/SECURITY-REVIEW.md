# MiFolyo C0-ARCH-05 native-host preflight — transport security re-review

**Scoped decision: GO_FOR_CONTINUED_IMPLEMENTATION**

**S1, S2 and S3: CLOSED FOR THE PREFLIGHT PATH.** No new actionable security finding was established in this frozen increment. This is not native acceptance, host admission, permission to run calibration, or certification of the existing runtime Docker CLI adapter.

Date: 2026-10-17. Review mode: independent, read-only source review plus offline/scripted-stream and reviewer-owned socketpair controls. No additional agents were used.

## 1. Source identity and scope

- Snapshot: `/tmp/opencode/c0-native-preflight-2026-10-17/transport-snapshot`.
- Inventory: `transport-source-inventory.json`.
- Inventory SHA-256: `faf8d794802f7a3b7603bea199689014cef21d415764571725ff9e3b66414baf`.
- Inventory-declared base: `babb2180d05698e64b7d9baf37f9cab14b3c0f7f`. The review binds to the inventory/frozen bytes; Git ancestry was not independently reconstructed.
- 62 inventoried files: 59 package files, two scripts, one checkpoint. Initial verification found no mismatches, extra files, or inventoried leaf symlinks. Final verification is retained in `integrity-after.json`.
- Compared with the initial 60-file security snapshot, only `host_preflight.py`, its tests, the package README and checkpoint changed; `engine_metadata.py` and its tests were added. No files were removed. In particular, `docker_backend.py`, `linux_observation.py`, `streams.py`, contracts and runtime-admission/context files are unchanged.
- The initial security evidence manifest remains `01d349d9be23ac0f4ee0324ab6180a6f0aaf20bf96b8f75a7268c1bde1e14841`; every one of its 15 listed original evidence files was checked without modification. The repository's exported copy was not independently audited in this re-review.

Scope covers the fixed local preflight read path, input/output bounds and validation, deadlines and cleanup, preservation of candidate-only semantics, and corresponding documentation. Native orchestration/calibration, admission of daemon/collector namespace placement, installation/provisioning, and future runtime-adapter execution are outside this increment.

Unless otherwise stated, source paths below are under `transport-snapshot/tests/crawl-jobs-v2-observer/`.

## 2. Disposition of initial findings

| Finding | Original severity | Disposition | Exact basis |
|---|---|---|---|
| **S1: implicit CLI configuration/credential reads and plugin execution** | Medium | **Closed in preflight by removal of the mechanism** | `host_preflight.py:14,74–86` imports/calls `read_engine_metadata`, not `docker_backend.command`; the endpoint check no longer needs a Docker executable. `engine_metadata.py:112–127` creates AF_UNIX/SOCK_STREAM, connects only to the constant `/var/run/docker.sock`, and sends only a literal GET info/version request. It does not invoke a CLI, read client configuration, consult proxy/header/credential environment settings, perform DNS, or create subprocesses. |
| **S2: closed-pipe timeout escapes structured blocker handling** | Medium | **Closed in preflight by transport replacement and value-free socket errors** | There is no subprocess `wait()` or `TimeoutExpired` source on the read path. `engine_metadata.py:24–31,112–127` applies the remaining shared deadline and normalizes `OSError` (including timeout/reset/permission failures) to `Invalid("ENGINE_TRANSPORT")`. `host_preflight.py:148–159` records `DOCKER_UNAVAILABLE`; `main:228–234` emits bounded JSON/exit 1. EOF is required, so complete bytes from a peer that stays open do not become an early candidate. |
| **S3: successful command return leaves an owned descendant running** | Medium | **Closed in preflight by eliminating child processes** | No subprocess is started on success or failure. The socket is owned by a context manager at `engine_metadata.py:116–123`, covering success, validation failure and exceptions. Scripted close assertions, real descriptor checks, joined reviewer peer threads and repeated socketpair cycles verified the replacement lifecycle. |

**These are scoped closures, not repairs of `docker_backend.command`.** Its old environment, subprocess timeout and descendant behavior remains in the unchanged runtime adapter. The checkpoint explicitly retains that as a future execution-assembly review concern (`docs/crawl-jobs-v2-m4-c0-native-preflight-2026-10-17.md:113–120`). This approval must not be reused to authorize that runtime path.

### S1 closure evidence

Independent test 01 used literal API wire fixtures for **both architectures × three framings × three fragmentation sizes = 18 cases**. The entire metadata exchange went through the real `HostProbe.docker`, parser, semantic checks, stability comparison and CLI report function, with surrounding Linux observations simulated. Exact request bytes and the operation order `/info`, `/version`, `/info` were verified.

During these calls, Popen/fork/spawn/system calls, configuration/file opens, DNS/network convenience APIs and sensitive environment access were tripwires. Only harmless locale/color/terminal-size environment lookups by Python's `argparse`/`gettext` were permitted; no config/proxy/credential/endpoint environment access occurred. The tripwires do not claim Python startup/import machinery never reads code or locale resources—only that the reviewed metadata invocation does not consult those sensitive sources.

Independent test 02 varied unselected proxy/diagnostic fields, including a **100,000-byte private field**, between observations and between runs. Candidate reports remained identical: the raw body was neither exported nor included in a whole-response digest/stability identity. This is deliberate selected-metadata observation, not authentication of every server response field.

Source confirms that the complete, bounded API body is temporarily parsed privately before projection (`host_preflight.py:31–52`); it can contain proxy values supplied by the daemon. The claim is **no client credential/config-file reads and no unselected-field export**, not that secret-looking daemon bytes can never enter process memory.

### S2 closure evidence

- Independent test 04: **32 constructor/connect/send/receive/EOF/close fault cases** produced JSON `BLOCKED`, exit 1, empty stderr and no synthetic error-value leakage. All created scripted sockets closed.
- Test 05: late connect, send, receive and final EOF all rejected; data/EOF arriving after expiry cannot succeed.
- Test 06: an aggregate 0.5-second fake-clock budget expired during the second observation; a fresh budget was not granted for each request and the third socket was not created.
- Test 15: **seven real owned socketpair cases** exercised incomplete status, slow headers, slow body, complete response held open, truncated body, extra bytes and missing terminal chunk. All returned structured blockers without tracebacks. The four deadline-driven cases completed in approximately **0.1505–0.1515 seconds** for a 0.15-second budget; malformed/EOF cases completed in about 0.0013–0.0020 seconds. These timings are observations on this review machine, not hard real-time scheduling guarantees.

### S3 closure evidence

Every used scripted socket closed. Every real test client descriptor became `-1`; all reviewer-owned peer threads were stopped and joined. Independent test 16 performed **80 real socketpair success/failure cycles** with process FD count unchanged (**5 before, 5 after**). No C0 or other target-created child process existed to clean up. The reviewer created one separate benign Python process for JSON stress testing; that is test machinery, not a preflight subprocess.

## 3. Additional security controls reviewed

### Fixed request surface and non-mutation

`read_engine_metadata` rejects non-string or non-allowlisted operations before creating a socket (`engine_metadata.py:112–114`). The only requests are:

```http
GET /info HTTP/1.1
Host: localhost
Accept: application/json
Connection: close
```

or the same request for `/version`. No arbitrary URL/path/header/body, redirects, TCP proxy fallback, authentication header, mutating HTTP method, CLI extension, or resource-creation request is accepted. Native/Linux/collector observation paths retain their original non-mutating code. Public collector metadata remains `lstat`-only and explicitly unverified.

This describes the client's allowed operations. It does not attest a remote or substituted socket peer's implementation, nor claim that a daemon's own internal logging/counters never change when servicing a GET.

### Bounded HTTP response processing

The parser accepts a deliberately restricted single-response subset:

- HTTP/1.0 or HTTP/1.1 status 200 only; no follow-up redirect/upgrade handling.
- Status line limit 1,024 bytes; combined status/header bytes at most **8,192**; at most **64 fields**; duplicate/case-duplicate headers, invalid names, folded lines and forbidden control bytes reject.
- JSON content type required. Only absent/identity content encoding; no decompression.
- Exactly one of Content-Length, simple chunked transfer, or connection-close framing. Content-Length plus Transfer-Encoding rejects; ambiguous length syntax and unsupported transfer coding reject.
- Body limit **262,144 bytes**; aggregate wire limit **335,872 bytes**; at most **4,096 chunk-loop iterations**, including the terminating zero chunk. Chunk extensions, malformed terminators and actual trailer fields reject.
- EOF required after a length/chunked body; truncation, buffered extra bytes, subsequent response bytes and delayed/nonexistent closure reject. Close-delimited data is independently byte-bounded.
- Remaining deadline set before each blocking connect/send/receive; deadline checked after sending/receiving and before successful return. A slow peer cannot reset the entire timeout with each byte.

Independent tests 07/10/11 confirmed the framing refusals and exact byte/header/chunk boundaries. These are wire/encoded-body bounds, not a promised fixed resident-memory allocation: normal Python JSON/object overhead is additional but input-limited.

### Parsing, redaction, fail-closed observations

- Strict UTF-8 decode, duplicate-key rejection (including nested/unselected objects), forbidden floats/non-finite constants, required selected Engine fields, a 16-KiB projected-object bound, and existing type/shape validators remain in effect.
- **40 known-invalid HTTP cases**, **13 invalid JSON/encoding/deep-selection cases** and **78 malformed/missing selected-field cases** all blocked without report/error canaries.
- **600 deterministic wire mutations** (seed 20261017) caused no uncaught error or unclosed socket: 446 were rejected by the HTTP layer; 154 were accepted by that layer. Accepted mutations include legal header or body-byte changes and are **not** claimed to be semantically valid host metadata. Separate semantic tests enforce that boundary.
- An isolated bounded JSON stress probe tested selected/unselected nesting at depths **3,000, 10,000, 60,000 and 120,000**, with inputs up to **240,342 bytes**. All eight cases completed without a crash/unhandled exception. Invalid selected fields always rejected; deeper inputs rejected value-free, while safely ignored unselected values at the smaller depths could be projected away. Original logs retain the exact dispositions.
- Hash-only identifiers and checked version text remain the exported daemon view. Unsupported features, unavailable observations and drift remain blockers rather than implicit success.

### COR-02 and provenance

The exact `name=no-new-privileges` token is retained (`host_preflight.py:129–138`), without admitting arbitrary hyphenated names or duplicate names. Both-architecture controls confirmed that rootless/userns and rootless-with-profile metadata still block; malformed or duplicated NNP tokens reject. The existing required builtin-seccomp/cgroup-namespace/resource checks are not relaxed.

Architecture mismatches and selected daemon drift still block through the real socket-to-metadata path. Stable release/architecture/ID data does **not** prove same-boot kernel placement, same namespaces, authentic engine identity, or native actor feasibility. Those obligations remain explicit `pending` entries. Collector presence is not source/permission/identity/authentication validation.

Both statuses retain:

```json
{
  "host_admitted": false,
  "runtime_evidence": false,
  "execution_authorized": false,
  "native_trials_run": 0
}
```

The test reports remained `evidence_kind=simulated`. No trusted context anchor, filter count, runtime acceptance evidence or execution packet was created.

## 4. Documentation review

The README native-host section (`README.md:163–209`) now describes fixed AF_UNIX GETs, no installed CLI requirement, private-body projection, byte/framing/deadline/closure refusals, and candidate-only authority accurately for the reviewed code.

The checkpoint separates the historical initial/correctness/security results, records the replacement transport, flags the unchanged runtime CLI concern, and does not present this increment as completion of C0-ARCH-05/native calibration. It also qualifies the old SSH CLI discovery's implicit plugin behavior as unattested. Historical test/PR/host claims were not independently re-run here.

Documentation lookup used Context7 `/docker/docs`. Retrieved official historical Engine API examples (v1.20/v1.22) confirm the wire-field distinction `CpuCfsPeriod`/`CpuCfsQuota`, info's `OSType`/`Architecture`, version's `Os`/`Arch`, and Unix-socket GET use. Attempts to obtain newer raw schema/source URLs returned 404; these are recorded in `documentation-lookups.md`. No successful live compatibility validation against Docker 29.9.1/API 1.56 is claimed. Unsupported or changed server fields fail closed; current-host compatibility remains a subsequent owner-approved observation, not a prerequisite for this implementation-only security verdict.

## 5. Tests and retained evidence

Review interpreter: **Python 3.14.8** on Linux. All runs suppressed Python bytecode writes.

| Run | Result | Evidence |
|---|---|---|
| Existing focused suites (`test_host_preflight`, `test_engine_metadata`) | **30/30 PASS**, zero skipped | `baseline-original.log`, `baseline-original.exit` |
| Independent transport security suite | **16/16 PASS**, zero failures/errors/skips; includes the matrices and 600 mutations above | `transport_security_tests.py`, `independent-original.log`, `.json`, `.exit` |
| Isolated deep-JSON stress | **8/8 completed safely**, exit 0 | `deep_json_probe.py`, `deep-json-original.log`, `.json`, `.exit` |
| Source/evidence integrity | Initial and final retained checks | `integrity-before.json`, `integrity-after.json`, `evidence-inventory.json` |

The full repository/package suite is being run separately by the implementation owner and was **not** independently reproduced or certified here. The 30 focused tests and independent controls are this review's test evidence.

All original new-run artifacts were retained; no failing run was rewritten into a passing run. The initial security directory was not modified. The scripts use exclusive JSON output creation so future reruns should choose fresh artifact names instead of overwriting these originals.

Reproduction of the focused baseline:

```sh
PYTHONDONTWRITEBYTECODE=1 \
PYTHONPATH=/tmp/opencode/c0-native-preflight-2026-10-17/transport-snapshot/tests/crawl-jobs-v2-observer \
python3 -B -m unittest -v test_host_preflight test_engine_metadata
```

For the independent suite, use the retained script with a fresh report destination; it does not need or contact a Docker daemon. Its socket factory supplies only scripted streams or already-connected reviewer-owned socketpairs. The production `/var/run/docker.sock` address is asserted by the shim, never connected to.

## 6. Review limits and next boundary

- No real Docker/SSH connections, ptrace, native actors, native trials, credential-file reads, collector actions, repository/snapshot edits, or host security-policy changes were made. Core dumps were disabled only in the reviewer-owned shell/Python JSON-stress process.
- No live host was selected or admitted. `moogle-main` remains historical, blocked as described by the supplied observations; it was not queried again.
- Ordinary Linux fixed-file reads retain byte bounds and deadline checks around observations. This re-review does not certify hard interruption of arbitrary uninterruptible kernel/filesystem operations, process startup/import trust, or adversarial privileged modification of the Python/runtime/host.
- The Engine socket peer is not independently authenticated or paired to the local kernel/namespaces by these metadata reads. The pending obligations and false admission flags correctly preserve that boundary.
- Existing runtime CLI environment/lifecycle behavior and complete native execution safety still require their own review. This GO does not waive those gates or the separately running full-suite verification.

**Conclusion:** the scoped preflight transport remediation addresses S1–S3 without relaxing security policy or extending authority. Continued implementation is justified on the frozen inventory above; native acceptance/execution is not authorized.
