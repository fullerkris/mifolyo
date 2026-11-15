# MiFolyo C0-ARCH-05 native-host preflight — independent security review

**Decision: NEEDS_REMEDIATION**

Date: 2026-10-17. Scope: host discovery/preflight only. This is not a judgment on unfinished native orchestration/calibration and does not authorize execution. No Critical/High findings were established; three Medium findings prevent certifying the preflight's current read-only/bounded-transport contract.

## Reviewed evidence and authority

- Frozen source: `/tmp/opencode/c0-native-preflight-2026-10-17/snapshot`.
- Inventory: `source-inventory.json`, SHA-256 `aa84e8b932141d6032a70e0f85a3fbc638dabec72992c6cb46a14c3cacc67414`.
- Inventory-declared base: `babb2180d05698e64b7d9baf37f9cab14b3c0f7f`. Git ancestry was not independently re-established; the review is bound to the supplied inventory and frozen bytes.
- All 60 inventoried files matched at the initial integrity check, with no extra files or inventoried leaf symlinks. See `integrity-before.json` and final `integrity-after.json` for the before/after checks.
- Primary changes reviewed: `host_preflight.py`, `test_host_preflight.py`, the preflight CLI wrapper, README lines 163–198, and the native-preflight checkpoint. Relevant unchanged dependencies were traced, particularly `docker_backend.command`, `LinuxReader`, contracts, and deadlines.
- No repository/snapshot edits, live Docker/SSH calls, ptrace, native actors, collector provisioning, credential-file inspection, or host security-policy changes. Test subprocesses were only reviewer-owned benign Python programs. The reviewer terminated the deliberately exposed sleeping descendant using a pidfd; termination was confirmed. Other test leaders were reaped by the tested command primitive.
- The supplied `moogle-main` discovery is not independently reproduced here. Its Yama=1/missing-collector blockers remain valid as reported; no claim is made that it had a harmful plugin, leaked credentials, or ran C0 actors.

File references below are relative to the snapshot; Python package files are under `tests/crawl-jobs-v2-observer/` unless otherwise specified.

## Findings

### S1 — Medium: projected `docker info` is not a side-effect-free or credential-free reader

**Locations:** `host_preflight.py:19,85–91`; `docker_backend.py:27–37`; checkpoint document lines 41–53.

The absolute client path, fixed Unix socket, closed operation list, and Go-template projection constrain the visible command/output, but not Docker CLI initialization or plugin execution. `command()` removes only five endpoint/TLS environment variables. It preserves `HOME`, `DOCKER_CONFIG`, plugin-hook settings, custom headers, loader/telemetry settings, and unrelated secret environment variables. No `--config` isolation or plugin-exclusion boundary is applied.

**Proof / reachability:**

1. `security_adversarial_tests.py::test_01_ambient_config_and_secret_environment_are_not_forwarded` intercepted Popen before any execution using an entirely synthetic environment. It confirmed forwarding of `DOCKER_CONFIG`, `HOME`, `DOCKER_CLI_HOOKS`, `DOCKER_CUSTOM_HEADERS`, `LD_PRELOAD`, `OTEL_EXPORTER_OTLP_ENDPOINT`, and a synthetic secret variable. The exact key list is in `adversarial-original.json:5–25`.
2. Docker CLI v29.0.0 source confirms unconditional client-config loading in `DockerCli.Initialize`, including decoding inline registry `auths`; explicit `--host` does not prevent that load.
3. The same version's `runInfo` calls `pluginmanager.ListPlugins` **before output formatting**, including when `--format` is supplied. Discovery searches configured/user/system directories; each eligible candidate's metadata is obtained by `exec.Command(...).Output()` before validating the metadata. `--config` alone does not remove system-plugin execution.
4. Thus an installed plugin can perform arbitrary same-user side effects during the nominally read-only info command, even if its metadata is rejected or omitted from the projected output. The preflight provides no boundary against these side effects or against a plugin's private unbounded output capture. These operations occur before an eventual `BLOCKED` result can protect anything.

Public source links, line references and excerpts are retained in `public-source-evidence.md`. The source version establishes a supported, unexcluded client behavior; it is **not** an assertion about the installed client's exact version. The recorded daemon version does not attest the client.

**Impact / prerequisites:** ordinary installed extensions or an attacker able to influence the operator's CLI configuration/plugin path can cause unapproved local code execution, configuration/credential reads, resource consumption, or network/file side effects under the operator's existing authority, potentially including Docker-socket access. This is not an asserted privilege escalation or observed credential exfiltration. Reading configured inline auth requires its presence; forwarding an environment variable to trusted Docker alone is not the finding—the automatic extension execution makes the non-mutation guarantee invalid.

**Remediation:** replace this preflight's general Docker CLI invocation with a narrowly reviewed, plugin-free fixed Unix-socket metadata reader/helper. It must allow only GET info/version, ignore user client configuration and ambient credentials/proxies, bound headers/body/time, project fields before exporting diagnostics, and never create resources or fall back to another endpoint. This is a preflight transport change, not an orchestration requirement. Do not treat an empty config directory, hook toggle, or output template alone as a complete fix.

**Immediate copy-paste-safe containment** (replace `HostProbe.docker` until that reader is available):

```python
def docker(self, operation, deadline):
    require(operation in ("info", "version"), "PREFLIGHT_OPERATION")
    deadline.check()
    # Fail closed before invoking a config/plugin-capable CLI.
    raise Invalid("PREFLIGHT_DOCKER_READER_NOT_ISOLATED")
```

The existing collector converts this to a structured `DOCKER_UNAVAILABLE` blocker. This containment intentionally disables successful Docker discovery; it is not presented as the completed replacement implementation. Test the replacement with synthetic config/credential/plugin canaries and fake engine replies, asserting zero config/plugin reads or starts and no unselected values in exported output. No policy/capability relaxation is needed.

### S2 — Medium: closed-pipe timeout escapes as a traceback rather than a structured blocker

**Locations:** `host_preflight.py:88,153–164,233–239`; `docker_backend.py:52–62`.

After stdout/stderr reach EOF, `command()` calls `process.wait(timeout=deadline.remaining())`. If the client is still alive, this raises `subprocess.TimeoutExpired`. The command kills/reaps its group leader and re-raises; `collect()` does not catch `subprocess.SubprocessError`, and `main()` does not normalize it. The usual pipe-open timeout raises `Invalid`, which *is* caught, so the original deadline test misses this distinct path.

**Proof / impact:**

- `test_02_closed_pipe_timeout_returns_structured_blocker` substitutes a benign Python process that closes descriptors 1/2 and sleeps. With a 0.3-second deadline, it exits the preflight via `TimeoutExpired` at `docker_backend.py:52` instead of returning `BLOCKED`. The full original stack is retained in `adversarial-original.log`; the child was killed/reaped with return code -9 in 0.3018 seconds (`adversarial-original.json:27–35`).
- `followup_tests.py::test_main_timeout_escapes_without_a_json_report` confirms the CLI entry function propagates the exception and produces **zero stdout bytes**, not a JSON report (`followup-original.json`). At an uncaught script boundary Python prints the traceback/command and internal paths.
- This does not fail open into admission or expose a demonstrated credential. It loses the promised structured record and value-free error boundary on an ordinary stalled-client/transport failure, and leaks implementation paths/argv in diagnostics. The severity is scoped to that contract and disclosure, not a remote authentication bypass.

**Copy-paste remediation:** normalize subprocess failures at the preflight transport boundary without catching interrupts or treating failures as success:

```python
import subprocess

# In HostProbe.docker, replace the command(...) assignment:
try:
    code, out, err = command(
        [*PREFIX, operation, "--format", template(fields)], deadline
    )
except subprocess.SubprocessError:
    raise Invalid("PREFLIGHT_DOCKER_TRANSPORT") from None
```

Retain the existing code/stdout/stderr validation and `collect()` handling. Do not print the original exception. Add the real closed-pipe/hanging-client regression in addition to the pipe-open deadline test. If the CLI is removed for S1, retain equivalent value-free handling for replacement socket/HTTP transport exceptions. This normalization alone does not fix S1 or S3.

### S3 — Medium: command success reaps the leader but leaves owned descendants running

**Locations:** `docker_backend.py:44–65`, reached from `host_preflight.py:88`.

Once both pipes close, the normal path waits for the leader, sets `reaped=True`, and returns without terminating the process group. A helper that closes/inherits redirected pipes can outlive the leader. The same normal-return path applies to nonzero exit codes; later metadata/stderr rejection cannot undo the leftover process. Plugin discovery in S1 makes hidden helper descendants relevant to this preflight, not just hypothetical native orchestration.

**Proof / impact:**

- `test_03_success_does_not_leave_a_running_descendant` ran a benign Python leader that spawns a five-second sleeper with stdio redirected to `/dev/null`, then exits successfully.
- `command()` returned `(0, <pid>, b"")` in 0.0166 seconds while the descendant was still running. See `adversarial-original.json:37–57`: `command_returncode=0`, `running_after_command_return=true`.
- Reviewer cleanup killed only that identified child via a pidfd and confirmed termination. No Docker, SSH or C0 actor was involved. The primitive itself did not provide that cleanup.
- A helper/extension can therefore outlive the preflight's deadline and report, consuming resources or continuing side effects. This is not a claim that the native cleanup controller is missing; it is the lifecycle of the preflight's own local subprocess.

**Remediation:** removing general CLI/plugin subprocess execution as in S1 is the simplest complete fix for this preflight path. If a subprocess helper is retained, constrain it to audited non-forking behavior and fix successful/failed-return cleanup, preserving the group leader's PID until signaling the group. A Linux-specific minimal correction for the reproduced **same-process-group** case is to replace the normal-path `process.wait(...)`/return block with:

```python
# Wait for leader exit without reaping; preserve its PID/PGID identity.
while os.waitid(
    os.P_PID, process.pid, os.WEXITED | os.WNOHANG | os.WNOWAIT
) is None:
    time.sleep(min(deadline.remaining(), 0.01))

# Terminate descendants even on successful/nonzero normal return.
try:
    os.killpg(process.pid, signal.SIGKILL)
except ProcessLookupError:
    pass

code = process.wait(timeout=deadline.remaining())
reaped = True
return code, bytes(output["out"]), bytes(output["err"])
```

The module already imports `os`, `signal`, and `time`. Keep its exception cleanup and pipe closing. Add a regression that waits within a bounded cleanup budget for termination of the benign descendant after normal return. This snippet is proposed, **not applied or certified** here; it addresses the reproduced group case but is **not** proof of absence for malicious descendants that create new sessions/groups. A process-group-only mechanism cannot make arbitrary Docker plugins safe. Do not weaken the scope to unrestricted plugin execution and call it read-only.

## Controls that held / scope boundaries

| Area | Reviewed result |
|---|---|
| Explicit mutation surface | Preflight Python does not call Docker create/run/start/build, provision the collector, change namespaces/credentials/sysctls, or call ptrace. `HostProbe.docker` allows only info/version; no shell is used. S1 prevents extending this claim to all implicit CLI behavior. |
| Endpoint selection | Absolute `/usr/bin/docker`, explicit local Unix endpoint, no Podman fallback; endpoint/TLS override variables removed. This establishes request destination syntax, not daemon/host pairing. |
| Exported metadata | Allowlisted projection excludes proxy fields; raw daemon ID/kernel/security-option lists are hashed. Selected version strings are bounded/character-restricted. Supplied stderr, nonzero exits, malformed JSON, unknown/duplicate keys, floats/constants and ordinary OSError/ValueError/TypeError values are not copied into reports. S2 is the exception-class gap. |
| Malformed fields | 143 synthetic Docker-field variants: 138 blocked, five valid unchanged `True` resource booleans remained candidates; all admission/execution flags stayed false. Surrogates, nested objects/lists and invalid types were rejected without canary export. |
| Byte bounds | CLI stdout+stderr combined cap 262,144 bytes; projected JSON input cap 16,384 bytes; final report cap 8,192 bytes. Fixed Linux reads have per-read caps, no-follow/nonblocking opens for proc/sys regular files, and interpreter hashing is bounded to 32 MiB with before/after checks. These Python bounds do not bound hidden CLI/plugin allocations (S1). |
| Time/abort handling | Pipe-open stalls and stdout/stderr floods aborted and reaped the tested leaders. The nominal deadline is checked around Linux observations, not a hard interrupt for arbitrary kernel/filesystem stalls; the test results do not certify D-state or hostile mounted-filesystem behavior. Closed-pipe timeout and success cleanup gaps are S2/S3. |
| Collector reads | Only fixed-path `lstat`; no anchor/source/SSH-key/config contents are opened. Leaf symlinks classify as unexpected types. `PRESENT_UNVERIFIED` makes no permissions/source/authentication claim. |
| Architecture / provenance | Explicit native uname/pointer-size checks, interpreter ELF architecture checking, selected Docker info/version architecture consistency, kernel-release digest comparison and before/after host/daemon stability checks. Matching releases do not prove same boot/namespaces or true native execution in all virtualization arrangements; pairing is correctly pending. |
| Fail-closed observations | Required unavailable observations produce blockers; nonzero Yama, missing controllers/limits, unsupported mappings/confinement and absent collector paths block. S2 aborts rather than promoting an incomplete result but does not preserve the promised structured blocker record. |
| No authority inflation | Candidate and blocked reports keep `host_admitted=false`, `runtime_evidence=false`, `execution_authorized=false`, `native_trials_run=0`. Injected probes/deadlines are simulated. No trusted anchor/filter count/execution packet is created. Exit 0 is explicitly candidate-only. |

Daemon/collector namespace pairing, collector installation/admission, native images/actors, complete trial dispatch, timing/calibration and independent runtime cleanup are intentionally pending. They are **not findings against this increment**.

## Tests, retained originals, and disposition

Environment: Python 3.14.8 on Linux. All imports/runs used bytecode suppression. Test subprocess environments used synthetic or minimal values; no real environment credential values were written into evidence.

| Run | Outcome | Retained evidence |
|---|---|---|
| Original preflight suite | 14/14 passed | `baseline-original.log`, `baseline-original.exit` |
| Independent adversarial suite | 12 tests: 8 passed, 3 assertion failures, 1 error; zero skipped | `security_adversarial_tests.py`, `adversarial-original.log`, `.json`, `.exit` |
| Follow-up disposition | 2/2 passed; confirms S2 and dismisses the overstrict deep-JSON assertion, not a repaired source | `followup_tests.py`, `followup-original.log`, `.json`, `.exit` |

The adversarial `test_09` originally expected `metadata()` alone to reject a 1,500-level nested JSON value. Python 3.14 accepted that syntactically valid, bounded JSON; this parser's contract validates keys, with typed semantic validation later. The end-to-end follow-up supplied the same nesting in Docker metadata and obtained `BLOCKED` / `DOCKER_UNAVAILABLE`, with no Docker observation or admission. **This original test failure is a reviewer test overconstraint, not a fourth product finding.** Its original source and failure log were not edited or replaced.

The other original failures/errors are evidence for S1–S3. No remediation was applied and no failure was converted into a passing result by changing the snapshot.

Reproduction of the baseline:

```sh
python3 -B -m unittest discover \
  -s /tmp/opencode/c0-native-preflight-2026-10-17/snapshot/tests/crawl-jobs-v2-observer \
  -p test_host_preflight.py -v
```

The standalone reviewer scripts use exclusive (`x`) report creation intentionally: preserve their original outputs, and choose fresh report names before a later rerun. They contain deny-by-default Popen guards; the only allowed execution fixtures use the absolute current Python interpreter with `-I -B`.

## Re-review / exit criteria

1. Establish a plugin/config-independent, bounded fixed-endpoint metadata transport, or retain S1's safe blocker until implemented. Do not rerun live discovery under the current unconditional non-mutation assertion.
2. Ensure every expected transport timeout/failure yields the bounded, value-free `BLOCKED` JSON/exit-1 contract without an uncaught traceback.
3. Remove the descendant-producing transport path or demonstrate bounded cleanup on normal success, nonzero return, overflow and timeout with benign-process tests.
4. Retain the original failures, pass the existing 14 tests and corrected/extended security regressions, freeze a new inventory and independently re-review only the affected preflight transport. No live/native trials are required to resolve this review.

After these scoped fixes, **GO_FOR_CONTINUED_IMPLEMENTATION** can be reconsidered. Neither that future decision nor this report would admit a host or authorize calibration/execution.
