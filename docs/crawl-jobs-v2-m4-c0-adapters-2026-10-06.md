# C0 owned-resource, stream and watchdog layer — 2026-10-06

**Correctness GO and security GO for continued implementation.** The next C0
adapter layer passes **78 tests**, including bounded benign local Python process
fixtures. All four correctness and three security findings are closed. No Docker
resource mutation, native actor start, tracing or Redis experiment was performed.

Work remains local on `feature/crawl-jobs-v2-c0-observer`, based at `e80d005`.
The package now has 22 source/test/documentation files. The original
[15-file foundation](crawl-jobs-v2-m4-c0-foundation-2026-10-06.md), design records
and their dated review states remain preserved.

## Added components

- **Owned resources / local Docker adapter:** closed names, image identities,
  daemon binding, commands, users, capabilities, namespaces, profiles, memory,
  mounts and stopped-state admission. Candidate names are acknowledged before
  creation; returned identities are bound before activation. The default adapter
  is read-only; an outer authorizer must explicitly permit mutations.
- **Bounded streams:** canonical fragmented NDJSON, shared 64-frame/64-KiB input
  budget, stderr refusal, fixed case/start/resume messages and phase ordering.
  Both complete and partial early output reject before dispatch. Local CLI EOF
  or reaping proves neither remote exit nor complete process-group absence.
- **Independent watchdog:** a separate host session, bounded private raw-byte IPC,
  monotonic resource prefixes, explicit external cleanup delegation and an
  exclusive mode-0600 fsynced journal. Owner EOF/deadline/protocol failure triggers
  scoped cleanup. The parent requires exact acknowledged-prefix, trigger/final
  bytes, file identity and hash reconciliation before accepting a final receipt.

Only the observer joins the target's PID namespace. The oracle uses a private
namespace so target PID-1 exit cannot kill its completion reader. The observer
has no control/witness volume; target/oracle use fixed 1 MiB UID/GID999 tmpfs
volumes with the oracle read-only. These are construction/admission policies,
not runtime filesystem or kernel measurements.

Cleanup distinguishes ownership from confinement: a known owned created object
can be removed safely even if admission failed, but foreign/replaced/attached
resources are not deleted. Safe cleanup continues elsewhere. A candidate-only
create stays **unsettled**, even after observed name absence, because the daemon
could complete an ambiguous request later. Such a prefix cannot claim complete
cleanup. Daemon identity is checked around reads; another daemon's empty inventory
is not proof of absence.

## Exact final bindings

| Binding | SHA-256 |
|---|---|
| Final 22-file inventory | `c754392b6a3f8ebbd03601a626bcce908da4a86ebe82d8cea8ef213913eb74e6` |
| Final read-only snapshot intake, 1,505 files | `c552a0d0da999c2ea94acba3077954a13f10be40783996194877613edb1497ee` |
| Final verification, 78 tests | `1a555dc2793c7e97a645dd4afe353aa40e69c5d967180cb34aa508a3b6ffcc5d` |
| Correctness final review | `39cbc6203475b9a90a23c8a995d6de8b9a8a6163acc4496fdfda7df20b4882fd` |
| Security final review | `d87b19a940641fd5f68a3d96ae5711e3bf1643c1855f2c31bffa42f2662146ed` |
| Combined decisions | `a3d9e0853ab4eaf2d6615070e2172bd94729b012d3088ca846ecf671aa8adfb8` |

All eight native build inputs and eleven retained native artifacts match the
foundation record. This Python adapter work did not rebuild or execute the native
programs. The normative Redis protocol and existing Redis fixture runtime remain
unchanged.

## Review and verification chronology

The first adapter snapshot passed 71 tests but received NEEDS_REMEDIATION from
both reviewers. The corrections cover:

1. Supported `docker container wait ID` syntax, followed by independent stopped /
   PID-zero inspection.
2. Rejecting partial pre-dispatch responses as well as complete queued frames.
3. Rechecking the full acknowledged attachment inventory immediately before start.
4. Collecting an already queued watchdog result even if a cleanup-request write
   finds the peer closed.
5. Explicit cleanup delegation before spawn and again in the child; no authority
   inferred from a scope hash.
6. Rejecting executable image/container healthchecks and emitting `--no-healthcheck`.
7. Requiring intact journal retention and the exact acknowledged prefix for final
   completion; independent cleanup still proceeds when retention fails.

The final source passes 78 tests, no skips, with ResourceWarnings treated as
errors. Correctness passes 19 independent probes twice with identical observation
logs; security passes 37 adapter tests and 21 independent semantic probes. Positive
controls use the corrected APIs, so closure is not attributed to interface errors.
The original reports and both read-only snapshots are retained.

Tests include synthetic Docker creates/failures/metadata/cleanup and real benign
Python pipe/watchdog processes. Actual benign owner-process exit was exercised,
with fake Docker cleanup. These observations are not evidence of real Docker
cleanup, target capabilities, installed seccomp, ptrace or held-boundary precision.

## Next layer

Native kernel identity/admission receipts, the oracle/client closure worker, full
trial dispatch and receipt/timing/resource assembly, exact runtime images and
separate approval/execution binding remain incomplete. The authorizer is an
integration boundary, not an implemented approval issuer or a new `run` CLI.
No complete executable OBS1 packet is claimed.

See the [package contract](../tests/crawl-jobs-v2-observer/README.md) and
[evidence index](evidence/m4-c0-adapters-2026-10-06/README.md). Private originals,
probe logs and frozen snapshots remain under
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-c0-adapters-2026-10-06/`.
