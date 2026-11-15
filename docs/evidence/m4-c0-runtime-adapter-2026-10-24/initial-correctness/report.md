# MiFolyo C0 runtime-adapter correctness review — 2026-10-24

## Verdict: NEEDS_REMEDIATION

The subprocess ownership changes are a substantial improvement: independent
benign-child controls preserved exit status, killed an orphaned in-group witness
before releasing the leader, closed descriptors on partial setup failures, and
kept cleanup failures sticky. Private config ownership and factory-error
normalization also behaved correctly in the exercised paths.

One **closed-dispatch hardening defect** remains: clustered short help flags pass
the new guard and reach subprocess dispatch. Pinned Docker v29.0.0 source shows
that those flags enter a help path that discovers and executes plugin metadata.
This blocks sign-off on the requested plugin-free dispatch boundary, not on the
independently passing lifecycle controls. No full runner/native readiness is
certified.

## Source and preservation binding

- Reviewed only the frozen `../source/` package and its supporting packet.
- Corrected review packet SHA256:
  `ef5134712918c3b705b6076f6367ff044a669237a7d47632cc677093c3814ccf`.
- Corrected inventory SHA256:
  `86855b7d27994088cf3dd6f0c825017ff826f8e6f33948b31cecfc389f11d7b3`.
- **Before and after: PASS** — all 329 packet-bound files, all 63 frozen source
  files, and their corresponding working source files match. See
  `integrity-before.json` and `integrity-after.json`.
- The corrected inventory's **five** changed/new paths are authoritative; the
  seven unchanged native files from the initial shallow comparison are not
  treated as new changes. Existing initial-failure, correction, and historical
  evidence files were neither rewritten nor removed.

All source paths below are relative to frozen `tests/crawl-jobs-v2-observer/`:

| Source | SHA256 |
|---|---|
| `docker_backend.py` | `8ceef501710ab69609d349d072d377a83fea0cf5f2377f05076114ef7fc0c130` |
| `streams.py` | `5892e07eaf0fba21c99c873489527a32f687c44dbcc4838a8adfb6f36d9b693d` |
| `local_process.py` | `ad40d3fe4775bc2e01768b4a3c0d16b24776a74717d70e9e4e991ad29cb0700e` |
| `test_live_adapter.py` | `cca3e563824e701e4e5b2e8548334c3aeee50c7b2d161f23a2a7fd9ea1690ea1` |
| `test_owned_resources.py` | `9c956de3a0499a881efc007ab56fbe00870bfb215c137a3272514bda9c03a31a` |

## Finding

### 🔴 R1 — Short-flag spellings bypass the help prohibition

**Location:** `docker_backend.py:39–53`, particularly `44–45`; dispatch at
`104–107`. **Severity:** medium; blocker for the stated hardening boundary.

The blacklist compares each whole token (before `=`) with `-h`, `-H`, and `-c`.
It does not account for Docker's clustered/attached short-option grammar. For
example, this returns normally instead of raising `DOCKER_ARGV`:

```python
cli_arguments([*PREFIX, "container", "inspect", "a" * 64, "-hh"])
```

`-hh`, `-Dh`, and `-hD` were all admitted. A separate end-to-end **adapter-only**
control confirmed each passed through `command()` to its Popen boundary with
the offending token intact. Popen was replaced with an inert Python child;
Docker itself was never invoked. Evidence:

- `independent-022.json` and `independent-077.json`, test 20;
- `dispatch-path-evidence.json`;
- `documentation-crosscheck.md` and the retained pinned sources in `references/`.

**Why this matters:** Docker v29.0.0 vendors pflag 1.0.10, which parses repeated
boolean short flags individually. Cobra 1.10.1 supplies `-h` for `container
inspect` and invokes the help handler when it becomes true. Docker's inherited
help handler calls `AddPluginCommandStubs`, which calls `ListPlugins` and runs
candidate metadata executables. System plugin directories remain in that search
even with an empty private config. Disabling hooks does not disable this
separate help/discovery path.

**Scope calibration:** current higher-level backend methods construct fixed
argument lists and were not found to generate these spellings. This is a defect
in the newly advertised low-level closed-dispatch guard, **not** a demonstrated
authorization bypass through `inspect()` or a demonstrated malicious plugin
execution on this machine. Installed Docker is absent here; the parser/help
consequence is supported by pinned upstream source, not an actual CLI run.

The same blacklist also admits `-Hunix:///review-unused.sock` and
`-creview-unused` (test 21). This is supplementary evidence of the same grammar
gap, **not a second proven remote-endpoint vulnerability**: v29.0.0 has separate
conflicting-host/context and multiple-host rejection. No remote rerouting is
claimed.

**Suggestion:** consider validating a closed per-operation argument grammar, or
rejecting noncanonical short-option forms in the Docker option portion before
binary inspection/config allocation/spawn. Do not indiscriminately reject every
single-dash token: the fixed oracle container command deliberately contains
`-I` and `-B` after its image. Preserve that exact allowed argument tail. Add
controls for clustered help, attached endpoint options, and the normal oracle
creation argv, then rerun the source-bound suites and these probes.

## Checks and results

| Check | Result |
|---|---|
| Corrected packet before/after | PASS, 329 files each time |
| Frozen/working source equality | PASS, 63 files |
| Historical test retention | PASS, all 318 retained, 17 new |
| Existing 022/077 reports | Both PASS/335; hashes and inventories independently cross-checked |
| New independent probes, umask 022 | 23 run: **21 pass, 2 fail**, 0 errors/skips |
| New independent probes, umask 077 | 23 run: **21 pass, 2 fail**, 0 errors/skips |
| Adapter-only dispatch reproduction | All five alternate spellings reached the intercepted Popen boundary |

The two failing tests in each independent run are the two manifestations of R1
above; neither was converted to a pass or deleted. The broad 335-test suite was
**not rerun**: its retained reports were checked against fresh discovery and all
61 frozen package hashes, plus the test-runner hash. The supplemental historical
318-test report is separately hashed in `coverage-crosscheck.json` rather than
misrepresented as part of the 329-file packet.

Both independent runs used `python3 -B -W error::ResourceWarning`. The new
controls exercised, among other cases:

- Concurrent stdout/stderr writers at the exact combined 262144-byte cap;
  nonzero exit 73 and natural SIGTERM exit -15 were preserved.
- Interrupted/would-block reads, spawn failure, second selector registration
  failure, final stdin setup failure, and deadline expiry before command spawn.
- Repeated normal close; SIGKILL failure after natural exit; injected wait
  timeout; sticky subsequent errors without a second numeric group signal.
- An externally reaped child with stale `Popen.returncode=None`: ownership was
  refused before any `killpg` call.
- A naturally SIGTERM-terminated leader with a closed-output, still-live
  descendant: the known witness died by SIGKILL and its witness pipe reached
  EOF, while the leader retained -15. A process-local subreaper setting let the
  harness reap that witness; this does not prove general descendant absence.
- Config existence through group cleanup, distinct simultaneous actor configs,
  constructor failures at validation/selector/spawn, and expiry after config
  allocation. Configs were removed and descriptors did not accumulate.
- A real benign-child factory setup failure through `open_actor()` normalized
  to exactly `DOCKER_TRANSPORT`, with no registered actor and no diagnostic
  canary leakage.
- Exact info/version interception and selected-field projection with injected
  metadata; malformed metadata failed closed. No real socket was contacted.
- `finish_transport()` continued to report both remote-exit and process-group
  absence proof as false.

To respect the write boundary, config-allocation controls verified the requested
explicit `/tmp` and `obs1-docker-` prefix, but redirected actual allocations into
this review directory. Those real directories had mode 0700 under both umasks.
This independently tests lifetime/isolation, **not real `/tmp` filesystem
behavior**. Reference source downloads were stored as data only and not run.

## Host eligibility and remaining limits

`host-assessment.json` remains a scoped observation record, not a fresh full
preflight or daemon/kernel/namespace pairing. Its recorded x64 host still has
Yama 1 and no collector. The selected host is already a KVM guest with no
exposed nested hardware-virtualization facility/tooling. A native sibling VM
remains dependent on upstream provider/hypervisor management details that have
not been supplied. No host, package, policy, VM, or collector changes occurred.

This review invoked no Docker, SSH, native actors, or calibration and spawned no
other agents. No product source, existing evidence, or Git state was edited.
Only new files under `correctness-review/` were written. The installed CLI
binary/version, real Engine interaction, actual native path, complete runner,
escaped descendants, remote container cleanup, and ARM64 native readiness are
**not admitted or proven**.

The lifecycle implementation is a good foundation. Address the one dispatch
grammar defect, preserve the fixed oracle tail, and repeat the scoped checks
before considering this hardening review resolved.
