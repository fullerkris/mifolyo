# Live adapter and x64 environment assessment — 2026-10-24

**Status:** remediated implementation passes **338/338 tests under umasks 022 and
077**; both independent final reviews are **GO_FOR_CONTINUED_IMPLEMENTATION**,
with **R1 and SEC-01 closed** and no new findings. The scoped adapter gate is
complete. Host eligibility remains blocked; the owner-selected dedicated sibling
VM awaits actual provider/project or hypervisor SSH details.

## Host decision

Fresh read-only SSH-MCP checks of `moogle-main` (`100.99.200.105:4927`, effective
user `moogleMain`) found Linux x64 / Ubuntu 24.04.5, kernel **6.8.0-147-generic**,
Yama **1**, the Docker socket present, cpu/memory/pids controllers present and
both collector paths absent. This updates the original host evidence without
overwriting it. Current daemon metadata/namespace pairing was not re-admitted.

The owner selected a dedicated VM. This server is already a **KVM guest**, with
two CPUs, about 8 GiB RAM, no `/dev/kvm`, no vmx/svm flags, no KVM modules and no
VM-management tooling. A native nested VM is unavailable in the present setup;
a sibling VM requires access at the provider/hypervisor level. The owner selected
providing these details, but no actual provider/project/profile has been supplied.
No host policy/package/VM/collector change or native C0 execution occurred.
[Assessment](host-assessment.json) retains the observations and decision boundary.

The eventual guest needs native x64, a C0-compatible Yama baseline, its own local
Docker socket and cgroup-v2 limits/confinement, independently paired kernel/daemon
namespaces, admitted collector and exact images. Neither a VM label nor metadata
alone proves hardware-breakpoint feasibility. The existing capless contract and
deferred ARM64 acceptance obligations remain in force.

## Adapter increment

- Exact daemon/platform metadata requests use the existing bounded AF_UNIX
  `GET /info` / `GET /version` reader and selected-field parsing. `docker info`
  is never executed by this path, avoiding its plugin-metadata enumeration.
- Lifecycle CLI commands use `/usr/bin/docker`, checked as a root-owned,
  nonwritable regular ELF beneath administrator-controlled directories.
- Each command/stream gets a new mode-0700 empty configuration directory,
  explicit `--config`, private HOME/DOCKER_CONFIG, fixed `/` working directory,
  and an allowlisted environment. Hooks, hints and OpenTelemetry are disabled.
  Help/completion/plugin commands and endpoint/config overrides reject.
- `waitid(WNOWAIT)` keeps the leader PID reserved. Both ordinary commands and
  actor streams signal the owned process group before reaping on normal and
  exceptional exits, including closed-pipe descendants. Cleanup is bounded,
  descriptors close and adapter errors remain value-free.
- Mutation authorizers, owned-resource checks and remote cleanup requirements
  remain active. Local group signalling is not escaped-session descendant or
  remote-container absence proof.

Tests use real benign Python children and synthetic metadata. The local machine
has no Docker executable; actual admitted-guest CLI behavior and native runtime
remain to be verified. [Documentation notes](documentation-notes.md) distinguish
the Docker v29.0.0 implementation consulted from future installed-binary admission.

## Evidence and preservation

Base: `e8a0a415ac2b370319cd57f4c5262ce616a536fb`. Five implementation/test paths
change; all **318 historical IDs** remain, with **20 new tests** after remediation. The prior
199-path candidate and 247 source/evidence files were captured before editing.
All original preflight and post-merge evidence exports remain unchanged.

- Current [remediated source inventory](remediation/source-inventory.json) and
  [338-test 022](remediation/tests-022.json) / [077](remediation/tests-077.json) reports.
- Current [re-review packet](remediation/review-inputs.json), SHA-256
  `b3e66f6bf5cc95194e04548b7cfb4df4978898d4cec1a48aaaa357a1d4be14e7`
  (444 files), including original snapshots and both failed reviews.
- Initial [corrected source inventory](source-inventory-corrected.json),
  [335-test 022](tests-022.json) / [077](tests-077.json) reports and logs.
- Initial [review packet](review-inputs-corrected.json), SHA-256
  `ef5134712918c3b705b6076f6367ff044a669237a7d47632cc677093c3814ccf`
  (329 files).
- Full durable snapshots/tooling: `out/c0-runtime-adapter-2026-10-24/`.
- [Complete five-path source delta](complete-source-delta.patch) and
  [before/final bindings](source-delta.json). The original shallow `source.patch`
  remains historical and is superseded only for delta comparison.

The first new test run had one fixture-expectation failure (expected ARM64 from
the fixture's default x64); its source/log and exact correction are preserved.
The first freeze comparison also misclassified seven unchanged native-subdirectory
files because the additional before-source inventory was shallow. Its original
packet/inventory/patch remain retained. The corrected comparison proves those
seven match HEAD, the prior tested source and the new snapshot; only the five
actual changed/new paths are the adapter delta.

## Initial findings and remediation

The [initial correctness review](initial-correctness/report.md) returned
**NEEDS_REMEDIATION**, R1: **21/23** independent tests passed under each umask.
The [initial security review](initial-security/report.md) independently returned
**NEEDS_REMEDIATION**, SEC-01: **14/15** probes passed. Both identified the same
guard gap: clustered help flags such as `-hh` or `-Dh` were allowed through to
the CLI boundary, whose pinned upstream help path can discover plugin metadata.
No typed-backend authorization bypass or actual plugin execution was demonstrated.
Their lifecycle/isolation/error-redaction controls otherwise passed.

The fix rejects every single-dash token before binary inspection, configuration
allocation or spawn, except the exact oracle positional suffix following
`--entrypoint python3` and a valid immutable image ID. Malformed/misplaced tails
reject; actual generated oracle-create arguments pass for both architectures.
All 335 original increment test IDs remain, plus three remediation test methods.
Only `docker_backend.py` and `test_live_adapter.py` changed since initial review.
Original failed reports, probes and upstream references are preserved as 40 exact
exports.

The [final correctness review](final-correctness/report.md) closes **R1**, with
**GO_FOR_CONTINUED_IMPLEMENTATION** and no new findings. All **62 test-method
executions** passed: the 23 original probes and eight new variant/compatibility
methods under each umask. Per umask, 6,132 negative guard cases rejected and
218 typed builtin plus six actor-start arguments passed. Both architectures'
oracle-create tails remained valid. All 444 packet files and 63 source/script
paths verified before/after; both original failed reviews remain unchanged.
Result SHA-256:
`b99a4ea884dfbaac244f7061de4c003e63ea1f445f88d659243cf0f374445659`.

The [final security review](final-security/report.md) independently closes
**SEC-01**, with **GO_FOR_CONTINUED_IMPLEMENTATION** and no new findings.
**13/13 checks pass under each umask**, including 8,362 negative guard invocations
and a 398-call simulated compatibility replay: **140 actual CLI/actor guard
validations plus 258 synthetic metadata calls per umask**. The original failing
regression passes on the final guard; the retained old-guard negative control
still detects the original defect. All 444 packet files and 63 source/script
bindings verify, with original reviews unchanged. Sixteen exact security outputs
are exported. Result SHA-256:
`a7fe04abb47a656173f650dcb4ded8b76b571fbf9ce3324f7e8b76bfe70569f5`.

## Completed scoped gate

[Final gate](final-gate.json), SHA-256:
`1dc21c0133daf39055ca7c43b65241c80e3bf0f5848567be92e7d8fe791f7ded`.
[Closure verification](closure-verification.json) rechecks all 444 review inputs,
63 current/frozen source/script bindings, both final and both original review
manifests, all 111 historical preflight exports, all 77 post-merge exports and
the 568-file predecessor durable delivery. The original 199-path candidate and
247-file before snapshot, initial fixture failure and comparison correction remain
preserved. The full-suite reports retain `commit: "local"` and bind the final
source; they are not merged-main or native runtime test results.

The [export map](export-map.json) binds exact source-to-export copies; the
[repository manifest](manifest.json) binds this evidence directory, excluding
itself. Final candidate and durable-retention inventories are under
`out/c0-runtime-adapter-2026-10-24/`. All changes remain local/uncommitted on
`feature/crawl-jobs-v2-c0-native-calibration`, with an empty real index.

Next: obtain the sibling VM's provider/project or hypervisor SSH profile, then
establish fresh host/installed-CLI/collector/image admission and complete the
runner's actual-end timing, confinement/resource and failure/cleanup evidence
before reviewed native x64 calibration. Native ARM64 remains deferred; eventual
native acceptance of both architectures is required for accepted dual-architecture
publication.
