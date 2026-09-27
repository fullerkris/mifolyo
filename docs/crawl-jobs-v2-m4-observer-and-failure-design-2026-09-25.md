# M4 local observer and failed-start teardown — design proposal, 2026-09-25

**Status: DRAFT FOR INDEPENDENT AND OWNER REVIEW.** The owner selected local
Docker as the observation target and requested an AOF-failure teardown proposal.
This document grants no tracing capability, Redis execution, protocol amendment,
or acceptance. The normative protocol and existing controller rules remain intact.

The primary [implementation plan](crawl-jobs-v2-plan.md) owns progress. This is a
dated design proposal addressing D01/D02 in the
[M4 readiness assessment](crawl-jobs-v2-m4-readiness-2026-09-25.md).

## 1. Fixed target and equivalence requirements

- Target: the existing Docker Desktop Linux/arm64 environment.
- Redis 7.4.11 image:
  `sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c`.
- Canonical COMMIT source remains byte-identical to its reviewed artifact; normal
  `SCRIPT LOAD` and `EVALSHA`, unchanged configuration, clocks, allocator and AOF
  policy remain mandatory.
- The existing executor never gains host/Docker sockets, a host PID namespace,
  tracing permission, or arbitrary debugger/command inputs.
- Traced fault cases are separate from untraced end-to-end latency samples.
  Instrumentation overhead must be recorded and cannot satisfy the p99 gate.

The Redis executable digest/build identity, matching symbol/address map,
observer image, exact UID/capabilities/seccomp profile, ARM64 stop precision and
memory/timing bounds are **not established yet**. Missing values prevent creation
of an executable approval packet; they are not substituted with invented hashes.

## 2. Local held-boundary observer candidate

### 2.1 Separation and confinement

Propose a distinct test-only observer container, recorded in intent and cleanup,
which can see only the owned Redis container's PID namespace. It has no network,
ports, host mounts, Docker socket, Redis credentials or writable Redis data mount.
Its root filesystem is read-only, with bounded memory/CPU/PIDs and fixed program
arguments. The controller alone creates it and selects the owned target.

Prefer non-root operation and the minimum demonstrated tracing rights. Any
required `SYS_PTRACE`, non-default seccomp allowance, UID change or PID-namespace
sharing is a **new case-specific authority decision**, not an extension of the
current executor's privileges. Do not use `--privileged`, `--pid=host`, blanket
`seccomp=unconfined`, daemon-wide policy changes or silent privilege fallback.
If the confined configuration cannot work on this target, record feasibility
failure and return for a target/design decision.

The observer program accepts only a closed approved boundary identifier and an
exact hash-bound address manifest. It must not accept arbitrary addresses,
debugger expressions, process IDs, paths, shell commands or memory-write requests.
Target container ID, main process identity/start time, executable digest, Redis
run ID and case/invocation binding are cross-checked before attachment and after
any restart. Reused PID/name alone is not an identity.

Reading Redis memory can expose private fixture material even without Redis
credentials. Comparisons remain private; output is a bounded allowlisted receipt.
No raw stack, memory dump, source/request bytes, lease owner/token or argument log
is exported. Observer/controller failures invalidate the fault-case evidence and
must leave a named, owned cleanup obligation.

The observer is a security-critical trusted component; the absence of Redis
credentials does not make its observation authority harmless or inherently
read-only. The receipt allowlist applies to normal output, errors and automatic
diagnostics. Private diagnostic retention requires a preapproved destination,
contents, access, size, lifetime and disposal rule; it must not preserve replayable
fixture state or reusable credential authority.

### 2.2 Boundary proof

The candidate must stop and hold the actual Redis execution thread at an exact
approved boundary before the controller injects the fault. An asynchronously
delivered event followed by a kill is insufficient.

Required observation bindings:

1. Exact target executable/image/source and case/request identity.
2. Script SHA and Lua prototype/program-counter/source mapping.
3. For the canonical mutation loop: `execution.count`, command ordinal and a
   privately compared command digest. One shared Lua source line is insufficient.
4. Stop reason and evidence that the relevant operation has not begun, or that
   the preceding operation completed, as the boundary definition requires.
5. Monotonic receipt sequence, controller fault dispatch and actual process
   termination; the exact client session/invocation's acknowledgment state is
   tracked independently as defined below.
6. Same-volume restart identity and startup outcome. Successful restart requires
   the complete case-specific post-state oracle and BOOT disposition. For a
   specifically permitted, evidenced startup refusal, post-state is unavailable,
   not empty/unchanged/complete, and no new BOOT approval is inferred. Missing
   post-state proof fails every assertion that requires it.

Acknowledgment classification is closed and bound to the exact tested invocation,
client session and case:

- **`known_pre_acknowledgment`:** affirmative, independently bound evidence
  establishes that the client had not received that invocation's relevant
  protocol acknowledgment at the fault boundary.
- **`acknowledged`:** evidence establishes receipt of that acknowledgment.
- **`unknown`:** evidence is missing, insufficient or contradictory.

A missing acknowledgment record is not proof of nonreceipt. The classification
covers acknowledged state-changing outcomes, including the first permitted
`DOWNSTREAM_BACKPRESSURE` bookkeeping mutation; it is not determined solely by
whether the status is `COMMITTED`. Earlier fixture probe/BOOT/setup acknowledgments
are separate invocations and are not silently conflated with the tested one.
Only `known_pre_acknowledgment` can be eligible for the proposed pre-acknowledgment
teardown alternative. Unknown state invalidates that acceptance route entirely.

Native hardware breakpoints/watchpoints are a candidate—not a demonstrated
method. Pure-Lua prevalidation requires verified interpreter/prototype mapping;
command-dispatch stops alone miss those checks. Software breakpoints, injected
Lua hooks, altered Redis binaries, forced flushes, debugger function calls and
changed instruction/register state are not implicit fallbacks. Any unavoidable
instrumentation change needs a separately reviewed equivalence decision.
That decision cannot waive canonical Lua or target Redis semantics: an approach
that changes either is outside this design's authority and must stop for a new
scope decision. Holding the execution thread does not establish that background
persistence, buffered reply delivery or the client is also held. Internal command
completion is not AOF persistence or client acknowledgment.

Stopping must be bounded so the observation does not silently enter a different
busy-script/timeout, client-deadline or expiry path. Use one selected fault point
per fresh case. Record pause/overhead limits in the exact recipe; stop precision
and failure behavior must pass calibration before testing Redis.

### 2.3 Feasibility gates

| Gate | Required output | Authority boundary |
|---|---|---|
| OBS0 | Static exact-image ELF/build/symbol inspection and a version-bound location manifest | Stopped image inspection only; no Redis or tracing |
| OBS1 | Demonstrated stop precision, lost-observer handling and bounded receipts on a purpose-built disposable calibration process | Separately reviewed observer image/configuration; explicit approval before tracing |
| OBS2 | Correlation and equivalence proof on a small canonical Redis operation | Fresh exact approval; no full COMMIT acceptance inferred |
| OBS3 | Frozen maximum-shape branch/boundary manifest and reviewed kill/restart/oracle choreography | New case-specific artifacts and execution decisions |
| OBS4 | Every required prevalidation/mutation/persistence/acknowledgment cut observed and reconciled | No random-kill or partial-inventory substitution |

The source-derived approximately 1,050-command maximum-count COMMIT branch is
only a starting inventory. Empty/existing-discovery/alias branches, backpressure,
receipt-only replay, pure-Lua checks and persistence/reply edges must remain
explicit. Maximum counts do not prove a combined maximum-byte/memory fixture.

## 3. AOF-startup-refusal teardown proposal

### 3.1 Preserve the ordinary rule

For every ordinary successful case and every failure with Redis still reachable:
quiesce all workers/observers, revoke every role, observe established-session
termination and failed reconnect while Redis is reachable, then destroy and
inspect exact resources. Missing proof is a failure. A reachable instance cannot
choose a weaker cleanup path for convenience.

After a fully acknowledged COMMIT, the tested process-crash model requires a
successful restart with the complete committed state. Startup refusal is not a
durability PASS merely because it is fail-closed.
The same distinction applies when durability of another acknowledged state-changing
outcome is tested, including permitted backpressure bookkeeping.

### 3.2 First investigate a compliant pre-fault retirement sequence

For a deliberately corrupted-AOF **startup-admission negative** that needs no
authenticated operation after the fault, investigate whether all credentials can
be retired and proved unusable while Redis is reachable *before* introducing
the declared fault, with the deny-all credential configuration durably preserved
through the later startup attempt. This must be reviewed for exact sequencing,
ACL persistence, session termination, allowed filesystem ownership and proof
binding; it is **not presumed compliant or implemented**.

This approach cannot simply be substituted for an internal COMMIT fault, where
the authenticated operation is still executing and subsequent restart may fail.
It does not resolve D02 generally and must not be used to invent a post-fault
authentication observation.

### 3.3 Proposed narrow alternative, only if a normative amendment is approved

For an explicitly approved disposable fault case whose **pre-acknowledgment**
outcome set permits fail-closed AOF startup, propose a distinct proof class:
**owned-resource credential extinction**. This is not observed online revocation.

Necessary conditions, all mandatory:

1. The exact case, fault boundary, permitted startup-refusal outcome and proof
   class are reviewed and approved before execution. No post-hoc conversion.
2. Evidence proves the declared fault and version-specific AOF startup refusal.
   Generic unreachability, an arbitrary crash, OOM, timeout, or unknown failure
   is not sufficient.
3. Affirmative, independently bound evidence establishes
   `known_pre_acknowledgment` for the exact tested client session/invocation.
   Missing receipt evidence is not proof of nonreceipt. Unknown or contradictory
   state makes this proof class ineligible, including for startup-refusal
   acceptance. Acknowledged state-changing outcomes, including permitted
   backpressure bookkeeping, are not treated as pre-acknowledgment merely because
   publication did not succeed.
4. The fixture used fresh, exclusive resources and per-case credentials, with no
   retained/production endpoint, data, credential or cross-case reuse.
5. Every owned worker, observer, Redis process and helper is stopped, waited and
   removed; exact container IDs/process identities and private transport
   disappearance are proven.
6. Every exact owned data/control volume and other authority-bearing fixture
   resource is destroyed and independently inspected absent. Foreign attachment
   or uncertain ownership prevents deletion/detachment of the affected resource;
   bounded cleanup continues for other independently verified owned resources
   where safe. Every unresolved resource remains a named cleanup obligation and
   invalidates acceptance. Removal success alone is insufficient: retain bounded,
   case-bound ownership and per-resource absence receipts; an inspection error
   is not evidence of absence.
7. No credential-bearing ACL/configuration or running identity is preserved as
   reusable authority. Any permitted diagnostic retention is privately bounded,
   identified and excluded from startup/reuse; its destination, contents, access,
   size, lifetime and disposal are preapproved. The inventory covers every
   helper/observer, transport and retained diagnostic artifact using demonstrated
   platform-supported identities. Public evidence remains redacted.
8. The report explicitly says online revocation/reconnect was **not observed**.
   The assertion may pass only for the predeclared startup-refusal outcome and
   destruction proof; it cannot attest zero acknowledged-write loss, complete
   COMMIT success, ordinary revocation or operational recovery.

This proposal concerns authority extinction in destroyed disposable resources,
not provable erasure of every credential byte from process memory. It grants no
permission to repair the AOF, start a replacement keyspace to manufacture an AUTH
result, reuse the volume, or broaden production/migration cleanup rules.

The fault classes and outcome obligations remain separate:

| Case/outcome | Required evidence and interpretation |
|---|---|
| Process-crash COMMIT, known pre-acknowledgment, successful startup | Complete branch-specific post-state proves no first commit or one complete commit; never an accepted subset |
| Process crash after acknowledgment of the tested mutation | Successful same-volume restart and complete acknowledged effects are required; startup refusal fails the durability assertion |
| Predeclared known-pre-acknowledgment AOF startup refusal | Exact fault/refusal evidence; post-state unavailable and no new BOOT approval; extinction proof only if the amendment is approved |
| Deliberate AOF-corruption startup-admission negative | Separate fault class and coverage; it cannot satisfy internal COMMIT process-crash atomicity/durability coverage |
| Unknown acknowledgment or unexplained startup failure | Acceptance invalid; bounded cleanup continues and unresolved obligations are retained |

An unmodified-AOF process-crash experiment must not introduce additional AOF
corruption between the observed fault and restart to obtain a refusal outcome.
An intentionally modified AOF belongs only to its separately declared corruption
test. A refusal report must not synthesize empty/unchanged/complete post-state or
successful BOOT evidence.

### 3.4 Proposed normative wording for review—not applied

The following would require explicit owner approval and corresponding changes to
both §5.1 and §17.7(9), plus evidence schemas and validators:

> For a reviewed disposable acceptance-fault case whose exact preapproved
> outcome set includes fail-closed AOF startup, and whose independently bound
> evidence positively establishes nonreceipt of the protocol acknowledgment for
> the exact tested client session/invocation at the fault boundary, an instance
> that cannot become reachable because of the evidenced AOF fault may use a
> separately reviewed owned-resource credential-extinction proof. Every case
> process, private transport and authority-bearing resource must be destroyed
> with independently verified ownership and per-resource absence receipts.
> Disputed ownership or foreign attachment MUST prevent the affected unsafe
> deletion/detachment, while bounded cleanup continues for other proven-owned
> resources; every unresolved obligation invalidates acceptance. A missing
> acknowledgment record is not proof of nonreceipt. Acknowledged state-changing
> outcomes, including permitted backpressure bookkeeping, MUST NOT be classified
> as pre-acknowledgment solely because they are not COMMITTED. The report MUST state that
> online revocation and reconnect denial were not observed, and MUST distinguish
> the permitted startup-refusal assertion from acknowledged-write durability and
> ordinary revocation. Post-state MUST be marked unavailable and no new BOOT
> approval inferred. Deliberate corruption tests MUST NOT substitute for process-
> crash atomicity/durability coverage, and an unmodified-AOF crash experiment MUST
> NOT introduce additional corruption before restart. This exception MUST NOT
> apply to an acknowledged or unknown tested-invocation acknowledgment state, an
> acknowledged-write durability test, a reachable instance, generic unreachability,
> unknown fault, retained data, or an operational deployment. Uncertain destruction
> invalidates the case. All other cases retain the normal reachable-server
> revocation and reconnect-proof requirements.

The wording is a proposal, not an instruction to weaken current validation.
If reviewers find a compliant lifecycle without a normative change, prefer that
and discard the exception proposal. If a change is approved, regenerate all
affected contract/source/bundle pins and reassess/rerun affected evidence before
claiming acceptance on the new identities. Existing accepted records remain
historical, byte-identical artifacts.

## 4. Closed evidence classes and negative controls

Do not make `revocation=verified` mean two different things. A future schema must
explicitly distinguish ordinary online proof, a proposed expected-refusal
extinction proof, and unproven teardown. The current PASS/FAIL schema is not
silently extended. Case/outcome eligibility, acknowledgment state and method are
validated together before any acceptance claim.

Required independent controls include wrong case/outcome/boundary, unknown or
acknowledged-success state, reachable Redis with skipped revocation, generic
startup failure, wrong image/process identity, missing or reordered observations,
observer death, partial journal, reused/foreign resources, persistent helper,
surviving private transport, incomplete destruction, diagnostic leakage and any
attempt to relabel the alternative as online proof. Every such mismatch fails.

## 5. Decisions still required

- Independent design review, then owner decision on any exact normative wording.
- An actually feasible observer UID/capability/PID/seccomp configuration and
  exact-binary mapping, independently reviewed before an execution request.
- Registered case-specific process/evidence/cleanup contracts and implementation.
- Fresh image/source/CI review and exact-artifact execution approvals.

The owner has selected the **design direction only**. Neither proposal is an
implemented or accepted full-M4 measurement method.
