# C0-ARCH-05 native host preflight — 2026-10-17

**Status:** the read-only host-preflight increment is implemented and independently
reviewed: **318 tests, two GO verdicts, all five findings closed**. On 2026-10-24
the working branch was synchronized to merged main and all 318 tests passed under
both umasks. The subsequent live-adapter increment passes **338 tests under both
umasks**; both independent final reviews return **GO_FOR_CONTINUED_IMPLEMENTATION**,
with **R1/SEC-01 closed** and no new findings. The selected x64 host remains blocked;
the owner chose a dedicated sibling VM and provider/hypervisor details are awaited.
Execution assembly remains in progress;
native ARM64 calibration is deferred by owner (2026-10-22).

Working branch: `feature/crawl-jobs-v2-c0-native-calibration`, synchronized to
owner-merged [PR #32](https://github.com/fullerkris/mifolyo/pull/32) at
`e8a0a415ac2b370319cd57f4c5262ce616a536fb`. Its actual main CI passed all 25 jobs
on attempt 1. Downloaded ARM64/x64 image artifacts and their exact source/content
bindings are independently verified: both post-merge reviewers return
GO_FOR_CONTINUED_IMPLEMENTATION, with no findings. Post-merge gate `80a18b43…`.
The original reviewed preflight evidence remains bound to
`babb2180d05698e64b7d9baf37f9cab14b3c0f7f` and is retained unchanged.

## Live-adapter hardening and dedicated-VM assessment — 2026-10-24

Fresh read-only SSH observations confirm `moogleMain` on `moogle-main`, native
x64, now kernel **6.8.0-147-generic**. Yama remains **1** and both collector paths
remain absent. The selected machine is already a **KVM guest** with two CPUs and
about 8 GiB RAM; it exposes neither `/dev/kvm` nor vmx/svm CPU flags and has no
VM-management tooling. Native nested virtualization is unavailable in its present
configuration. The owner selected a dedicated x64 VM and then providing its
provider/hypervisor details; those actual details are still awaited. A sibling VM
must be assessed upstream, with a C0-compatible baseline and its own native
admission. This discovery did not revalidate daemon metadata or namespace pairing.

The adapter now routes daemon/platform metadata through the reviewed plugin-free
Unix-socket reader. Remaining closed builtin CLI operations use a fixed native
binary, private empty config, minimal environment, fixed working directory and
disabled hooks. Commands and actor streams signal their owned process group
**before reaping on every exit**, including normal closed-pipe completion, and
bound cleanup after deadlines/setup failures. Local group cleanup remains separate
from remote-container absence proof. The test suite retains all 318 prior IDs and
adds 20 controls including three remediation tests; **338/338 pass under umasks
022 and 077**.

Both initial reviewers identified the same clustered short-option gap (R1/SEC-01).
The local fix rejects noncanonical short flags while preserving the oracle's exact
positional command tail. Both final re-reviews return **GO** on packet
`b3e66f6bf5cc95194e04548b7cfb4df4978898d4cec1a48aaaa357a1d4be14e7`;
all original failed reviews and probes remain unchanged.
Correctness passes **62/62 independent test executions**. Security passes
**13/13 checks per umask**, with a 398-call simulated replay comprising 140 actual
CLI/actor guard validations and 258 synthetic metadata calls. All 444 packet
files and 63 current/frozen source/script bindings verify.
The [final scoped adapter gate](evidence/m4-c0-runtime-adapter-2026-10-24/final-gate.json)
is `1dc21c0133daf39055ca7c43b65241c80e3bf0f5848567be92e7d8fe791f7ded`.
See the [adapter/host evidence](evidence/m4-c0-runtime-adapter-2026-10-24/README.md).
The prior 199-path candidate was preserved before edits, and earlier preflight
and post-merge evidence remains unchanged.

## Merged-main synchronization — 2026-10-24 (before adapter changes)

The exact 120-path candidate was preserved in a durable copy, backup branch and
retained stash `2475fda26223c43485b933089274476e497be3ab`. Because PR #32 was
squash-merged, the dependent branch was moved onto the verified merged baseline
before `git pull --ff-only origin main` reported **Already up to date**. Applying
the retained stash was conflict-free: 119 paths restored byte-identically; the
roadmap gained only six upstream Go-security lines. Subsequent chronology updates
are separate from this restoration proof and all historical inventories.

At synchronization, all **61 reviewed package/script paths** were unchanged.
Fresh **318/318 tests under umasks 022 and 077** retained every historical test ID
and all 59 package hashes. The later adapter delta is recorded separately above.
The preflight work remains uncommitted with an empty real index. The original
failed snapshot-export helper is retained alongside its correction; it referenced
a nonexistent auxiliary filename, while the corrected exporter verifies all 58
actual merged image-source/auxiliary paths against the reviewed inventory.

See the [post-merge evidence checkpoint](evidence/m4-c0-post-merge-2026-10-24/README.md).
Durable source/backup/test records: `out/c0-main-sync-2026-10-24/`.

## Original owner-selected host discovery — 2026-10-17

The owner selected read-only inspection of the configured `moogle-main` SSH host.
The observations below came from that host, not the local Podman workstation.

| Check | Observation | Consequence |
|---|---|---|
| Native ABI | Linux x86_64; Docker server linux/amd64 | x64 candidate only |
| Engine | Docker 29.9.1 / API 1.56; kernel 6.8.0-110-generic | Fixed local socket accessible |
| Resource metadata | cgroup v2, systemd; memory/swap/PID/CPU limits supported | Necessary metadata available |
| Security metadata | AppArmor, builtin seccomp, cgroup namespaces | Live actor confinement remains unverified |
| Yama | `ptrace_scope=1` | Blocks the unchanged capless sibling observer |
| Context collector | Anchor and code directory absent | Installation/admission pending |
| ARM64 | Host not selected | Native ARM64 admission pending |

The [Linux 6.8 Yama documentation](https://github.com/torvalds/linux/blob/v6.8/Documentation/admin-guide/LSM/Yama.rst)
explains the descendant/declared-tracer restriction in mode 1. C0's independently
launched sibling observer has no capabilities, parentage change or `PR_SET_PTRACER`
exception. This is a **preflight incompatibility**, not a failed native attach
experiment: zero C0 actors/trials were started. C0's original
[calibration contract](evidence/m4-design-gates-2026-10-05/d01/calibration-plan.md)
retains its policy/privilege constraints.

The local x64 workstation has rootless Podman, no Docker client and no local
Docker socket. Its construction environment cannot satisfy the current runtime
adapter's endpoint contract.

## Repeatable implementation

- `tests/crawl-jobs-v2-observer/host_preflight.py`: bounded, read-only observations
  for either explicit native architecture, selected local Docker metadata,
  before/after stability checks and value-free blockers.
- `tests/crawl-jobs-v2-observer/engine_metadata.py`: fixed Unix-socket HTTP GET
  reader, with bounded headers/body/chunks/time and deterministic socket closure;
  no Docker CLI/config/plugin/subprocess path.
- `scripts/preflight-crawl-jobs-v2-native-host.py`: JSON/exit-status CLI.
- `tests/crawl-jobs-v2-observer/test_host_preflight.py`: both-platform controls,
  Yama/resource/namespace/collector failures, drift, bounds, typed parsing,
  command allowlisting, deadlines and diagnostic leakage checks.
- `tests/crawl-jobs-v2-observer/test_engine_metadata.py`: protocol framing/bounds,
  real owned socket timeout/EOF controls and ambient config/plugin canaries.

The result is `BLOCKED` or `HOST_CANDIDATE`; neither is execution approval or
runtime admission. A candidate retains explicit pending obligations. Public
collector-path presence is not source/config/permission/SSH validation. Collector
keys and client credential/config files are never opened. The bounded Engine API
body is parsed privately and unselected fields, including possible proxy settings,
are discarded without export. No preflight observation populates a trusted context
anchor or filter count.

## Verification and retained evidence

The first frozen 57-file package passes **302/302 offline tests**, including all
288 earlier test IDs and 14 new preflight controls. The frozen review inventory
also binds two scripts and this report's initial version (60 files total):
`aa84e8b932141d6032a70e0f85a3fbc638dabec72992c6cb46a14c3cacc67414`.
The initial correctness review returned **NEEDS_REMEDIATION**; its original
report, two failing regression tests (four architecture subtests), 15 passing
independent controls and initial harness failure are retained unchanged.

An actual local CLI invocation returned `BLOCKED`, exit 1 and empty stderr, with
`LOCAL_DOCKER_ENDPOINT_MISSING` and `COLLECTOR_NOT_PROVISIONED`. Its native x64
kernel reports Yama scope 0, distinct from `moogle-main`'s scope 1. The remote
observations are SSH discovery commands; the new CLI has not been installed or
run on that remote host.

The [evidence directory](evidence/m4-c0-native-preflight-2026-10-17/README.md)
retains the original missing-path/SSH-tool classification errors, successful
read-only observations, source patch, tests and exact-head PR status captures.
Initial export manifest:
`764d031b544ea4f80b22c6e6145da06b866593af6240c4644299b3f6b5d88149`.
The initial snapshot/evidence also live in Git-ignored
`out/c0-native-preflight-2026-10-17/initial-aa84e8b93214/`.

### Correctness remediation

- **COR-01:** normalize `subprocess.TimeoutExpired` at the new preflight transport
  boundary after the existing command runner's kill/reap cleanup. Real benign
  children with open and closed output pipes now preserve the CLI's structured
  `BLOCKED` JSON and nonzero exit on both architecture fixtures.
- **COR-02:** recognize Docker's documented exact `name=no-new-privileges` token.
  Duplicate names, arbitrary hyphenated names and rootless/userns incompatibility
  still reject. This metadata default matches C0's existing NNP requirement.

The first correctness-remediation suite passed **16/16 focused and 304/304 full
tests**. Its independent re-review closed COR-01/02 with a scoped GO on inventory
`4046bd7f3604bdf51ddd50e159a41c394ec9c55e4715cbbfef69e54d2e743ba6`.
This intermediate GO does not certify the later replacement transport. Initial review result:
`bdbf3995200eb89eec734ce4de40c668b9193259c4c9fd55785101b73d2e9273`.

### Security remediation — plugin-free transport

The initial security review returned **NEEDS_REMEDIATION** with three findings:

- **S1:** Docker CLI startup can read ambient config/credentials and execute
  plugins even for projected `info` output.
- **S2:** the same closed-pipe timeout gap as COR-01.
- **S3:** the shared CLI runner can leave a same-group descendant running after
  normal return. The review reproduced this using an owned benign sleeper, then
  independently terminated it; no Docker plugin was executed by the reviewer.

The preflight now uses a fixed AF_UNIX HTTP reader. Only GET info/version are
possible; no CLI/plugin/config discovery, environment overrides, DNS or subprocess
exists on this path. Every socket operation uses the remaining shared deadline;
HTTP framing, headers (8 KiB / 64), body (256 KiB), wire bytes and chunks are bounded.
All transport failures are value-free and all owned sockets close. This removes
the subprocess timeout/descendant path from preflight. At that 2026-10-17
checkpoint, the runtime adapter was unchanged and its CLI environment/lifecycle
boundary remained an execution-assembly review concern; the later adapter gate
above records its separately reviewed changes.

The original failed security controls and corrected overstrict reviewer JSON
control are retained unchanged. Initial SSH CLI discovery remains historical;
its implicit client/plugin behavior was not attested. No new remote CLI discovery
is performed after this finding. The revised suite passes **318/318 full and
30/30 focused tests**, retaining all 304 prior IDs. Frozen 62-path inventory
`faf8d794802f7a3b7603bea199689014cef21d415764571725ff9e3b66414baf` binds 59 package
files, two scripts and the checkpoint at review submission. Final-source
correctness review returns **GO_FOR_CONTINUED_IMPLEMENTATION**, with no findings:
30 focused and 15 additional controls independently pass; the supplied 318-test
report/log and all 62 source paths are verified. Result:
`71aec3c6668e7c80d5a169f191af769ed0ccb93f63c57f60c16dee0eb731afac`.
Final security re-review also returns **GO_FOR_CONTINUED_IMPLEMENTATION**, closing
S1/S2/S3 for preflight with no new findings: 30 focused and 16 independent tests,
600 wire mutations and eight deep-JSON cases. Its result hash is
`cbd9ff8b61da256bf13ba03c530e13573ce65d821f20247cb464db718da2afc4`.
Actual final-code local preflight remains BLOCKED
for missing Docker socket and collector; its report hash is
`9ed3f5b278a3c1a29ddb116bea254e81b963ce17f0df71171a81aeca0f3d3907`.

### Completed scoped gate

[Final preflight gate](evidence/m4-c0-native-preflight-2026-10-17/final-gate.json):
`2e91337ea1d30d2d319c9b86019f36c7af75677e01a082d88660d37ad22fd7e8`.
All 59 package files and two auxiliary scripts match the reviewed source; the
reviewed checkpoint version is frozen separately from this updated chronology.
All 288 C0-ARCH-04 and 304 intermediate test IDs remain in the 318-test suite.
The 89 entries in historical/reviewer export manifests verify unchanged. Both
initial failed reviews and the intermediate correctness GO remain separate.

The gate applies only to preflight source/discovery. At that checkpoint the
runtime/image implementation was unchanged; those preflight verdicts did not
certify the CLI adapter.
No collector, C0 actor or native calibration trial ran. Changes remain local and
uncommitted on the dependent C0-ARCH-05 branch.

## Remaining C0-ARCH-05 work

The owner subsequently selected the existing SSH-MCP `moogle-main` server;
read-only checks verified its effective account as `moogleMain` and native x64
architecture. Native ARM64 host selection/calibration is deferred by owner on
2026-10-22, with no scheduled resumption date.

1. Obtain provider/project or hypervisor SSH details and assess a dedicated
   sibling x64 VM. Fresh 2026-10-24 observations confirm the current guest's Yama
   scope 1, absent collector and unavailable nested virtualization.
2. Admit the installed Docker binary/version and closed CLI behavior on that
   guest. The local adapter-hardening reviews are complete; 338 tests pass under
   both umasks, with R1/SEC-01 closed.
3. Independently establish daemon/kernel/namespace pairing; provision and admit
   the pinned-SSH collector and exact x64 images.
4. Complete closed trial dispatch, actual-end hold/aggregate-pause measurement,
   whole-trial confinement/resource evidence and failure/cleanup handlers.
5. Freeze and independently review the complete execution packet, then perform
   finite native x64 calibration. Resume ARM64 admission/calibration later and
   retain each platform's outcomes separately.

The host-discovery/preflight work is the first C0-ARCH-05 increment. It does not
complete the 276-trial suite or C0-ARCH-06 accepted-image publication.
