# M4 D01/D02: static feasibility and decision package — 2026-10-05

**Review-ready research and proposals; no experiment or normative change is
approved.** This package advances the existing local-Docker observer and
AOF-failure design. It does not close D01/D02, OBS0–OBS4 or full M4.

Work is isolated on `docs/crawl-jobs-v2-m4-design-gates`, based at
`e80d00538b11c46d021427bcfe1c408e16071c68`. In parallel, correctness/security
reviewers accepted the separate frozen cancellation source for image/CI
preparation. That implementation is not applied to this design worktree; the
shared normative/Lua baseline remains unchanged in both streams.

## What advanced

| Workstream | New evidence / proposal | Remaining boundary |
|---|---|---|
| D01 binary availability | Exact cached-image ELF recovered through Docker image inspect/save; matches previous size, SHA and build ID | Static bytes only; no container/Redis/ELF execution or tracing |
| D01 boundary candidates | 37 native function bodies decoded; ELF-relative call, VM-fetch, AOF-write/sync and reply candidates mapped | Canonical Proto/bytecode/ordinal binding, runtime load bias and held-stop precision unproven |
| D01 calibration | Finite 276-trial synthetic OBS1 proposal, with confined capless and separately gated SYS_PTRACE candidates | No observer images/profile or demonstrated capability/precision/cleanup result |
| D02 lifecycle gap | Live DELUSER retirement leaves startup ACL bytes unchanged; restart may reload old grants | Existing cases restart before retirement; future restart recipes need a new reviewed lifecycle |
| D02 outcome classification | 19 matrix rows, nine receipt contracts, nine proposed exception predicates and 36 grouped negative controls | Research requirements, not installed validators or runtime evidence |
| D02 coverage choices | Limited prefault-retired corruption-admission candidate; optional narrower cleanup-only exception proposal | Owner sequencing/coverage decisions and independent design review remain required |

## Exact sources and evidence classes

| Binding | Identity |
|---|---|
| Normative protocol | `835e98db86e4d0cba224bc9fb9c3a3408514e4730d832773b7448f9b238a69c9` |
| Preserved September 25 proposal | `f6568eb6b322fa84fc9164d1b616d983a4f71cdc1dc85d42bccc4cb59b1479ae` |
| Redis image config ID | `sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c` |
| Exact ELF | 16,892,616 bytes; `772f79e9154598fe509961928dc4d7c1ac577a948be6e86b793fdb7b0599e1b6` |
| GNU build ID | `a6678635938881e640c42ca2824a014aea114c4f` |
| Pinned upstream Redis 7.4.11 commit | `aaf0ce63b3239f4b51f86ca1da8711b055721993` |
| D01 canonical evidence | `487af0243d96240f400bedae2e7465676e527749bae06fd94136e1f3e239b5b0` |
| D02 canonical evidence | `e28e9278831e9709a10e0635832668b766ea5b2236729f2fc54f261435c70e70` |
| Parent integrity/interface reconciliation | `e66ee8ae1505ea5995d33be7eb5c1249856b17b8276ce7e95cb80a8f5419b900` |

Both researchers pinned the same upstream source revision. D01 additionally
verified the source archive hash recorded in the image history and matching
selected upstream files. Historical compiler/dependency/generated-header and
post-patch build provenance remain incomplete; a version tag/build ID is not
reproducible-build proof. Static source/binary candidates and proposed calibration
requirements are distinct evidence classes.

The parent rehashed all 114 D01 retained artifacts and the exact ELF, verified
D02's six local input hashes, and confirmed that cancellation did not change the
executor's `initialize`, `revoke`, `ready` or `configuration` function ASTs on which
D02's lifecycle observations depend. This integration check is not an independent
observer-feasibility or normative-acceptance verdict.

## D01: concrete findings and next static work

The [D01 report](evidence/m4-design-gates-2026-10-05/d01/proposal.md) distinguishes
ELF-relative virtual addresses and file offsets from runtime addresses. Examples
include native dispatch around `call`, the `luaV_execute` fetch candidate, and
write/fdatasync sites inside `flushAppendOnlyFile`. The actual write loop is
inlined; a breakpoint at standalone `aofWrite` would miss that path. Reaching an
fdatasync return site does not prove success or persistence of the intended bytes.

No `lvm.c` compilation unit or Lua VM line-DWARF rows were found. The VM fetch
instruction and register roles are disassembly candidates, not a proven loaded
canonical Proto/PC mapping. That missing correlation remains a blocker for
prevalidation and per-command fault cuts.

Canonical COMMIT assembly matched its pinned bytes. A specified branch was
materialized as **1,050 semantic descriptors**, with distinct three-descriptor
first-backpressure and zero-write replay branches. This is source-derived ordinal
accounting, not a constructed valid maximum-count/maximum-byte fixture. Native
command count, dirty effects, propagated AOF records, durable bytes and client
receipt are separate quantities.

The [OBS1 calibration proposal](evidence/m4-design-gates-2026-10-05/d01/calibration-plan.md)
defines 276 trials per admitted candidate: 140 native ordinal cuts, 60 fake-VM cuts,
40 asynchronous/acknowledgment controls, 24 lifecycle-loss trials and 12 closed
negative controls. Its pause/read/output/resource limits are proposed rejection
limits, not measured capabilities. OBS1 uses a purpose-built synthetic process;
even a successful future OBS1 result would not establish Redis VM correlation.

## D02: outcome and authority distinctions

The [D02 report and matrix](evidence/m4-design-gates-2026-10-05/d02/proposal.md)
separate `known_pre_acknowledgment`, `acknowledged` and `unknown` for each exact
client/session/invocation. Probe, BOOT, setup, first backpressure and publication
acknowledgments remain separately accounted effects. Missing ACK logs are unknown.

Redis 7.4.11 loads ACLs before listeners and AOF; loading may service AUTH/ACL
commands. Socket reachability, successful authentication, finished dataset loading,
protocol readiness and AOF refusal therefore require different receipts. Generic
exit, OOM, timeout or AUTH failure cannot substitute for source-matched AOF damage.

Current `ACL DELUSER` calls do not update `fixture.acl`. Saving before the final
revoker is deleted can preserve that revoker, and saving afterward cannot assume
remaining administrative authority. An empty ACL file is not deny-all: Redis can
recreate a permissive default user. A proposed prefault-retirement route must
persist an explicit deny-all startup policy and prove live session/reconnect
retirement separately.

The limited no-amendment candidate applies only to a separately reviewed
corruption-admission negative, with the deny-all file sealed and all live roles
retired before the fault. It needs new helper/lifecycle proof and an owner decision
on sequencing; it cannot be substituted for an authenticated COMMIT killed in
flight. The alternative amendment proposal is narrower than the old draft:
unmodified-AOF COMMIT process-crash, affirmative pre-ACK evidence, exact refusal
and complete owned-resource extinction. It substitutes only for teardown proof.

Acknowledged publication/backpressure and separately scoped fixture effects
retain their complete durable-effect obligations. Refusal provides no recovered
post-state or new BOOT approval. Safe disposal, fault-safety outcomes, recovered
atomicity and acknowledged-write durability remain separate assertions; cleanup
alone never produces full M4 acceptance.

## Parent integration clarifications

These clarify the follow-up proposal contract; they do not apply a protocol change.

1. **Static versus instantiated seccomp identity.** Freeze the exact OCI profile,
   additional-filter template/generator and validator identities before approval.
   A TID-specific filter's actual bytes/hash are generated only after the target
   identity is bound. Pre-attachment receipts must bind the approved template,
   allowed substitutions, observer/target identities, TID/start time, generated
   bytes/hash and successful installation. Choose either explicitly approved
   deterministic instantiation or a separate per-instantiation approval model.
   An unknown future PID-specific hash is not an exact preapproved artifact.
2. **C1 is a feasibility hypothesis.** Inspect capability sets after observer
   entrypoint exec under the pinned runtime/profile. Losing SYS_PTRACE, unexpected
   sets, denied operations, or requiring unapproved root/file/ambient/inheritable
   authority or another exec blocks C1. Prefer capless C0 if it is sufficient;
   there is no automatic privilege fallback.
3. **Client closure remains independent.** D01's selected-thread hold and fault
   receipts do not prove whole-process termination or client nonreceipt. D02
   requires an independently sealed interval through actual target stop and client
   retirement, accounting for buffered/late complete replies. Server PC, native
   return, fsync and socket write do not replace that proof.
4. **Research dumps are not runtime manifests.** The retained full location dump
   is 2,666,992 bytes, above the current 2 MiB artifact bound. A future executable
   observer needs a compact, closed, separately reviewed location manifest. This
   package neither raises the bound nor admits raw DWARF/disassembly as a recipe.

## Decisions needed next

| Decision | Recommended next action | What remains unapproved |
|---|---|---|
| Static evidence | Independent review of exact-byte recovery, address/offset/PLT joins and ordinal derivation | Runtime mapping/equivalence or held-stop acceptance |
| OBS1 preparation | Decide whether to authorize implementation-only preparation of fixed synthetic target/observer/validator artifacts, beginning with C0 | Any tracing run, C1 privilege grant or Redis experiment |
| D02 coverage | First assess the limited prefault-retired corruption-negative route and its sequencing interpretation | Calling that route compliant or treating it as in-flight COMMIT coverage |
| D02 exception | Pursue the narrower cleanup-only amendment only if unreachable pre-ACK COMMIT refusal evidence is needed | Normative edits, regenerated pins or validator relaxation |
| Later execution | Freeze reviewed images/profiles/receipts and obtain exact case authorization after the preceding decisions | Reuse of current cancellation or historical approvals |

The [evidence index](evidence/m4-design-gates-2026-10-05/README.md) lists curated
exports and their hashes. Raw image archives, ELF, full DWARF/locations, private
analysis dependencies and command logs remain retained outside the repository.
The [September 25 proposal](crawl-jobs-v2-m4-observer-and-failure-design-2026-09-25.md)
and [normative protocol](crawl-jobs-v2.md) remain unchanged. The primary
[implementation plan](crawl-jobs-v2-plan.md) owns broader status; full M4 is open.
