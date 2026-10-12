# MiFolyo C0 independent SECURITY remediation re-review

**GO_FOR_CONTINUED_IMPLEMENTATION** — no unresolved in-scope finding after the
verified remediations. This is a source/static-artifact gate, not publication,
installation, live-authentication, native-runtime or M4 approval.

## Source and history

All **45 package files plus original runner** matched the new inventory before
and after. Only `context_transport.py` and `test_context_provisioning.py` differ
from the initial snapshot. Both snapshots remain unchanged, without extra files;
all **39 initial security/correctness report and probe files** were rehashed and
preserved. No repository, documentation or Obsidian content was modified.

- New inventory: `2ecf2c68b6a3293cc3b34314e23022b3f35c2eda69f9f8efa0391728804a8edf`
- Remediation record: `be92677e3a523e0659800cbd6cf59944f6f1ad23994cd9175fcc485f1308d99b`
- New transport bundle: `20b53849d3617f1a7f1dc2897cd2ab3fed48990b8df188317c8c136d8276869a`
- Transport file: `b8d5d1f1dd568e66115ee6745cb385965f5e7dfac63e7b3d9f0c0979020cc1eb`

The initial security review did not exercise exited-leader/first-setup failure
or preparation-mismatch reuse and did not detect COR-SSH-002/003. This re-review
independently reproduced both correctness findings on the original snapshot and
verified closure on the new one. It does not retrospectively relabel the initial
GO or the original **196-pass/one-error** test record.

## Finding closure

### COR-SSH-002 — High/P1, VERIFIED FIXED

**Location:** `context_transport.py:66,73-77,104-118`.

The original empty map was mistaken for observed EOF before the first successful
registration. An exited leader could be reaped without killing its pipe-holding
descendant. Fresh owned-Python probes reproduce the prior failure: first
`set_blocking` and first `register` failures generate only `wait`, leaving the
inherited pipe open and descendant live. The reviewer explicitly disposes of
those control groups.

The corrected `setup_complete` flag is set only after all three registrations.
An incomplete setup therefore kills the group **before** reaping. All six
first/second/third nonblocking/registration faults produced `kill -> wait`, closed
parent resources and actual EOF on an independently duplicated output reader.
The registered-pipe timeout control stops the descendant on both variants;
normal echo/EOF succeeds without an unnecessary group signal. Final FD delta: 0.

This closes the supported owned-process cleanup contract. It does not assert
that normal pinned SSH flags spawn helpers, or that local cleanup proves remote
cessation. Exact current remediation function is retained in `review.json`.

### COR-SSH-003 — Medium/P2, VERIFIED FIXED

**Location:** `context_transport.py:157-181`.

The old helper accepted preparation B with a different deployment or transport
digest after constructing the source with A, issuing a B-bound lease through A.
Fresh before probes reproduce both gaps with copied and in-place changed packets;
the original host-source mismatch and fresh-factory rejection controls still fail
closed. The normal one-shot backend was not shown to bypass authorization.

The new private `ContextSource` subclass retains a separate three-way binding
copy and snapshots validated preparation before every acquisition. The new
source rejects all six mismatched-packet cases **before authorization or I/O**,
without spending challenge/frame/byte budgets. Additional independent controls:

- Matching-source reuse and an unrelated `target_elf` change still succeed with
  three fresh challenges and the correct whole-preparation lease hashes.
- Public receipt rewriting cannot rebind the source. Sixty-five refused attempts
  consume no challenge capacity; a subsequent valid acquisition succeeds.
- Mutating the caller's packet during either authorization or exchange does not
  rewrite the signed request or lease: both retain the admitted snapshot hash.
  The mutated packet cannot view that lease.

The full subclass/factory remediation code is retained in `review.json`. These
are supported-API guards, not isolation from hostile code already executing as
the trusted controller UID.

### COR-SSH-001 — Medium, REMAINS FIXED

**Location:** `context_transport.py:61-76,104-122`.

Selector allocation still precedes spawn. Fresh `ENFILE` probes on both reviewed
variants create no child and leak no descriptor. The three earlier selector,
spawn and pipe-startup regression IDs all pass in the complete suite. Original
pre-COR-SSH-001 one-child/three-pipe evidence remains preserved in the first
security report; it is not confused with the newly reproduced COR-SSH-002.

### QA-ENV-001 — Informational, VERIFIED FIXED

**Location:** `test_context_provisioning.py:232-234`.

The private simulated install directory now explicitly calls `root.chmod(0o755)`.
Its private parent remains owner-only; production file/directory checks are
byte-identical and were not relaxed. The previously failing test ID passes in
the single new full-suite run under **umask 077**. Original failure and isolated
022 retest evidence remain unchanged.

## Verification

The complete frozen suite ran **once** under umask 077, `TMPDIR=/tmp/opencode`,
`python3 -B -W error::ResourceWarning`, via the original runner:

**202/202 PASS**, 13.362 seconds; no skips, expected failures, errors, ignored
exceptions or ResourceWarnings. All 197 prior IDs remain, with exactly five new
regression/positive IDs. The report's 45 source hashes and runner hash match the
inventory. This is an independently executed run, not a relabelled main-session
result or a repair of the historical security report.

The variant-isolated `probe-remediation.py` reproduces old counterexamples and
checks the fixed version in separate Python processes. It uses owned benign
Python children, files and pipes plus simulated Linux context only. It makes no
SSH connection and runs no native actor or real collector. The 202-test suite
also retains the existing credential, host/client pinning, canonical framing,
secret-free error, policy, deadline and backend authorization coverage.

### Static artifact continuity — PASS

Actual bytes were rehashed, not just previously recorded PASS booleans:

- **11 native outputs**, oracle manifest and raw interpreter match the initial
  independent security artifact check exactly.
- **9 native** and **10 oracle** source/recipe inputs are unchanged in both
  snapshots. Native toolchain/source and oracle source/interpreter joins remain
  exact. Host source and oracle-worker aggregate identities are unchanged.
- Prior build/pull/extraction record hashes and both reviewers' original evidence
  remain intact. Only the transport aggregate changes as specified above.

This preserves the earlier independent ELF/instruction-manifest/compiled-filter/
three-profile/source joins, including the original 987 compiled-BPF vectors.
Those vectors were **not rerun** for this transport/test-only delta. No build,
container workload, native program or ELF execution was performed. Original
artifact check SHA-256:
`27865da68adc1c69a64fd756bbfd66bbf765592673557d32bf4d25481c168c52`.

## Remaining boundaries

Root/operator remains the trust root for source, pins, executable dependencies,
secure path ancestors, account/startup environment and effective live SSH policy.
The placeholder private key still proves no real authentication or key pairing.
Identify and admit a collector in the selected daemon's actual Linux/AArch64
kernel/namespace domain; the local Docker endpoint is unchanged. Preparations
must use the **new transport identity**; public receipts grant no authority.

Actual installation, live SSH, full OCI/Python runtime admission, native
confinement, ptrace/timing/calibration and whole-trial cleanup/absence remain open.
Local pipe/group closure is not remote-stop proof. Historical artifact provenance
does not newly attest live Podman state or original-container preservation.
X64 runtime remains **planned and outside this delta**. No agents, live SSH,
Docker/Podman workloads, tracing, calibration or host-policy mutations were used.

## Reproduction and evidence

From the new frozen snapshot, the completed suite command was:

```sh
umask 077
TMPDIR=/tmp/opencode python3 -B -W error::ResourceWarning \
  scripts/test-crawl-jobs-v2-observer.py \
  --report /tmp/opencode/m4-c0-ssh-remediation-2026-10-11/security/c0-tests.json
```

Focused probes: run `python3 -B -W error::ResourceWarning probe-remediation.py`
with argument `before` or `after` from this private security directory.
`check-continuity.py` performs static continuity checks only. All generated
records are under this new private directory. Exact report hashes are in
`SHA256SUMS`; evidence hashes follow.

| File | SHA-256 |
|---|---|
| `integrity-before.json` | `11f4381c6f7aca276bec74c1479e33a098373a8b8e3aa70c026856d3f61b1a8e` |
| `integrity-after.json` | `084bba56f4d3a11fc649c4d680aafb357f2165efd6c646ab1a0b64aa40c040ed` |
| `c0-tests.json` | `1d8d1cd2b3cecf39bbc61652968971a40c853902c2d08df91cb42502fa6494fc` |
| `c0-tests.log` | `f484b7ebd3e37490f2926a273342c2a326a1fb4b374d3ee757f58f104965dcfc` |
| `test-verification.json` | `88caf2fc0ed8b9916b04c1dde42e3ed4dfd30f537a734416a837f0661241d93f` |
| `probe-remediation.py` | `0f140e2141ee004da620172d26bf7d8287677da86cfcaded3195991e95bc268d` |
| `probe-before.json` | `9f098dfe46dc7f91c263bd5f460aeae2c6306f79e6de8a8817aaacd9b1b0cb3c` |
| `probe-before.log` | `9f098dfe46dc7f91c263bd5f460aeae2c6306f79e6de8a8817aaacd9b1b0cb3c` |
| `probe-after.json` | `a8ba0fcb2cd7fea90251dc179ea4ca41ddc0c1ee39f3cad80bb5dd6701eebe7f` |
| `probe-after.log` | `a8ba0fcb2cd7fea90251dc179ea4ca41ddc0c1ee39f3cad80bb5dd6701eebe7f` |
| `check-continuity.py` | `94825e92be49adc763eb409f4091623f01d171e99793f217898884f24a87f4f1` |
| `artifact-continuity.json` | `a193040963356dd64397fa00cb90d6e74495fa8d97dd56da618f1254e4ea1059` |
| `continuity.log` | `ab8c7d5fd6f9479403032126397f6d7f06803e77d563868e3946913da4db9fbc` |
| `write-review.py` | `9004cf4e92b1227758e019aa5f6abe8318cda4f4071d4a4c876582cbf2402438` |
