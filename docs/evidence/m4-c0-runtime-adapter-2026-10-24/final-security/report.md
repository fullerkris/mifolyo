# SEC-01 security re-review

**Date:** 2026-10-24. Separate, read-only review of the remediation snapshot.
The initial security/correctness reviews remain immutable historical evidence.

## Verdict

**GO_FOR_CONTINUED_IMPLEMENTATION** — **SEC-01 is verified remediated** in the
actual final guard. No new in-scope security finding was identified.

Both independent runs passed **13/13 checks**, with effective umasks **022** and
**077**, Python **3.14.8**, and `ResourceWarning` treated as an error. This is a
scoped security signoff for the argument-guard fix, **not** permission to execute
Docker/native actors, admit a host or installed binary, or certify a full runner.

## Input integrity and preservation

- New packet SHA-256, before **and** after:
  `b3e66f6bf5cc95194e04548b7cfb4df4978898d4cec1a48aaaa357a1d4be14e7`.
  All **444** bound files match their sizes/hashes; all **63** source/script files
  match `remediation/source-inventory.json`. Current source matches the frozen
  snapshot and is unchanged during this review.
- The authoritative inventory and an independent snapshot comparison agree:
  only `docker_backend.py` and `test_live_adapter.py` changed since the initial
  review. Final hashes:
  - `docker_backend.py`: `7a3aaaf2de8c3dd79f6b749dfbed00fcf35ed052d62dc20246bd66fe91d19e9c`
  - `test_live_adapter.py`: `6607d0879870d36b3b0573273896416aa9080a6523e6daefdd86f539f4c79e93`
- `streams.py` and `local_process.py` are byte-identical to the first reviewed
  snapshot. The rest of `docker_backend.py`, excluding `cli_arguments`, is
  AST-identical. No lifecycle change is hidden in the remediation.
- All original **329** input bindings are carried forward. Original packet hash
  remains `ef5134712918c3b705b6076f6367ff044a669237a7d47632cc677093c3814ccf`.
- All **13** original security-review files and **27** original correctness-review
  files are unchanged before/after. Their `docs/evidence/.../initial-security`
  and `initial-correctness` exports match exactly, including file inventories.
- Initial failures remain failures in their original evidence: security **14/15**
  and correctness **21/23 per umask**. The original proposed-guard 398-call evidence
  is also retained unchanged. This re-review does not rewrite either verdict.
- Supplied 022/077 broad-suite reports each record **338 passing tests**, retain
  all **335** prior IDs (**318** historical), and add **3** remediation tests
  (**20** total since the historical baseline). All 61 reported source hashes
  match the final inventory. The full 338-test suite was **not** rerun here.

Machine-readable checks: `integrity-before.json` and `integrity-after.json`.

## Regression replay and compatibility

**Actual fix reviewed:** `docker_backend.py:39–63`, particularly lines 49–55.
Existing prefix/operation/long-flag checks remain. Every single-dash token is
rejected except the two fixed Python switches in the final oracle tail, and that
exception requires all of:

```text
not actor
container create
at least eight operation arguments
final six arguments exactly:
--entrypoint python3 sha256:<valid nonzero lowercase 64-hex digest>
-I -B /oracle/worker.py
```

Only the final three tokens are exempted from the single-dash scan. Earlier
tokens still undergo it. Thus a permitted oracle suffix cannot hide `-hh` or
additional `-I`/`-B` options elsewhere. The admission and transport calls remain
after `cli_arguments` (`command:114–117`, actor constructor:82–90).

**Original SEC-01 regression:** the unchanged original method was AST-extracted
from the hash-bound original script and executed against the final snapshot.
All four original forms (`-hh`, `-hhh`, `-hh=true`, `-sh`) reject; none reaches the
transport. Historical module top-level code was not executed, so it could not
create files in the old review directory. Function/script hashes are recorded.

**Negative control:** substituting only the original guard **in memory** makes
that same regression fail, with all four forms reaching the intercepted transport.
The final guard is restored and its object identity checked. The intentionally
failing `negative-control-022.log` / `negative-control-077.log` demonstrate test
sensitivity; they are not failures of the remediated code. No product file was
modified and no process was launched.

**398-call compatibility replay:** the original compatibility function was also
AST-extracted unchanged. Its historical variable named `corrected_guard` was
bound **directly to the actual final `db.cli_arguments` object**, not to the old
proposed replacement. Each run reproduces the exact original 398-call sequence:
**140 CLI/actor guard validations + 258 synthetic metadata calls**. Typed oracle
creation succeeds for both architecture fixtures. Factory failures still produce
value-free `DOCKER_TRANSPORT`; no actor stream is registered on failure.

Canonical call-list SHA-256:
`5d1822da0964bc6059c27b1f45e3daddb3e08e96c19e91a55187292cf86e18fc`.

## Focused negative controls

The following **8,362 rejection invocations per umask** use the actual guard,
with tripwires proving no binary admission, configuration allocation, Popen or
bounded transport occurs:

| Control | Invocations per run | Result |
|---|---:|---|
| All length-1/2/3 short tokens over `hHDcslIBv`, across all ten permitted operation pairs | 8,190 | Reject |
| Attached options, lone dash, whitespace/control characters, and selector/format-value positions | 51 | Reject |
| Wrong/missing entrypoint, interpreter, image digest, Python switches or worker path; reordered/extra tails | 47 | Reject |
| Earlier short tokens before a valid oracle suffix, wrong operation, `--` concealment attempt, actor misuse | 49 | Reject |
| Existing long help/completion/config/context/host gates, wrong prefix/operation, type/NUL/length/count bounds | 25 | Reject |

Positive controls accept three valid nonzero image digests, including a mostly
zero digest, with the exact minimal oracle suffix; they reach only a stubbed
transport and remove their temporary configurations. The exact actor-start form
still validates. The three new product regression methods also pass, including
actual typed oracle-argv generation for both architectures using fake metadata.

The per-umask observations and compatibility sequences are identical. Evidence:
`rereview_probes.py`, `probes-{022,077}.{json,log}`,
`negative-control-{022,077}.log`, and `compatibility-calls-{022,077}.json`.

## Scope and limits

- New artifacts and all temporary profile/configuration fixtures stay under
  `remediation/security-review/`; scratch directories are empty after both runs.
  The probe audit tripwires recorded **zero** prohibited subprocess, connection,
  or outside-write attempts. No Docker, SSH, native actor, calibration, other
  agent, Git command, product edit, or old-evidence edit occurred.
- SEC-01 shares the root cause recorded as correctness R1. This is the separate
  **security** closure; it does not overwrite or issue a new correctness verdict.
- Unchanged lifecycle behavior was hash/AST-crosschecked, not re-exercised with
  child processes during this narrow re-review. Prior lifecycle evidence remains
  prior evidence; no process-group/escaped-descendant or remote-container absence
  claim is added.
- No installed Docker binary/version was invoked or admitted. No real daemon
  metadata was read. The original version-specific Docker v29 source reasoning
  remains in the immutable `security-review/upstream-notes.md`; the fix prevents
  the prohibited argv from reaching any executable. Current Context7 CPython
  documentation was consulted for the AST replay mechanism.
- The host still awaits actual provider/hypervisor management details for a
  sibling VM. No host policy, package, VM or collector change occurred. There is
  no fresh full preflight, daemon/kernel/namespace pairing, native calibration,
  native x64/ARM64 acceptance, full-runner certification or execution approval.
- `result.json` is the machine-readable verdict. `review-artifacts.json` hashes
  the completed review files, excluding itself. The original reports remain
  immutable and their previously failing results are preserved honestly.
