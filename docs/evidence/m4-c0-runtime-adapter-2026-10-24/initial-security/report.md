# C0 runtime adapter — independent read-only security review

Date: 2026-10-24. Reviewed the corrected frozen snapshot, not the parallel
correctness review. Product source and frozen evidence were not changed.

## Verdict

**NEEDS_REMEDIATION** — one **Low-severity**, narrowly scoped argument-guard
defect: clustered short help flags bypass the new help restriction. Fix that
guard before treating this hardening increment as complete. No broader redesign
is requested, and no currently reachable authorization bypass was demonstrated.

Independent results: **14/15 security probes passed; one regression probe failed
on the actual guard defect; zero probe errors.** The review-only proposed fix
passes its controls, including **398 simulated backend calls** across the two
architecture contracts. No product fix was applied.

The local process-group hardening and public-boundary error redaction passed the
independent probes. This verdict does not authorize Docker operations, native
actors, calibration, a full runner, host changes, or a replacement VM.

## Frozen-input integrity

- Before **and** after SHA-256 of `review-inputs-corrected.json`:
  `ef5134712918c3b705b6076f6367ff044a669237a7d47632cc677093c3814ccf`.
- All **329** packet-bound files match their recorded sizes/hashes at both checks.
  All **63** snapshot source files match the corrected source inventory; current
  source also matches that snapshot before/after. See `integrity-before.json`
  and `integrity-after.json`.
- `source-inventory-corrected.json` is authoritative: exactly **five** changed/new
  paths (`docker_backend.py`, `streams.py`, `local_process.py`,
  `test_live_adapter.py`, `test_owned_resources.py`). The original patch's seven
  native-file additions are a shallow-baseline comparison error, not seven code
  changes. All seven match the prior 318-test source hashes and corrected
  inventory. No Git commands were executed to independently re-read HEAD.
- All **247** before-snapshot copies match `before.json`; the **199** preexisting
  candidate paths remain represented in that preserved record. All **190**
  current `docs/evidence/` files enumerated there match their before hashes.
  Chronology documents outside the packet were not used as frozen input.
- The supplied 022/077 test reports each record **335 passed**, retaining all
  **318** historical IDs plus **17** new IDs; all 61 reported source hashes match
  the corrected inventory. These reports were verified, **not rerun**. The prior
  report's independent hash is recorded in `supplementary-probes.json`.
- Initial evidence remains intact: `targeted-01.log` records **1 failure / 17**
  caused by expecting `arm64` from the default x64 fixture (`amd64`). The original
  test, correction record and `targeted-02.log` showing 17 passing tests were
  reviewed and preserved. This is separate from the newly found guard defect.

## Findings and remediation

### SEC-01 — Low: repeated/clustered short help escapes the argument restriction

**Location:** frozen `docker_backend.py:39–53`, especially the exact-token check
at lines 44–45; dispatch to the CLI transport at lines 104–107.
**Classification:** CWE-184 (incomplete denylist), argument-boundary hardening gap.

**Proof:** `security_probes.py::test_02_regression_clustered_help_must_be_rejected`
passes the following argv to the unchanged `command()` and observes it reaching
the transport stub rather than rejecting:

```python
[*db.PREFIX, "container", "inspect", "-hh"]
```

`-hhh`, `-hh=true`, and `-sh` also pass the adapter guard. Ordinary `--help=true`,
completion commands, and explicit configuration overrides are rejected before
binary/configuration/process side effects (positive rejection controls).

This is not merely an unknown spelling that the documented CLI necessarily
rejects. The independently inspected **Docker CLI v29.0.0** source establishes
the `-hh` path:

1. pflag parses it as repeated boolean `-h` shorthand flags.
2. Docker registers persistent `help`/`h`; Cobra invokes `HelpFunc` when it is true.
3. Docker's help function calls `AddPluginCommandStubs` → `ListPlugins`.
4. Discovery includes system plugin directories despite an empty private config;
   candidate metadata uses `exec.Command(...).Output()`.

Exact upstream links, symbols and relevant excerpts are in `upstream-notes.md`.
Neither disabling hooks nor isolating HOME prevents this **help-discovery** path.
No Docker binary/plugin was executed here; the proof combines an executed
adapter-guard bypass with a version-specific static upstream call path.

**Impact and prerequisites:** a caller able to supply this raw argv can leave the
intended closed-builtin path and trigger installed plugin metadata execution under
the CLI user's privileges. Malicious side effects additionally require a suitable
plugin on that host. The present typed `DockerBackend` methods do **not** accept
arbitrary argv; their selectors/specifications reject tested help injection and
their mutations retain authorizer checks. **No untrusted-data path through those
methods was found**, and local `/usr/bin/docker` is absent. Accordingly this is
not reported as current remote code execution, privilege escalation, an endpoint
override, or a production CVSS-scored exploit. It is a concrete defect in the new
explicit help prohibition and its claimed closed-operation contract.

**Small remediation, not applied:** retain the existing operation/prefix/long-flag
checks and reject all unneeded single-dash tokens. Preserve the existing oracle's
fixed positional `-I -B /oracle/worker.py` tail after its image ID. Insert this
inside `cli_arguments`, after `args = argv[3:]` and the existing forbidden check:

```python
flag_args = args
if (not actor and args[:2] == ["container", "create"] and len(args) >= 6
        and args[-3:] == ["-I", "-B", "/oracle/worker.py"]):
    require(args[-4].startswith("sha256:"), "DOCKER_ARGV")
    digest(args[-4][7:])
    flag_args = args[:-3]
require(not any(item.startswith("-") and not item.startswith("--")
                for item in flag_args), "DOCKER_ARGV")
```

The equivalent review-only `corrected_guard` rejects eight negative forms,
retains the five focused safe forms, and accepts every emitted lifecycle/actor
argv in the 398-call synthetic integration control, including oracle creation.
Add the clustered-help regression to the product tests, rerun the existing
offline gates, and produce a **new** frozen packet without rewriting this one.

## Independent checks

Executed with Python **3.14.8**, `-I -B -W error::ResourceWarning`:

| Boundary / control | Evidence and result |
|---|---|
| CLI grammar | 13 invalid long/completion/actor/size/NUL forms reject before side effects; clustered-help regression fails as SEC-01. |
| Metadata → selected output | Four fragmented fake AF_UNIX exchanges use only the fixed socket and exact `/info`/`/version` GETs; x64 passes, ARM server mismatch rejects; unselected credential canary does not escape. No real connection. |
| Malformed wire / errors | Eight malformed JSON/framing/status/extra-data cases and three transport exception classes yield value-free `DOCKER_TRANSPORT`; fake sockets close. |
| Typed selectors / authorization | Nine injected selectors and two unauthorized mutation attempts reject before the runner. No external injection path for SEC-01 established. |
| Configuration lifetime | Two concurrent inert children get distinct empty 0700 configs; closing one does not remove the other's. Ambient loader/proxy/context/telemetry variables are excluded; spawn failure removes config. Explicit `/tmp` request asserted; actual test directories routed under this review. |
| Binary admission | Ten synthetic ELF/type/mode/owner/ancestor cases exercise fail-closed gates without opening or executing a Docker binary. This is not version/hash admission. |
| PID reservation | 32 `WNOWAIT` observations preserve child identity and exit status; group SIGKILL precedes `wait`; an already-reaped leader is not signaled. |
| Closed-pipe descendants | Both command and stream paths terminate same-group descendants after leader exit. Deliberately disabling signaling leaves the control descendant alive, so the detector distinguishes the broken implementation. All fixture descendants are then independently killed/reaped. |
| Cleanup failures | Injected group-signaling denial, wait timeout, lost ownership and selector failure reject; stream failure stays sticky; tested pipes/selectors/configurations close. Public boundary suppresses synthetic private error text. |
| Output and FD limits | Exactly 262144 combined bytes succeed, one extra byte rejects; repeated command cycles leave FD count at 6 → 6. |
| Guard compatibility / factory redaction | 398 purely simulated calls across x64 and ARM contract fixtures pass the proposed guard; both `open_actor` factory failures yield `DOCKER_TRANSPORT` without a canary or registered actor stream. |

Artifacts: `security_probes.py`, `security-probes.json`, `security-probes.log`,
`supplementary_probes.py`, `supplementary-probes.json`. The failed regression log
is retained; it was not turned into a success by changing the frozen source.

**Limit control, not a finding:** an inert descendant using `setpgid(0, 0)` stays
in the leader's session but leaves its process group and survives the adapter's
group signal. It was then killed via its pidfd and reaped by the probe. Both
`remote_exit_proven` and `local_process_group_absence_proven` remain false. This
supports the stated **process-group** scope, not a promise to terminate every
same-session descendant. A process-local subreaper was used only in the probe to
adopt/reap these deliberately orphaned fixtures; no host policy was changed.

## Host eligibility and claims

The frozen `host-assessment.json` correctly keeps the selected x64 host **not
admitted**: recorded kernel `6.8.0-147-generic`, Yama 1, collector anchor/code
missing. It records a KVM guest with 2 CPUs/~8 GiB and no exposed vmx/svm, `/dev/kvm`
or VM tools. This supports **no native nested VM in the present configuration**,
not a claim about every future provider configuration or software emulation.

The owner's sibling/dedicated-VM direction is still awaiting provider/hypervisor
management details. No replacement VM has been created. These records are not a
fresh complete preflight or daemon/kernel/namespace pairing; the old daemon
version remains historical. This review did not reconnect to the host, alter
Yama, install packages, provision a collector, or run native trials. ARM64 native
work remains deferred; the ARM fixture above is only an offline contract check.

## Scope and limits

- Read-only source/evidence review plus inert local Python and fake-wire probes.
  All new files and probe temporary directories are under `security-review/`.
  No Git command, Docker/native actor/calibration, SSH/remote-host operation,
  other agent, product-source edit, or frozen-input edit occurred.
- HTTPS access was used only for public upstream documentation/source; no
  project code or host data was submitted to those documentation queries.
- Review covered the five-path increment and relevant unchanged metadata,
  ownership and dispatch code. It is not a repository-wide audit or a fresh
  re-certification of all historical controls.
- Installed CLI authenticity/version admission, native ABI/runtime behavior,
  full-runner safety, daemon/collector pairing, detached groups/sessions,
  remote-container shutdown and independent cleanup evidence remain separate
  obligations. The code's root-owned ELF check is not a pinned binary approval.
- An outside reaper, concurrent caller manipulating the same Popen child, or
  administrator replacement of trusted paths is not covered by the documented
  exclusive-child-ownership/trusted-administrator assumptions. No generic
  redesign finding is raised for those assumptions.
- `result.json` carries machine-readable findings and hash bindings;
  `review-artifacts.json` binds the final review files (excluding itself).
