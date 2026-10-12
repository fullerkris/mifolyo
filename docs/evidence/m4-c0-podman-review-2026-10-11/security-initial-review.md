# MiFolyo C0 independent security review

**Verdict: GO_FOR_CONTINUED_IMPLEMENTATION** — source/static-artifact review only.

No unresolved exploitable security-boundary defect was found under the documented
root/operator trust model. This is **not** publication permission, installation
approval, native runtime acceptance or M4 acceptance. X64 runtime remains PLANNED
and outside this review.

## Frozen scope and integrity

- Reviewed the full increment diff; complete new `context_files.py`,
  `context_provision.py`, `context_ssh.py`, `context_transport.py`; worker/contracts,
  signed runtime context, Linux observations, admission and backend integration;
  relevant failure-path/security tests.
- Checkpoint `1e956962596f7bb86143d45dd62ebf863c0b64eb`, base
  `64c98a78e0e0ae1731f1516c06932bc881e8becf`, plus the local startup correction.
- All **45 package files plus original runner** were hashed before and after.
  Every inventory join passed, no extra/symlink snapshot files, no byte changes.
- Source inventory: `f53058a955de01027efcdbaa6adace7d6904197c845e5886b25e7d9db41bc227`.
- Corrected transport source bundle:
  `1f83f90e21d13092ff8ac146d2415681afceb15de7ac5780b308413b0024abdd`.

## Findings and verification

### COR-SSH-001 — Medium, verified fixed

Original `context_transport.py:64-69` spawned before allocating its selector and
before entering cleanup. Injecting selector `ENFILE` with an owned benign Python
sleeper reproduced **one live child / three open pipes / +3 descriptors**. The
reviewer disposed of that original control. Corrected `:61-76,103-120` produced
**zero children / zero leaked descriptors** for the same injection.

Minimal remediation is already present: allocate the selector before `spawn`,
initialize `process = None`, and put spawn, pipe setup, exchange and cleanup in
the guarded lifetime. Exact corrected implementation is preserved in the frozen
file. Independent failures at all three nonblocking/register positions, spawn
`EMFILE`, and deadline expiry between selector allocation and spawn also passed.
The impact of the original bug was local resource exhaustion on approved failed
invocations; this is not presented as remote arbitrary execution.

### QA-ENV-001 — Informational, nonblocking fixture issue

The required complete suite ran **once** under reviewer-selected `umask 077`:
**197 started, 196 passed, one error, no skips or ResourceWarnings**. The failing
test is `ProvisioningTests.test_ssh_entrypoint_checks_exact_code_inventory_and_interpreter`.
At `test_context_provisioning.py:232`, `mkdir(mode=0o755)` becomes mode 0700 under
that umask. Production `context_files.directory()` correctly rejects it. The
same unchanged **single test** passed under umask 022. All 194 earlier IDs remain;
the three new failure-path tests passed. All 197 distinct IDs therefore passed
across the complete run and isolated retest, **not in a clean single full run**.
The retained complete-suite JSON remains `FAIL` and must not be relabelled.

Optional minimal fixture-only hardening (not applied):

```python
root = Path(folder) / "code"
root.mkdir(mode=0o755)
root.chmod(0o755)  # exact simulated install mode inside private TemporaryDirectory
```

Reproduce with the named test under `umask 077`; compare under 022. Do not weaken
application ownership/mode checks or globally relax umask. No production
permission bypass was demonstrated by this observation.

### Additional independent checks — PASS

**13 probe groups**: 35 deployment rejection variants; canonical JSON; traversal,
owner/mode/link/FIFO/size and actual named-file replacement checks; real offline
`ssh -G` for numeric IPv4/IPv6; original/corrected cleanup comparison; partial
startup failures; deadline races; bounded real benign process pipes; three-way
preparation binding; strict boolean authorization; generic refusals and private
credential-copy cleanup; HMAC/frame tampering/replay; forced policy/marker checks;
outer backend authorization revocation after exchange. Final probe FD delta: 0.
Silent/closed-output-but-live peers were reaped in approximately 0.20 seconds for
0.20-second probe deadlines. No real collector reader or native actor ran.

**Static artifacts**: all **11 native outputs**, oracle manifest and raw
`oracle-python3.13` match both original evidence records exactly. Independently
checked ELF64/little-endian/AArch64/ET_DYN, non-W+X load segments, nonexecutable
stack and RELRO; native artifacts have no dynamic interpreter. Verified target
instruction manifest/ELF, cases/header, three seccomp profiles, exact compiled
observer `.obs_filter` and generator bytes, and **987 interpreted compiled-BPF
vectors** without installing a kernel filter. Toolchain/native/oracle/source
hash joins all match. Original evidence digests:

- Source inventory: `b69502b2287b08331570f7a7c2728586da216341ec9da0fdb0c097abfaac4c4a`
- Build verification: `c9826477cb4e8afbfbe3adb6c41112f1af87be97ebddf7e06a53e07b4cd2ec58`
- Oracle manifest: `3f51809ebddbad24cbe03611e67b678df003121b77aff0743de0251baf538131`
- Oracle interpreter: `cb4548d280f38c2b1df4cba45f12c90a034729e08fce5bf1a223c58add0e21e3`

Build/pull logs match their retained record hashes; build records use pinned
ARM64 bases, artifact target, no network, pull-never and explicit ignore files.
Container extraction/removal records are consistent with never-started extraction.
This reviewer did **not** independently query live engine state or re-attest
container preservation. No Podman/Docker workloads were run during this review.

## Boundary assessment and remaining gates

- Root/operator owns the pins, installed code, executables, dependencies and
  trusted anchor. Malicious root replacing these is outside this model.
- Controller files are owner-only, bounded regular single-link no-follow reads;
  private exchange copies avoid reopening mutable admitted credential paths.
  Public manifests/receipts exclude secret values and secret-file hashes.
- SSH uses fixed executable/argv and isolated configuration: strict pinned host
  checking, fixed numeric endpoint/command, no agent/password/proxy/forwarding or
  control socket. HMAC, fresh request challenge and source/daemon/preparation/lease
  joins prevent accepting forged, replayed or rebound context in tested paths.
- Installed path **ancestors**, dedicated account and home/startup environment,
  executable dependencies and effective live `sshd` configuration remain operator
  admission responsibilities. File helpers check final components; this is not
  an attestation against hostile ancestors/root or hostile code already running
  as the controller UID. Live policy must be reviewed, not inferred from text.
- The private-key placeholder proves neither parsing/key pairing nor live
  authentication. A real matching client key and host policy must be admitted
  separately. Bundle possession is not installation or execution authorization.
- Identify an SSH-accessible Linux/AArch64 host in the **selected daemon's actual
  kernel/namespace domain**. The backend still uses the existing local Docker
  socket. A different host cannot attest Docker Desktop's kernel.
- Exact OCI/runtime-image admission, Python shared-library/runtime identity,
  real context acquisition, trial/timing/confinement, cleanup/absence assembly
  and separately approved calibration remain open. ELF byte identity is not
  full runtime feasibility. Local child/pipe cleanup is not remote-stop proof.

## Reproduction and retained evidence

The complete run used the frozen snapshot working directory and:

```sh
TMPDIR=/tmp/opencode python3 -B -W error::ResourceWarning \
  scripts/test-crawl-jobs-v2-observer.py \
  --report /tmp/opencode/m4-c0-podman-arm64-2026-10-11/security/c0-tests.json
```

The shell umask was 077 as disclosed above. Probe scripts are independently
re-runnable with `python3 -B -W error::ResourceWarning` and write only within
this private security directory. All repository/snapshot/document/Obsidian bytes
were left untouched; no agents were spawned, production connections made, host
policy changed, ARM64 target/observer/oracle worker/real collector run, or native
tracing/calibration performed. UTC execution timestamps may be October 12 while
the supplied review label is October 11.

SHA-256 of retained evidence (full report digests are in `SHA256SUMS`):

| File | SHA-256 |
|---|---|
| `integrity-before.json` | `f092982586551c985f457a86b584f36269846388cf5d359fcf2bdb7c1bca2fb3` |
| `integrity-after.json` | `5269603ec4555f4ce94045cd866fbec8c8563d8478216171cfe389949f4be4a0` |
| `c0-tests.json` | `66161b67cf93ce43f405314ef4b4606aaa6af2cf9f94d350fe2d07fbc0065310` |
| `c0-tests.log` | `524255410bb68d7f2a599340cae5ab2784aa735c09c939320a8d2f707ca1c631` |
| `isolated-umask-retest.json` | `72da6af58a6f29f7ab013d3b655c8c650eabcfb5f17269750bda456d3a919e10` |
| `isolated-umask-retest.log` | `52536f4378bdcb072740e499d5156e4fbb3207d104f4e7b02d9e7950e874fdd9` |
| `test-inventory-comparison.json` | `046d58a58f3e3bc67890766267294123052856f255d124dc0124345ffcf72035` |
| `probes.json` | `607210143f9a8b119d7987046cd1e3d603a4b9535c25cdee7efdd54856edd8f9` |
| `probes.log` | `b5bc6e77aa387ee7af0e20e57d79f97477d5f4ec4d5ff6bbf3584bfa64a0588b` |
| `probes.py` | `325d4c36cb6fe149f4d00c0798f35c96a0c9a60f0734f849c7e53616bb706796` |
| `artifacts.json` | `27865da68adc1c69a64fd756bbfd66bbf765592673557d32bf4d25481c168c52` |
| `artifacts.log` | `66f0f695e8c2a8c88c925bca4da92b033b81dd082181bf9531df04aa2181b9d1` |
| `inspect-artifacts.py` | `9a2135bb5c59928288793c9af1c4c7a33ac8f08358e0ee47ca7490155aa4a5df` |
