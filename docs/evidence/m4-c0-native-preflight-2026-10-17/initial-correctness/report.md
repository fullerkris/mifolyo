# C0-ARCH-05 first-increment independent correctness review

**Scoped disposition: NEEDS_REMEDIATION**

Scope: read-only native-host discovery/preflight only. Two reproducible P2
correctness issues need bounded remediation. Neither issue grants admission or
execution authority. No native runtime acceptance is requested by this review.

## Snapshot and review boundaries

- Frozen root: `/tmp/opencode/c0-native-preflight-2026-10-17/snapshot`.
- Inventory SHA256:
  `aa84e8b932141d6032a70e0f85a3fbc638dabec72992c6cb46a14c3cacc67414`.
- Base: `babb2180d05698e64b7d9baf37f9cab14b3c0f7f`.
- All 60 inventoried files matched. All 55 context files outside the five scoped
  files are byte-identical to the base. The README differs only by its added
  Native host preflight section. See `inventory-before.json` and
  `inventory-after.json`.
- Reviewed the three new Python files, README section, checkpoint document, and
  the imported observation/validation/transport definitions relevant to them.
- Ran fixture tests and owned benign Python children only. No Docker operations,
  Docker resources, SSH, ptrace, native actors, host-policy changes, repository
  writes, or snapshot writes. Python bytecode generation was disabled.
- ARM64 test outcomes are **simulated**, not native ARM64 execution. The stated
  `moogle-main` observations were reviewed as supplied discovery context and were
  not independently re-observed over SSH.
- The separately running full source-bound suite is not claimed as reviewed or
  rerun here. Test interpreter: CPython 3.14.8 on Linux.

## Findings

### 🔴 COR-01 — Normalize subprocess wait timeouts into structured refusal (P2)

**Location:** `tests/crawl-jobs-v2-observer/host_preflight.py:85-91,153-164`.
Relevant unchanged dependency: `docker_backend.py:52-62`.

`HostProbe.docker()` calls the shared command runner directly. If the subprocess
closes stdout and stderr but remains alive, the runner reaches
`process.wait(timeout=deadline.remaining())`. Expiry there raises
`subprocess.TimeoutExpired`. The runner correctly kills/reaps its owned process,
then re-raises. That exception is not an `OSError`, `ValueError`, `TypeError`, or
`RecursionError`, so `collect()` does not turn it into `DOCKER_UNAVAILABLE`.

**Observed:** With all other observations supplied by the normal candidate
fixture, replacing only the command executable with a benign Python child that
closes fd 1/2 and sleeps produces an uncaught `TimeoutExpired` on both architecture
fixtures. There is no report returned for `main()` to print. The CLI contract
promises a structured `BLOCKED` report on refusal; it instead terminates with a
traceback on this transport/deadline path. This is not a process leak: the actual
runner reaped both test children with return code -9 and closed their pipes,
without invoking the test harness's fallback cleanup.

**Control:** An otherwise identical sleeper that keeps its pipes open reaches
the runner's `Deadline` check and correctly returns structured `BLOCKED`.

**Suggested minimal remediation:** Catch `subprocess.TimeoutExpired` at the new
preflight transport boundary and translate it to a stable value-free `Invalid`
failure (or explicitly handle it in `collect`). Keep the existing runner's
cleanup and leave the context package unchanged. Add regression coverage for
both open-pipe and closed-pipe timeout paths, including JSON/exit-status behavior.

**Reproduction:** `regressions.py`,
`test_closed_output_pipes_timeout_returns_structured_blocker`.
Evidence: `regression-evidence.json`, first two rows; `regressions.log`.
Both runs complete in approximately 0.502 s with a 0.5 s test deadline.

### 🟡 COR-02 — Accept the supported `no-new-privileges` metadata token (P2)

**Location:** `tests/crawl-jobs-v2-observer/host_preflight.py:134-136`.

The `SecurityOptions` regex restricts option names to `[a-z]+`, which excludes
Docker's legitimate `name=no-new-privileges` option. Docker documents that this
token is included when the option is enabled globally; this is an unversioned
change documented in the API 1.43 history and present in the API 1.56 schema.
The exact emitted string is also confirmed by the public Moby implementation.
References/excerpts are retained in `upstream-security-options.md`.

**Observed:** Adding only `name=no-new-privileges` to an otherwise passing fixture
changes `HOST_CANDIDATE` to `BLOCKED`, with `['DOCKER_UNAVAILABLE']` and a null
Docker observation, on both x86_64 and aarch64. No confinement/resource/architecture
property was removed. This global default does not conflict with the C0 adapter:
the unchanged `docker_backend.py:233` explicitly supplies
`--security-opt no-new-privileges:true`, and runtime admission requires NNP=1.

**Why it matters:** A supported, stricter Docker default is misclassified as
malformed/unavailable metadata, unnecessarily excluding a proposed host. This
is a discovery compatibility error, not a reason to disable the daemon setting
or weaken C0's execution contract.

**Suggested minimal remediation:** Explicitly recognize this supported option
name (or use a tightly bounded valid-name grammar that includes hyphens), while
retaining duplicate-name checks, raw byte bounds, rootless/userns blockers, and
the existing candidate-versus-admission distinction. Add this positive metadata
fixture for both architectures. No Docker or runtime test is needed to fix it.

**Reproduction:** `regressions.py`,
`test_supported_no_new_privileges_metadata_is_not_malformed`.
Evidence: `regression-evidence.json`, last two rows; `regressions.log`.

## Passing controls and positive observations

- **14/14 supplied focused tests passed** (`focused-tests.log`).
- **15/15 independent control tests passed** (`controls-final.log`):
  - both native architecture names and their valid Docker info aliases;
  - both architecture fixtures through the actual formatted-JSON parser bridge;
  - exact 16,384-byte parser boundary, duplicate/escaped-duplicate keys, rejected
    floats/constants, trailing data, and nested wrong-type data;
  - Docker metadata wrong-type matrix and kernel integer bounds/boolean rejection;
  - drift of every selected daemon field and every checked kernel-context field;
  - detached nested snapshots and already-expired deadline guards;
  - fixed collector `lstat` paths and final-component symlink nonacceptance;
  - real benign-process open-pipe expiry, output-flood bounding/reaping, and stderr
    suppression;
  - CLI candidate JSON and all false admission/authorization flags.
- **2 desired-behavior regression tests fail**, each on both architecture fixtures
  (**4 failing subtests**). Those failures are precisely COR-01 and COR-02, not
  unexecuted runtime acceptance tests.
- Kernel and daemon snapshots are deliberately detached before later reads; the
  in-place mutation and per-field drift controls fail closed as intended.
- Fixed Docker argv, selected-field projection, typed validation, digest-only
  daemon/kernel diagnostics, and public collector-path-only inspection are good
  foundations. No path to resource creation or actor execution was found in the
  scoped implementation.
- `HOST_CANDIDATE` consistently retains `host_admitted=false`,
  `runtime_evidence=false`, `execution_authorized=false`, `native_trials_run=0`,
  and all pending obligations. Dependency injection is labeled simulated.

Harness note: the initial `controls.log` contains one **test-harness** failure
from incorrectly assuming that 1,200 nested arrays must trigger Python's decoder
recursion guard. Nesting is valid JSON and decoder behavior varies by interpreter.
The corrected control verifies rejection at the typed Docker validation boundary;
it passes. This was not raised as an implementation finding. The initial log is
preserved, and `controls-final.log` is the authoritative control run.

## Remaining gates are appropriately deferred

The README and checkpoint do not treat matching architecture/kernel-release
metadata as proof of native feasibility, daemon/kernel/namespace placement, or
collector trust. Public path presence stays `PRESENT_UNVERIFIED`; no anchor or
seccomp filter count is manufactured from preflight observations.

The supplied Yama scope 1 and absent collector observations correctly explain
why the unchanged capless sibling design remains blocked, without asserting a
failed native attach trial. The following remain separate, unchanged obligations:

1. Eligible native x64 and ARM64 host selection under the existing C0 contract.
2. Independent daemon/kernel/namespace pairing, collector provisioning and
   pinned-SSH admission, exact image and actor admission.
3. Closed dispatch, actual-end timing evidence, whole-trial confinement,
   failure handling, and independent cleanup/evidence review.
4. A frozen, independently reviewed execution packet and separately authorized
   finite native calibration; predecessor PR/CI/artifact/merge gates remain open
   as described in the checkpoint.

None of those runtime gates is being requested as acceptance evidence for this
first discovery increment. Address the two scoped findings and rerun the focused
and retained offline regressions before reassessing this increment.

## Re-run commands

From `/tmp/opencode/c0-native-preflight-2026-10-17/correctness`:

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -B -m unittest discover \
  -s /tmp/opencode/c0-native-preflight-2026-10-17/snapshot/tests/crawl-jobs-v2-observer \
  -p test_host_preflight.py -v
PYTHONDONTWRITEBYTECODE=1 python3 -B controls.py
PYTHONDONTWRITEBYTECODE=1 python3 -B regressions.py
```

The last command intentionally exits 1 on the reviewed snapshot. Probe sources
and evidence are retained only in the approved correctness directory.
