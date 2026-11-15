# C0-ARCH-05 preflight remediation — final correctness re-review

**Scoped verdict: GO_FOR_CONTINUED_IMPLEMENTATION**

**COR-01: CLOSED. COR-02: CLOSED.** No remaining correctness finding identified
in this remediation scope. The fixes are narrow, preserve the original negative
controls, and do not change the shared observation/transport/admission package.

This verdict covers only the first increment's read-only host discovery/preflight
and these two fixes. It is **not** native host admission, execution approval,
calibration acceptance, completion of C0-ARCH-05, or accepted-image publication.

## Frozen source and preservation

- Reviewed root: `/tmp/opencode/c0-native-preflight-2026-10-17/remediation-snapshot`.
- Inventory: `remediation-source-inventory.json`, SHA256
  `4046bd7f3604bdf51ddd50e159a41c394ec9c55e4715cbbfef69e54d2e743ba6`.
- Base: `babb2180d05698e64b7d9baf37f9cab14b3c0f7f`.
- **60/60 files verified before and after**, with no extra snapshot files.
- Exactly three files differ from the initial snapshot: `host_preflight.py`,
  `test_host_preflight.py`, and the checkpoint document. All other 57 are identical;
  the original 55 context files are still byte-identical to the base.
- Production fix source SHA256:
  `330dd22cb769d25e67b60ee0f55fddd5cd8275ce20bda20aa71bd80cc5da2131`.
- Focused test source SHA256:
  `7fe50abc064d3a2951560204ce964caa25bf730c9ba4895449ca389ae63402eb`.
- The initial `correctness/` tree, including failed regressions, original report,
  original result and initial harness-failure log, is unchanged. Its 13 files
  match the repository's `docs/evidence/m4-c0-native-preflight-2026-10-17/initial-correctness/`
  byte-for-byte. Original report SHA256 remains
  `a60fcf36b52b382bc5b896884c2e370370a989c3add04796682a7762e60ed8d2`.
- Evidence: `audit-before.json`, `audit-after.json`, `snapshot-diff.patch`.

## COR-01 — CLOSED: structured refusal on subprocess wait timeout

**Fixed location:** `tests/crawl-jobs-v2-observer/host_preflight.py:89-94`.

The new transport boundary catches `subprocess.TimeoutExpired` and raises
value-free `Invalid("PREFLIGHT_DOCKER_TIMEOUT") from None`. The existing runner's
kill/reap behavior remains unchanged, and `collect()` converts that failure to
`DOCKER_UNAVAILABLE` rather than allowing a traceback to replace the JSON report.

Verified evidence:

- Independently reran the new focused CLI test on **both architecture fixtures**,
  for real benign sleeping Python children with **open and closed output pipes**.
  All four cases returned exit 1 and `BLOCKED` JSON; the runner reaped each child
  with return code -9 and closed stdout/stderr. No Docker executable was invoked.
- Reran the **original unmodified regression assertions** against the new source.
  Both formerly failing closed-pipe cases now pass, returning structured refusal
  in approximately 0.502 s for a 0.5 s deadline. The shared runner performed the
  cleanup; the retained harness's fallback cleanup was not used.
- Additional independent controls injected a timeout at each of the three
  metadata reads (`info`, `version`, final `info`) on both architectures. All six
  CLI cases returned `BLOCKED`, exit 1, empty stderr and no private canary. Direct
  boundary checks also confirmed the stable error and suppressed exception chain.
- Timeout does not become a candidate or drop the pending execution obligations.

Evidence: `focused-tests.log`, `retained-probes.log`,
`retained-probe-evidence.json`, `fix-boundaries.log`,
`fix-boundary-evidence.json`.

## COR-02 — CLOSED: supported NNP metadata without broad grammar relaxation

**Fixed location:** `tests/crawl-jobs-v2-observer/host_preflight.py:140-144`.

The regex adds exactly the alternative `no-new-privileges`. It does not generally
permit arbitrary hyphenated names or profile/value suffixes on this token. The
existing duplicate-name, type and list-size checks remain intact.

Verified evidence on **both architecture fixtures**:

- The original formerly failing positive NNP regression now returns
  `HOST_CANDIDATE`, with empty blockers and all admission/authorization flags false.
- Independent formatted-JSON transport controls confirm the complete
  `info/version/info` sequence, valid option hashing and the same candidate result.
- Duplicate NNP tokens, unsupported hyphenated names, case/spelling changes,
  added profile/value suffixes, newline/NUL variants, and a 17-entry options list
  still reject. The existing 16-entry boundary remains accepted.
- Rootless/userns blockers, missing seccomp/cgroup namespace metadata and missing
  resource support still block even when NNP is present.
- No Docker setting was disabled, no profile changed, and no native equivalence
  claim was added to obtain this result.

Evidence: the focused test
`test_documented_no_new_privileges_metadata_preserves_other_checks`, the retained
original regression, and `fix-boundary-evidence.json`.

## Exact test verification

| Evidence | Verification performed | Result |
|---|---|---|
| `focused-tests.log` | Independently executed new snapshot's focused module | **16/16 PASS** |
| `retained-probes.log` | Reran initial 15 controls plus both original regression methods | **17/17 PASS**, including all four formerly failing architecture subtests |
| `fix-boundaries.log` | Additional transport/error/NNP boundary controls | **4/4 PASS** |
| Supplied `remediation-tests-022.json` and `.log` | Validated source/runner hashes, inventory, results and individual log IDs; **not independently rerun** | **304/304 reported PASS**, all original **302 IDs retained**, exactly two new IDs |

The retained tests are loaded read-only; only the source-root selection string
is replaced **in memory** to point at the remediation snapshot. Their assertion
logic is unchanged, and their original main-block evidence writer is not run.
New results are written only under `correctness-final/`.

Full-suite artifact bindings verified:

- JSON SHA256: `2843f2928627665fa059fb67be53536008435ed6a6b6259029a47274ba774683`.
- Log SHA256: `da20037bc56b4602fa7e7af73cc3835a24c0aa18b00509831ae1d5d4410b4e26`.
- Runner SHA256: `9afb715125f734f5b905da63dde5c9787fc5997b2e5a73a0bf62d781b23aef84`.
- All **57 package source hashes** match this remediation snapshot.
- Inventory, started IDs, passed IDs and all 304 individual `... ok` log entries
  reconcile; zero failures, errors, skips, expected failures or unexpected successes.

Audit harness note: `audit-before.log` retains an initial audit-script assertion
caused by assuming unittest's parenthesized log name omitted the method. The
parser was corrected to handle the supplied full-ID format. The authoritative
successful audits are `audit-before-final.log` and `audit-after.log`; this was
not a source finding, and no source/test/evidence input was changed.

## Safety and remaining gates

No Docker operations/resources, SSH, ptrace, or native C0 actors were run.
Only offline fixtures and owned benign Python children were executed. No
repository or snapshot was modified; bytecode generation was disabled. All ARM64
results here are simulated architecture controls, not native ARM64 execution.

The checkpoint preserves the initial NEEDS_REMEDIATION chronology, distinguishes
remote discovery commands from execution of the new CLI, and keeps the owner
decision to provide eligible replacement Docker hosts separate from admission.
Replacement x64/ARM64 profile/address details remain pending. The checkpoint's
full-suite/final-verdict pending statement reflects its frozen pre-review state;
the subsequent verification is recorded here without rewriting that history.

Daemon/kernel/namespace pairing, trusted collector provisioning and pinned-SSH
admission, exact image/actor admission, closed trial dispatch, whole-trial
confinement, timing/failure/cleanup evidence, predecessor gates, and independent
execution-packet review remain required. Continue implementation under those
unchanged gates; this review does not authorize native calibration.

## Re-run commands

From `/tmp/opencode/c0-native-preflight-2026-10-17/correctness-final`:

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -B audit_snapshot.py --phase before
PYTHONDONTWRITEBYTECODE=1 python3 -B -m unittest discover \
  -s /tmp/opencode/c0-native-preflight-2026-10-17/remediation-snapshot/tests/crawl-jobs-v2-observer \
  -p test_host_preflight.py -v
PYTHONDONTWRITEBYTECODE=1 python3 -B rerun_initial_probes.py
PYTHONDONTWRITEBYTECODE=1 python3 -B test_fix_boundaries.py
PYTHONDONTWRITEBYTECODE=1 python3 -B audit_snapshot.py --phase after
```
