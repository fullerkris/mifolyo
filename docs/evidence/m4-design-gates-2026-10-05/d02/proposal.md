# D02: acknowledgment, startup refusal, and teardown

**2026-10-05 — REVIEW-READY RESEARCH/PROPOSAL; NOT AUTHORIZED, IMPLEMENTED, OR RUNTIME-PROVEN.**

This is a private D02 intake record, not a protocol amendment or execution
approval. The existing draft has GO for proposal intake only. Its amendment is
unapproved/unapplied. No normative document, runtime validator, worktree, image,
or published Git artifact was changed. No Docker/container/Redis process, tracing,
fault injection, image build/pull, or fixture execution was performed. Research
used read-only worktree inspection and public Redis source/document retrieval.
D01 owns binary mapping and observer calibration; this record supplies the D02
receipt interface, not an independent claim that D01's method works.

## 1. Decision in brief

1. **Keep the current protocol and PASS gate unchanged now.** A failed AOF load
   can be a permitted fail-closed *outcome*, but destruction is neither proof of
   live credential revocation nor proof of recovered atomic/durable state.
2. **There are limited no-amendment routes.** Ordinary same-volume successful
   restarts with full state checks and online teardown remain eligible. A
   separate deliberate-corruption startup-admission negative is also a feasible
   *design candidate*: durably install deny-all startup ACL bytes while the old
   server remains reachable, then actually retire every live role, including
   the revoker, and prove disconnect/reconnect denial **before** the fault. This
   requires a new reviewed lifecycle and owner agreement that the prefault
   retirement satisfies the existing teardown sequencing; it is not implemented.
   It cannot cover an authenticated invocation killed inside COMMIT.
3. **The current ACL lifecycle is insufficient for either new restart recipe.**
   `DELUSER` changes live ACLs, not `fixture.acl`. The original file regrants
   setup/loader/BOOT/other roles at startup. Current cases restart before early
   retirement, so this is a future-case design gap, not evidence that their
   existing measured sequences already restart after retirement.
4. **If unreachable, genuinely known-pre-acknowledgment COMMIT refusal outcomes
   must be admitted as evidence, a narrowly confined cleanup exception is still
   needed.** Section 8 proposes one for unmodified-AOF process-crash COMMIT cases
   only, narrower than the old draft. Reject it if the owner instead chooses to
   fail and retain failure records for every such refusal. It never waives an
   acknowledged-write assertion, supplies post-state, or approves BOOT.
5. Redis 7.4.11 source proves useful distinctions, not target-image execution:
   ACLs load **before listeners and AOF**, listeners precede AOF loading, and
   loading can service AUTH/ACL commands. A socket, `WRONGPASS`, `LOADING`, or
   exit code alone does not prove either completed startup or AOF-caused refusal.
   Some forms of data loss (valid record deletion, empty files, missing manifest)
   may load without a corruption error. The complete oracle remains mandatory.

## 2. Verified input identities and normative constraints

Base worktree:
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-parallel-review-2026-10-05/design-worktree`

`git rev-parse HEAD` returned `e80d00538b11c46d021427bcfe1c408e16071c68`;
`git status --short` was empty at inspection. SHA-256 values were independently
computed, not copied as an assertion of verification:

| ID | Worktree-relative input | SHA-256 |
|---|---|---|
| L1 | `docs/crawl-jobs-v2.md` | `835e98db86e4d0cba224bc9fb9c3a3408514e4730d832773b7448f9b238a69c9` |
| L2 | `docs/crawl-jobs-v2-m4-observer-and-failure-design-2026-09-25.md` | `f6568eb6b322fa84fc9164d1b616d983a4f71cdc1dc85d42bccc4cb59b1479ae` |
| L3 | `tests/crawl-jobs-v2-redis/controller.py` | `7f7054882771d8163e8b3200be6107d9f304154f878e244cdeae865433d67f3e` |
| L4 | `tests/crawl-jobs-v2-redis/executor.py` | `329148ae49ee6ce776d811ab5688fe919537088db1168595a2471b7ccfc5a24f` |
| L5 | `tests/crawl-jobs-v2-redis/runtime_case.py` | `d8121103f8d45567e5463663630c8545d80fa12f6e4a8630fa2d8a3c63f8c114` |
| L6 | `tests/crawl-jobs-v2-redis/redis.conf` | `40dd75d86bd6edacfa64fb832c46652151030c06066856007bfdb7d7854522fd` |

Fixed target: Redis **7.4.11**, Linux/arm64, standalone, image
`sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c`.
The image was not inspected or run here; exact executable/source equivalence is
D01's unresolved input. L6 preserves `appendonly yes`, `appendfsync always`,
`aof-use-rdb-preamble yes`, `aof-load-truncated no`,
`no-appendfsync-on-rewrite no`, noeviction and the existing resource/configuration
limits. No proposed route relaxes them.

Normative ownership (L1):

- §§2.2/2.3, lines 272–358: lost response is ambiguous; tested acknowledged
  request/rate loss is zero; persistence/configuration/manifest admission and
  truncated-AOF warnings matter; PING is not readiness; changed run ID means
  effective unapproved BOOT despite a persisted `approved` field.
- §5.1, lines 823–975: fresh exclusive disposable resources; unchanged canonical
  operations; exact approvals; complete inventory; setup never regains access
  during measurement; real preliminary acknowledged-write rehearsal; later BOOT
  follows §2.3; all roles/sessions revoked with locally reachable failed reconnect
  before resource destruction. Teardown failure invalidates acceptance.
- `CJ2_COMMIT`, lines 3392–3525: all publication effects are one complete
  transition; first backpressure is a real bounded atomic durable mutation;
  refusal is one possible fail-closed fault outcome, **not** a successful
  post-acknowledgment zero-loss result. A post-ack fault requires successful
  restart with the complete transition.
- §17.2, lines 4615–4639, and §17.7(8–10), lines 4865–4881: every required
  prevalidation/mutation/acknowledgment boundary, independent oracle, explicit
  teardown and later assembly gates. No partial coverage becomes full M4.

## 3. Exact current behavior: what must not be inferred

### 3.1 ACL files, live retirement, and restart

- L4:234–252 creates `redis.conf` and `fixture.acl` once, mode 0600, UID/GID
  65534, in a 0700 control volume. It writes/closes the files but records **no
  file/directory fsync receipt**. L5:308–314 puts the disabled passwordless-denied
  default plus every role's hashed password and original grants in `fixture.acl`.
- L5:75–79 places revoker last. L4:204–231 authenticates a revoker, opens a held
  session for each still-valid target, issues `ACL DELUSER`, accepts count 0/1,
  observes held-session EOF/reset, then requires fresh AUTH to return `WRONGPASS`.
  An already revoked role must still yield `WRONGPASS`, not a transport error.
  The final target is revoker itself; its additional held session and reconnect
  are tested after self-deletion. This is real live proof, not persisted policy.
- L5:120–125 requires exact per-role `{reconnect,server_reachable,held_session}`:
  denied, true, terminated/already_revoked. There is no extinction alternative.
- Every revoker ACL is only `+ping +acl|deluser` (L5:183–260). No role grants
  `ACL SAVE`, `ACL LOAD`, or `ACL SETUSER`; there is no save after retirement.
  Redis's AOF/fsync policy does **not** make these ACL deletions persistent.
- L3:194–208 mounts control read-only into executor/revocation workers, writable
  into init/Redis; only init/Redis mount data. Executor cannot implement an
  ACL-file replacement without new reviewed authority. The init container is
  removed before Redis starts (L3:715–729); no existing approved writer helper
  provides the proposed lifecycle.
- L3:730–742 does ready/probe, KILL, start the **same** Redis container/volume,
  ready/resume. L4:269–332 verifies changed run ID and exact persistent probe,
  removes probe, loads scripts, approves BOOT, installs fixture, then retires early
  roles. No post-retirement Redis restart exists in this controller sequence.
- L4:395–404 authenticates **setup** for `ready`, retries TransportError only,
  and requires configuration/persistence readiness. It is unsuitable for a
  deny-all startup-admission test or a future measurement restart that must not
  regain setup. Postfault loading and read-only state checks need their own
  preapproved role/lifecycle, not a call to this setup-based ready stage.

### 3.2 Cleanup, failures, and missing refusal evidence

- `redis_started` is set before create/start to avoid skipping revocation on a
  lost start reply (L3:727–729). A failed startup therefore still attempts normal
  teardown, not a retroactive `server_never_started` success.
- L3:834–903 grants a separate 60-second cleanup budget even after approval
  expires; it quiesces/waits/removes executor(s), creates a **fresh** revocation
  helper, attempts every role's online proof, then removes resources. Failure
  sets revocation `not_proven` and continues bounded disposal. PASS requires
  `case_passed`, verified revocation, verified removal and intact journaling.
- L3:438–456 has stop/wait/PID-zero/removal proof for quiesced workers. Generic
  cleanup (481–506) checks ownership labels, refuses attached-volume removal,
  continues other resources, and separately inspects absence. Its generic
  resource records are principally names/labels; they are not the complete
  exact-ID/process/private-transport extinction receipts proposed below.
- L3:303–313 distinguishes a specific no-such-resource inspection from a daemon
  or permission error. `rm` success, an inspection failure, or killing a CLI is
  not proof of resource absence or worker death.
- Container logging is disabled (`--log-driver none`, L3:326–340); command errors
  are allowlisted/redacted (L3:88–102, 297–301). The current controller has no
  bounded version-specific AOF-refusal receipt. Exit 1, timeout, generic stage
  failure, or the disappearance of the socket cannot fill that gap. A future
  diagnostic channel needs independent security review: manifest errors can
  echo a source line and unknown-command errors can echo AOF bytes.

## 4. Version-pinned source findings

Public Git ref lookup resolved `refs/tags/7.4.11` to commit
`aaf0ce63b3239f4b51f86ca1da8711b055721993` (no peeled tag entry).
GitHub's commit API independently returned that commit, tree
`9e201597f552b0b66c77a1e82152168497edd26b`, message `Redis 7.4.11`, author date
`2026-08-17T06:04:23Z`, committer date `2026-08-17T13:28:51Z`.
`src/version.h` says `7.4.11` / `0x0007040b`. Immutable upstream source is not
proof that the unchanged target image contains a matching build.

For S1–S11 below, use this immutable URL prefix plus the path and line fragment:
`https://github.com/redis/redis/blob/aaf0ce63b3239f4b51f86ca1da8711b055721993/`.
Hashes are of raw bytes fetched from the same commit under
`https://raw.githubusercontent.com/redis/redis/aaf0ce63b3239f4b51f86ca1da8711b055721993/`.

| ID | Path; decisive lines | Raw SHA-256 |
|---|---|---|
| S1 | `src/acl.c`; 471–491, 1407–1414, 2272–2282, 2406–2462, 2469–2576, 2873–2898, 2978–2998 | `b4eecbb04562fa100488ad7be714d04ec7d3e3c8d8d0f76684bbcadf6aaf9cc4` |
| S2 | `src/aof.c`; 213–366, 694–755, 1007–1246, 1382–1756 | `e2bb132463edee1a673ca3fd2bd397eb0f7138d9f49c241ffd44212e54dc40a2` |
| S3 | `src/server.c`; 1733–1757, 6631–6638, 7179–7214 | `cc9adb1d9307719a9ddb36b1728107fff56da9306d89eabfbdad1e700ea49392` |
| S4 | `src/config.c`; 1408–1436, 3119 | `8f1f6b0b27ca4753b75b41ba2b74529944373d49e72ff963090fff28f49af1af` |
| S5 | `src/networking.c`; 1585–1595 | `0ed207f003c35589f97e6375bcf589c5fb22a1f905b3c2e9ec6a08d67984893c` |
| S6 | `src/commands/acl-deluser.json`; 3–15 | `c0645a047f085276ddb5ddca1a9d874828ae0480b9d16ebd75c592250daeed66` |
| S7 | `src/commands/acl-save.json`; 3–15 | `e6cba91cf5b83466706610ddb000a5b6cc138af73d2e3ed05a72cfb491fc6dd7` |
| S8 | `src/commands/auth.json`; 15–22 | `41afc0dd25fcb1fb794f4875cd0b6b311b1eaf2999f7ba10d0fff215c25053e2` |
| S9 | `redis.conf`; 1029–1048, 1433–1458, 1501–1528 | `528f53f3ffc6255bbc8a5281d5977e7aaad164233db7cf6b7896f096115fe900` |
| S10 | `src/rdb.c`; 3324–3355, 3688–3697 | `dbda3f767eefb868d258f822783a7aa4b43287ae0ae31af8d56a6b264601f133` |
| S11 | `src/version.h`; 1–2 | `9fa5ed92da961ec8030876760e89881670ebd2370add4f77ae582eaabb103a24` |

Findings supported by those exact sources:

**ACL persistence.** S1's DELUSER branch removes users and closes their clients;
it does not call `ACLSaveToFile`. The latter writes the complete current user
table, syncs the temporary file, renames it, and syncs the parent directory
(2517–2533). In 7.4.11 it is incorrect to claim ACL SAVE never fsyncs. It is also
incorrect to claim this harness invokes it. S4 skips rewriting inline users
when an external `aclfile` is configured; CONFIG REWRITE is not ACL SAVE.
S5 allows the self-deleting caller's command reply before closing that client;
other user sessions close asynchronously and require actual observations.
Deleting the final revoker and then attempting ACL SAVE cannot be assumed to
work: there is no remaining authenticated administrative authority. Saving
before deleting it persists that revoker. Do not solve this with unreviewed
pipelines, transactions, new wildcard admins, or a persistent last credential.

**Deny-all must be explicit.** S1:2406–2418 creates a default user if absent;
S1:1407–1414 defines that default as `on nopass +@all ~* &*`. An empty ACL file,
or a file merely omitting all named users, is **not** deny-all. A fixed file must
explicitly keep default disabled, clear passwords/keys/channels/selectors and
deny commands. Merely setting `off` in live memory does not terminate existing
authenticated sessions (S1's ACLSetUser documentation; D3 below).

**Startup order and reachability.** S3:7191–7203 loads ACLs, initializes
listeners, loads the AOF manifest and dataset, then opens AOF for appending;
readiness logging follows. S2:1464–1470 services clients during loading. AUTH,
ACL DELUSER and ACL SAVE have LOADING flags (S6–S8). Thus temporary AUTH or even
real retirement during loading is source-permitted, not guaranteed. A bind,
successful socket connect, `WRONGPASS`, or `LOADING` response does not establish
full AOF load, protocol readiness, or a whole-state oracle. AOF refusal does not
prove no credential was ever regranted during the startup attempt.

**Detected damage versus unavailable storage.** With truncation recovery off,
incomplete RESP reads and a still-open MULTI at EOF produce AOF_FAILED
(S2:1565–1623); loadDataFromDisk exits on AOF_FAILED/AOF_OPEN_ERR (S3:6633–6636).
Manifest syntax/order errors exit in its parser; a file referenced by a manifest
but missing on disk fails (S2:1672–1677). RDB base load errors also fail; checksum
damage must actually trigger checking, since a zero stored checksum is explicitly
treated as disabled (S10). Generic I/O permission/resource failures can use a
similar exit path without proving deliberate or crash-caused AOF damage.

**Detection has limits.** S2 ignores an absent AOF directory/manifest in its
legacy/empty-start logic; no entries can produce AOF_NOT_EXIST; total size zero
can produce AOF_EMPTY. S3 does not exit for those results, and S2's startup-open
logic can create an initial AOF. Removing complete syntactically valid records,
or changing data to another syntactically valid command/value, need not trigger
a parser failure. `aof-load-truncated no` is not a general corruption checksum.
Do not infer that every file deletion or every byte edit must cause refusal.

**Acknowledgment is a client fact.** S3 flushes AOF before pending replies;
S2's always-fsync path exits on write/sync errors. Those facts support the
durability expectation, not an observation that a particular client received a
reply. A held execution boundary, command completion, buffered reply, or missing
client log does not independently establish receipt/nonreceipt. Source comments
about typical single-write/process-crash behavior (S9:1501–1506) are not a
proof that every arm64/Docker volume cut will restart successfully.

Human-facing corroboration (retrieved 2026-10-05; moving documentation, not the
version authority):

- D1: <https://redis.io/docs/latest/commands/acl-deluser/> — removes users and
  terminates authenticated connections; default cannot be deleted.
- D2: <https://redis.io/docs/latest/commands/acl-save/> — writes current in-memory
  ACLs to configured external file; an error is possible.
- D3: <https://redis.io/docs/latest/operate/oss_and_stack/management/security/acl/#use-an-external-acl-file>
  — external ACL LOAD/SAVE and CONFIG REWRITE distinction. Its newer-version
  commentary is not adopted for 7.4.11; e.g. do not assume ACL-file comments work.

## 5. Closed classifications and applicable assertions

### 5.1 Acknowledgment binding (not a status-code shortcut)

Use exactly `known_pre_acknowledgment`, `acknowledged`, or `unknown` for a tested
Redis invocation. Bind case/fixture, client process/start identity, connection
generation/session, Redis process/run identity, immutable request identity,
attempt number, protocol response shape and independently observed event order.
Retries on another connection are different invocations of possibly the same
idempotent transition; keep both identities. An earlier acknowledged transition
cannot be relabeled unacknowledged merely by selecting its unacknowledged replay.

- `known_pre_acknowledgment`: affirmative independent evidence that the exact
  session had not received the relevant complete protocol reply at the effective
  fault boundary. A verified pre-reply causal stop may supply evidence only if
  it excludes earlier execution/replay and pending delivery for that invocation.
- `acknowledged`: an actual fully received relevant protocol reply is established,
  including the first durable DOWNSTREAM_BACKPRESSURE outcome, not only COMMITTED.
  A socket write completion, server-side return value or controller dispatch ACK
  is not this acknowledgment.
- `unknown`: missing, truncated, contradictory, unbound or unorderable evidence.
  No log entry means unknown, never affirmative nonreceipt.

Record the held boundary, kill dispatch, actual process-stop interval, client
receive events and final sealed client retirement separately. A prefault
classification must cover the gap from hold to effective kill. For the proposed
exception, a later complete buffered reply before client retirement disqualifies
the route conservatively, even if the original cut preceded receipt; keep that
timing fact rather than rewriting history. Missing closure of that interval is
unknown. D01 must supply its boundary witness; D02 still needs an independent
client receipt/closure witness.

Maintain a separate **acknowledged-effect ledger** for probe, probe deletion,
BOOT, direct setup, canonical fixture preparation, prior publication/backpressure,
and each measured invocation. Each row states its identity, receipt, expected
effects, permitted subsequent mutations/expiries and which fault/restore assertion
it belongs to. Prior fixture ACKs neither make the tested invocation acknowledged
nor disappear from durability accounting. A prior successful probe establishes
only its own measured restart, not the survival of later writes at the target cut.

A standalone filesystem-corruption admission test may have **no tested Redis
invocation**: use `tested_invocation=null`, with a reason and an intact fixture
ACK ledger. Do not invent a pre-acknowledgment invocation for the corruption
syscall. If it does select an invocation, the same three-state rules apply; it
still cannot masquerade as unmodified-AOF process-crash evidence.

### 5.2 Fault classes (closed families, exact recipes still required)

| ID | Concrete class | Required distinction / coverage |
|---|---|---|
| P1 | Redis SIGKILL before dispatch or at an independently held prevalidation cut | Same unedited volume; selected cut may require specifically unchanged state, not the entire broad none-or-complete outcome set. |
| P2 | Redis SIGKILL at a selected mutation-command or propagation/persistence/reply cut before receipt | Whole branch-specific transition or no such transition on loaded restart; a detected-damage refusal is separate. D01 supplies exact cuts, not this record. |
| P3 | Redis SIGKILL after tested mutation acknowledgment | Successful same-volume restart with complete acknowledged effects is mandatory; refusal never passes zero-loss. |
| P4 | Client loss alone, Redis remains alive | Not Redis process-crash coverage. Reconcile exact identity under valid BOOT and ordinary online teardown; client death need not stop an executing script. |
| C1 | Deliberately cut final incremental AOF inside a RESP record | Exact file/offset and old/new digests; source-matched unexpected EOF, not arbitrary timeout. |
| C2 | Deliberately remove EXEC so valid commands end inside MULTI | Incomplete-transaction detection even at a RESP boundary; no accepted rollback/repaired tail. |
| C3 | Deliberately break a known RESP command introducer or use a bounded known-invalid record | Source-matched format/unknown-command error; no arbitrary oversized lengths, code or unbounded parse input. |
| C4 | Deliberately make a bounded manifest grammar/order violation | Exact parser error, e.g. duplicate base/nonmonotonic incremental sequence; distinguish missing manifest, which is different. |
| C5 | Deliberately remove one exact manifest-referenced file | Manifest still present and bound; distinguish missing entire manifest/wrong mount/empty volume. |
| C6 | Deliberately invalidate a base RDB checksum | Requires checksum-enabled/nonzero-checksum provenance and exact error; not every RDB edit is this case. |
| C7 | Remove whole valid transactions, zero a file, lose manifest/directory, or make a valid-value edit | May load successfully. Must fail any violated manifest/state/durability assertion; not an expected-refusal recipe without further proof. |
| G1 | Invalid configuration/ACL, missing or unreadable ACL, socket bind/permissions, wrong UID | Not AOF-damage acceptance, even with exit 1 or AUTH failure. |
| G2 | Wrong/reused volume or process/image, Redis never actually started, stale socket | Identity/admission failure, not a recovered empty state or expected refusal. |
| G3 | OOM/exit 137, resource exhaustion, unrelated signal/assertion/observer death | Not the intended SIGKILL/AOF refusal without exact independent causal evidence. |
| G4 | AOF open/write/fsync I/O error, EACCES/ENOSPC/EIO without the declared damage | Storage/startup failure, not this corruption-negative PASS. A separate fault model would need its own approval. |
| G5 | Deadline/timeout, still loading, hung/busy process, daemon/inspection outage | Unknown startup outcome; bounded disposal, no refusal inference. |

C1–C6 are research candidates, not registered/approved executable recipes. Choose
one exact fault per new fixture. Never edit an AOF after P1/P2/P3 to obtain C1–C6.

### 5.3 Reachability, state, BOOT, cleanup and assertion vocabulary

Keep these independent:

- `redis_load`: `loaded`, `evidenced_aof_refusal`, `other_startup_failure`,
  `unknown`. `loaded` requires same-volume manifest/configuration/persistence
  admission, not merely a live listener. A completed bad-state load is still
  loaded, but its oracle fails.
- `teardown_access`: `online_available`, `not_available`, `unknown`, plus a
  complete history of any actual online retirement. It means the exact local
  Redis can service the relevant AUTH/ACL proof, not merely accept a socket.
- `post_state`: complete independently verified branch oracle, failed oracle,
  or `unavailable`. A refusal/unfinished load never becomes empty/unchanged/full.
- `boot`: actual stored record and actual current run ID only if observed;
  effective unapproved on mismatch; any later BOOT invocation/evidence is
  separate. On refusal use `not_approved_after_fault`, no synthetic new run ID,
  no new BOOT success, and stored/recovered state unavailable.
- `cleanup_proof`: `online_revocation`, proposed
  `prefault_online_retirement_persisted`, proposed
  `owned_resource_credential_extinction`, or `unproven`. The prefault name is a
  timing/persistence specialization of actual online proof, **not** a new weaker
  substitute. Existing validators recognize neither new label.
- Assertions are separate: declared fault observed; startup-admission negative;
  allowed fault-safety outcome; recovered-state atomicity; acknowledged-effect
  durability; BOOT conformance; credential retirement; exact resource disposal.
  Overall M4/release acceptance is never computed from cleanup alone.

Branch-specific complete oracles:

| Branch | Restart obligation |
|---|---|
| First publication | Every §10.5 COMMIT effect, exact payloads/TTLs, empty/nonempty outlinks, manifest/images, backlinks, new versus existing discovery, aliases, one notification, completed job, all counters/indexes/lease/stage-slot/residual expiry changes. No accepted subset. |
| First backpressure | No consumer-visible output; exact fence/first reason/start/deadline, matching index score, slot-control G deduction and retained terminal floor all agree. Acknowledgment requires this complete durable bookkeeping; restart cannot reset the deadline. |
| Backpressure replay | No new allocation/deduction; preserve prior durable first record and identity. Track the original acknowledged mutation separately. |
| ALREADY_COMMITTED / no-write rejection | No new mutation or recreated output. Check the branch's permitted retained state, not a fabricated second publication. Replay after legitimate output cleanup may not demand now-deleted payloads; prior publication evidence remains separately scoped. |

Time/expiry must be bounded and modeled explicitly. A recovered snapshot is not
byte-for-byte equal to a precrash snapshot if authorized expiry intervened; nor
may an expired stage justify missing persistent output, reset counters or erased
durable first-backpressure evidence. No post-restart mutating repair or BOOT write
may hide the pre-recovery state being asserted.

### 5.4 Decision matrix (AND with exact case outcome set, never post-hoc)

`K/A/U` denote the three tested-invocation states. `N` means no Redis invocation
was selected, not a fourth acknowledgment state. `ONLINE` requires actual all-role
session/reconnect proof, quiescence and verified disposal. `PRE` is the fully
proved §6 candidate. `EXT` is §8's currently unauthorized cleanup-only proposal.
For every row, missing ownership/quiescence/absence/journal evidence invalidates
cleanup; continue safe bounded disposal regardless of assertion failure.

| Row | Fault / ACK / result | State and durability outcome | BOOT | Cleanup and acceptance |
|---|---|---|---|---|
| M01 | P1/P2, K, loaded + online | Exact cut-allowed no-transition or complete branch; preserve separately scoped prior acknowledged effects. | Changed run ID effectively unapproved; later approval separate. | ONLINE. Eligible only when all scoped assertions pass. No amendment. |
| M02 | P3 or other process cut, A, loaded + online | Complete acknowledged effects required, including first backpressure. Missing/partial effects fail. | Same rule. | ONLINE. No extinction. |
| M03 | Process cut, U, loaded + online | Record complete oracle if obtainable; it cannot retroactively determine receipt. A zero-loss timing claim is unproven; D02 acceptance invalid pending proper new evidence. | Same rule. | ONLINE still required; no extinction. |
| M04 | Process cut, any ACK, loaded but no usable teardown access | State assertion requires actual permitted complete read proof; cannot infer it from startup. | No automatic approval. | Cleanup unproven unless already completed actual online proof remains valid; denial by the wrong ACL alone is not retirement. No extinction. |
| M05 | P1/P2, K, exact approved AOF refusal, no online access | Allowed refusal facet only; recovered state unavailable. All assertions requiring recovered acknowledged effects remain unproven/fail. | No new approval. | Current rule: FAIL. EXT may satisfy cleanup only after explicit amendment/implementation approval and all E1–E9. |
| M06 | Process cut, A, AOF refusal | Zero-loss assertion FAIL regardless of whether refusal is fail-closed. | No new approval. | No EXT; safe disposal may succeed but cannot validate case evidence. |
| M07 | Process cut, U, AOF refusal | Receipt/timing classification unknown; refusal acceptance ineligible. | No new approval. | No EXT; bounded disposal only unless actual online proof existed. |
| M08 | Process cut, K, refusal with online access or completed real online proof during loading | Refusal facet still needs exact causal evidence; no recovered state. | No new approval. | ONLINE must be used when available; no weaker EXT choice. Completed same-server online proof can avoid amendment, but a loading window is not assumed. |
| M09 | Process cut, any ACK, other/unknown startup failure | Not accepted AOF refusal. State unavailable unless independently obtained from a completed load; post-ack durability not passed. | No new approval. | ONLINE if actually available, otherwise safe disposal/unproven retirement; never EXT. |
| M10 | C1–C6, N, proved PRE, evidenced refusal | Only separately approved startup-admission negative; no COMMIT/process-crash/acknowledged durability credit. Fixture ACK ledger preserved with no invented recovery result. | No new approval. | PRE + exact disposal: conditional no-amendment candidate after owner/lifecycle review. |
| M11 | C1–C6, K/A/U selected invocation, proved PRE, evidenced refusal | Same corruption-only limit. A/U cannot be relabeled K. Any requested acknowledged-write assertion fails/unproven; cannot pass it using corruption. | No new approval. | PRE may establish retirement independently of ACK; no EXT. Target-invocation claims assessed separately. |
| M12 | C1–C6, any/N, no PRE, refusal + online access/complete online proof | Only exact preapproved admission facet may pass. | No new approval. | ONLINE if complete; no amendment needed for cleanup. No reliance on an unobserved transient window. |
| M13 | C1–C6, any/N, no PRE, refusal without online proof/access | Admission diagnostic may be retained; case acceptance invalid under existing teardown. | No new approval. | Safe disposal only; proposed EXT deliberately excludes corruption. |
| M14 | Expected-refusal corruption, any/N, Redis finishes loading | Negative FAIL even if BOOT remains unapproved or AUTH is denied. If no read authority remains, state unavailable, not empty. | Unapproved is not AOF refusal. | PRE if genuinely proved, otherwise ONLINE; no EXT. Do not restore credentials to obtain state. |
| M15 | Corruption, any/N, generic/unknown failure | Negative FAIL/unproven; cannot attribute ACL/config/OOM/timeout failure to AOF damage. | No new approval. | PRE if already proved, else ONLINE if reachable, else unproven; safe disposal always. |
| M16 | C7, any/N, apparently loaded | Exact full oracle/manifest admission decides any declared diagnostic claim; missing acknowledged state fails. Does not become process-crash coverage. | No automatic approval. | PRE/ONLINE as applicable; no EXT. |
| M17 | G1–G5, any/N, reachable or unreachable | Harness/infrastructure failure, never accepted AOF refusal or zero-loss evidence. | None inferred. | Actual ONLINE/PRE facts may be retained; otherwise unproven retirement. Owned disposal does not rescue case. |
| M18 | P4 client-only loss, K or A, server available | Exact identity reconciliation and branch oracle; no Redis restart coverage from this alone. | Existing run/BOOT must still match; if restarted apply process/restart rows. | ONLINE; no EXT. |
| M19 | P4 client-only loss, U or unavailable server | Lost receipt remains unknown; investigate exact fault, no automatic reclassification. | None inferred. | ONLINE if recovered within original allowed lifecycle; else bounded disposal. No EXT for an unexplained server loss. |

Rows are guards, not a choice menu. Reject unmatched combinations and contradictory
states. For example, `evidenced_aof_refusal` plus `post_state=complete` or
`new_boot=approved` is invalid. Current runtime acceptance remains its existing
strict gate; proposed labels/rows are not installed validators.

## 6. No-amendment candidate: retire before a separate corruption negative

### 6.1 Exact proposed sequence, all before any deliberate AOF edit

1. Preapprove a **separate startup-admission-only case**, not an internal COMMIT
   boundary test. Use the exact image/configuration and normal real probe/BOOT
   where required by §5.1. Seal all bounded preparation/state/ACK evidence while
   reachable. Freeze the AOF inventory/manifest and ownership; no pending
   authenticated work may remain when retiring roles.
2. Quiesce/remove measurement workers/observers. A newly reviewed controller-owned
   fixed-function helper, not the ordinary executor, receives only the exact
   control-volume write authority needed to replace `/run/cj2/fixture.acl`.
   Prefer no Redis credentials, no data mount, no network and no arbitrary
   path/content arguments. Its existence/mounts/identity require new approval.
3. While the old Redis's in-memory ACL still permits the dedicated revoker,
   atomically persist this **fixed startup policy** (one LF; no comments):

   ```text
   user default off resetpass resetkeys resetchannels clearselectors -@all
   ```

   No named users, passwords, selectors, alternate ACL path, inline users,
   includes, startup generator or retained copy may reintroduce authority.
   Use a uniquely created no-follow regular file, mode 0600, expected UID/GID,
   bounded bytes, same-directory atomic replacement, successful file sync and
   directory sync, readback hash/metadata and independent ownership checks.
   Detect symlink/hardlink/replacement races; inspect exact file identity again
   before startup. Keep `redis.conf` itself byte-identical. Do not call ACL LOAD:
   the old live server must remain usable for actual retirement proof.
4. Remove/wait/inspect the file writer; exclude later rewrites/regrant. Run a fresh
   revocation helper against that **same still-running** server. Retire every
   role with held-session disconnect and fresh WRONGPASS; revoker is last.
   Independently retain pre-retirement session provenance for roles reported
   already_revoked. Prove default remains unusable, and bind negative AUTH to
   the original target rather than any replacement. Seal online proof and retire
   the helper. No more authenticated operation is needed.
5. Only after **both** durable startup-policy proof and complete online retirement
   are sealed may the controller stop/wait Redis, inventory exact stopped data,
   and allow the separately reviewed fixed corruption operation on one declared
   file/offset. A failure at any earlier step aborts this test. No fault may be
   injected in order to excuse incomplete retirement.
6. Attempt the single declared same-volume startup with unchanged image/config,
   sealed deny-all ACL and a new bounded startup-observation path, not setup-based
   `ready()`. Capture source-matched refusal + final process exit + identity/fault
   chain. If it unexpectedly loads, the negative fails; do not regrant any role
   to produce a post-state or manufacture a successful AUTH check.
7. Stop/wait/remove all helpers/Redis and destroy/inspect exact data/control/private
   transport resources. Report online retirement as **prefault**, postfault AUTH
   as unobserved unless genuinely observed, and recovered state/BOOT unavailable
   on refusal. The proved denied startup ACL prevents retirement from being
   undone by that later attempted boot; it is not an observed postfault AUTH.

This sequencing avoids the final-revoker ACL SAVE trap without adding Redis
administrative grants. Redis 7.4.11's real ACL SAVE is another possible building
block only after a separate design solves final-role retirement and file modes;
its temporary-file creation uses mode 0644 before process umask (S1:2499), unlike
the harness's explicit 0600. Merely adding `+acl|save` does not solve the lifecycle.

### 6.2 What must be proved/changed before calling it compliant

- Owner interpretation: prefault online retirement, preserved across the sole
  startup attempt and followed by verified destruction, meets §§5.1/17.7(9)
  without rewriting their required ordering. Until that decision, **candidate**,
  not certified no-amendment compliance.
- New closed recipe, helper/process admission, exact ACL-transition artifact,
  persistence/ownership receipt, reachability proof, source-bound refusal channel,
  stage deadlines and failure cleanup. Current validators must not simply have
  `revocation=verified` assigned from file inspection. No validator change now.
- All failure prefixes, including crash before/after rename/sync/last DELUSER,
  abort the negative; bounded safe disposal continues. The case cannot retroactively
  choose extinction. Same-volume restart checks on independently approved clean
  controls must prove denied credentials do not resurrect, including default.
  These controls use fresh separate fixtures, never a substitute server against
  the failed case to manufacture AUTH evidence.
- Positive isolation/control proves the deny-all file is valid on the exact image
  and does not itself cause startup refusal; unchanged healthy AOF startup under
  that policy must succeed as Redis load while remaining unauthenticated and
  protocol-unapproved. No Redis process was run to establish this here.
- Evidence must prove no AOF mutation by the policy writer and no policy mutation
  by the corruption helper; write scopes and same-volume identities are separate.

**Coverage limit:** only the exact corruption-admission branch exercised, not
COMMIT internal atomicity, client receipt timing, acknowledged-write durability,
availability/RTO, host/storage crash survival, or a full restore rehearsal. It
does not retire an authenticated COMMIT in flight. Stopping an execution thread
does not make the ordinary DELUSER command run inside that script.

### 6.3 Other limited no-amendment paths

- Predeclare that all process-cut cases require successful restart/whole-state
  proof and ordinary online teardown. If every required cut actually meets those
  stricter outcomes, no cleanup exception is needed; an actual refusal remains
  failed acceptance evidence. Do not delete failures or infer approval afterward.
- Complete genuine ordinary online proof during a source-permitted loading window
  can avoid an exception **if the originally approved lifecycle actually measures
  it**. It is not guaranteed, and delaying/repairing/replacing Redis to create the
  window is not authorized. Incomplete proof remains incomplete.
- Successful future internal-COMMIT restart recipes must durably retire setup
  before measurement, preserve only separately reviewed postfault observation/
  BOOT/loading roles, and never reuse setup-based ready. The current ACL file
  cannot be reused verbatim across that retirement boundary. Later BOOT or script
  reload capability needs an explicit lifecycle; neither regrant nor automatic
  approval is implicit. Read-only post-state may be collected with BOOT still
  effectively unapproved; replay mutations require the proper later approval.

## 7. Proposed receipt contracts and negative controls

These are **requirements for a future closed schema**, not runnable validators.
Use canonical UTF-8 JSON: sorted keys, compact separators, integer timestamps,
no floats/nonfinite numbers, duplicate keys rejected, one final LF. Every receipt
has domain/version, exact case/fixture/plan/approval/recipe/contract/source/image/
config bindings, issuer image/process/start identity, monotonic sequence, previous
receipt hash and bounded evidence references. Do not export raw ACLs/passwords,
request/token bytes, AOF records, memory, unbounded logs or replayable keyspace.
Hashes alone are integrity bindings, not evidence that an event occurred.

Proposed common envelope has exactly `domain`, `version`, `kind`, `bindings`,
`issuer`, `sequence`, `previous_receipt_sha256`, `payload` and `evidence_refs`.
Domain is `mifolyo:crawl:v2:m4-fault-receipt`; version is integer 1; kind is one
of R1–R9 below. SHA fields are nonzero lowercase 64-hex, not invented placeholders.
Sequence is a bounded nonnegative integer. Previous-receipt hash is null only
at sequence zero. References are unique registered digest/receipt identifiers,
not arbitrary paths or URLs. `bindings` has exactly `case_id`, `fixture_id`,
`plan_sha256`, `approval_sha256`, `recipe_sha256`, `contract_sha256`,
`source_set_sha256`, `redis_image`, `harness_image`, `redis_config_sha256`.
`issuer` binds a registered role, immutable image and the platform-supported
process/container start identity; its exact platform identity union remains a
joint D01/security decision, not a guessed host PID. Additional or duplicate
fields and undeclared identity variants reject. All array/string/byte/time limits
must be explicit in the hash-bound recipe before this becomes an executable
schema; current 300-second case, 30-second stage, 60-second cleanup and bounded
export rules are not relaxed by this proposal.

An assertion result has exactly `assertion_id`, `scope_id`, `result`,
`reason_code`, `evidence_refs`: result is `pass`, `fail`, `unproven` or
`not_applicable`; assertion/reason/scope IDs belong to the frozen case manifest.
`not_applicable` cannot hide a required assertion. Any required fail/unproven
result prevents its acceptance claim. Null means explicitly unavailable or
absent in an allowed tagged variant, never zero, empty state or success. Each R2
must carry exactly one of the three acknowledgment classifications; null R2 is
allowed only through R1's no-tested-invocation corruption-case variant. R4's
load/access enums and R5/R9's state/BOOT implications are those in §5.3, with
cross-receipt checks mandatory; a structural JSON parse is not validation.

| Receipt | Required fields / invariant |
|---|---|
| R1 `case_intent` | Exact assertion IDs, fault family/subtype, allowed boundary manifest digest from D01, permitted startup outcomes fixed **before** fault, tested invocation or explicit null, acknowledged-effect ledger digest, cleanup class, resource/process/transport/diagnostic inventory, observation and cleanup budgets. No class switch after failure. |
| R2 `invocation_ack` | Invocation/idempotency/attempt IDs; client/session/generation and Redis process/run bindings; request/response-schema digests; complete response observed and status/effect class; K/A/U with affirmative witness refs; held/fault-dispatch/process-stop/client-receive/seal order with integer uncertainty bounds and clock domains. Contradictions or missing closure => U; no response-log absence inference. |
| R3 `acknowledged_effect_ledger` | One row per actual acknowledged fixture/measured effect, including first backpressure; receipt refs, complete expected-effect oracle digest, subsequent authorized supersession/expiry, exact crash scope and recovery result `verified/failed/unavailable/not_tested_in_this_scope`. Do not equate a prior probe result with postfault recovery. |
| R4 `fault_and_restart` | Before/after exact container/process identity, unchanged image/config, exact volume creation/ownership identity and attachment inventory, AOF manifest/file identity+length+digest inventory, declared edits only for C cases, independent stop/wait evidence, restart attempt identity, loader result, source error enum+source SHA/range, bounded diagnostic digest, OOM/signal/timeout distinctions, local reachability intervals, actual run ID only if observed. File/disk hashes are not an invocation post-state oracle. |
| R5 `post_state_and_boot` | Complete expected inventory/schema/values/membership/counters/absolute-expiry checks and result per branch, independent observer identity, observed current/stored BOOT distinction, later canonical BOOT invocation receipt if any. On refusal: `post_state=unavailable`, no recovered BOOT record/new approval; no repaired state. All required missing assertions fail/unproven, never PASS. |
| R6 `online_retirement` | Complete role inventory, held authenticated sessions or prior retirement provenance, DELUSER outcome, peer EOF/reset, fresh full WRONGPASS response from exact local Redis, current process binding and event order; includes final revoker/default denial. Transport error/NOPERM/LOADING is not failed AUTH. Prefault proof records timing explicitly. |
| R7 `startup_acl_seal` | Exact prior/deny-all digest, path/volume/file identity, regular/no-follow/single-link metadata, mode/owner, atomic replace + file/dir sync/readback receipts, validated disabled default/no other grants, unchanged config path/no includes/generators, writer stop/removal, no later regrant. ACL-file validity positive control bound separately. No synthetic postfault AUTH field. |
| R8 `disposal` | Every named worker/observer/helper/Redis identity with stop/wait/removal/absence proof; exact data/control/other authority-bearing volume ownership + attachment checks and independent absence; private socket/transport disappearance; evidence/diagnostic retention exclusions, bounded private destination/access/size/expiry/disposal and no reusable authority. Resource ownership ambiguity/foreign attachment blocks affected deletion; other safe cleanup continues, unresolved obligations enumerated. Use platform-supported volume creation identity, not an invented Docker volume ID. |
| R9 `assertion_report` | Distinct results for startup refusal, recovered atomicity, each acknowledged-effect durability assertion, BOOT, online revocation, proposed extinction and disposal. Current-rule eligibility, proposed-rule eligibility and authorization are separate fields. `execution_authorized=false`, `implementation_proven=false`, `m4_accepted=false` in design records; future runtime values need new evidence. |

For EXT, R1/R2/R3/R4/R8/R9 are mandatory; R5 must explicitly mark unavailability
and absence of new approval, while R6 must explicitly say **online proof not
observed**, not disappear from the report. PRE requires R6+R7; neither replaces
the other. Any optional retained diagnostic is independently governed; default
is no retained raw fixture/ACL/AOF content. Extinction asserts disposal of owned
authority, not cryptographic erasure of every credential byte in host memory or
backing storage.

Minimum independent controls (all must reject the indicated invalid claim):

| IDs | Negative/control set |
|---|---|
| N01–N04 | Wrong session/generation/invocation/retry; borrowed probe or BOOT ACK; missing ACK log treated as K; contradictory/late reply or lost client journal hidden. |
| N05–N08 | COMMITTED versus first DOWNSTREAM_BACKPRESSURE misclassification; original acknowledged transition hidden behind replay; partial publication/bookkeeping accepted; old durable deadline reset on restart. |
| N09–N12 | Reachable server skips online proof; socket refusal/timeout/exit 1/137 treated as AOF damage; WRONGPASS from wrong/substitute server; loading AUTH mistaken for complete load/readiness. |
| N13–N16 | Raw original ACL resurrects roles after DELUSER; save-before-self-delete resurrects revoker; empty/missing-default ACL becomes permissive; `off` alone leaves held sessions. |
| N17–N20 | Wrong file/symlink/hardlink/foreign volume; unsynced/failed ACL replacement; ACL invalidity rather than AOF damage causes refusal; helper rewrites policy after seal or touches AOF outside its scope. |
| N21–N24 | Missing manifest or zero/valid-truncated record loss assumed detected; deliberate edit smuggled into process-crash case; config/fsync/truncation policy changed; `redis-check-aof --fix`, relaxed truncation or substitute Redis used to manufacture success. |
| N25–N28 | Wrong image/process/start/volume identity; missing/reordered/partial journal; observer/helper death with missing obligations; fake post-state/new BOOT on refusal. |
| N29–N32 | Surviving worker/helper/transport; foreign attachment or inspect error treated as absence; residual ACL/config/diagnostic reusable authority; cleanup-only success upgrades atomicity/durability/release. |
| N33–N36 | Outcome/exception approved after failure; A/U gets EXT; C/G/client-only failure gets EXT; hidden earlier acknowledged fixture effects receive blanket zero-loss credit. |

Positive controls are also necessary: exact healthy-image load, known all-role
live retirement including last revoker, deny-all valid-startup/no-regrant control,
whole successful publication and first-backpressure restart oracles, and correct
failure attribution for each approved corruption recipe. No such controls ran
in this research task.

## 8. Minimal conditional amendment proposal — UNAPPROVED / UNAPPLIED

Prefer §6 if its limited coverage is sufficient. If the owner needs M05 admitted,
the following changes only the **fixture teardown proof**, not §§2.2/2.3 or the
COMMIT atomicity/durability contract. The proposed exception deliberately excludes
deliberate corruption and generic startup failure; adding those would be a new
owner decision, not implicit reuse of this wording.

### 8.1 Proposed insertion after §5.1's ordinary teardown paragraph

> Only for an individually preapproved, fresh exclusive disposable CJ2_COMMIT
> Redis process-crash case, with no intentional AOF alteration, whose specified
> fault outcome permits detected-AOF-damage startup refusal and whose independently
> bound evidence affirmatively establishes `known_pre_acknowledgment` for the exact
> tested client session/invocation, an evidenced AOF refusal that prevents completion
> of locally reachable revocation MAY use the separately approved proof class
> `owned_resource_credential_extinction`. This substitutes only for the fixture's
> online teardown proof. All owned workers, observers, helpers, Redis processes,
> private transports, data/control volumes and other reusable authority-bearing
> resources MUST be quiesced and destroyed, with independently verified exact
> ownership and per-resource absence receipts. Disputed ownership or foreign
> attachment MUST block the affected unsafe deletion/detachment; bounded safe
> cleanup continues elsewhere, and every unresolved obligation invalidates
> acceptance. Retained diagnostics MUST have preapproved private bounds and
> disposal and MUST preserve no reusable fixture authority.
>
> The report MUST state that online revocation/reconnect denial was not observed,
> recovered post-state is unavailable, and no new BOOT is approved. Extinction
> does not establish the startup-refusal assertion, recovered-state atomicity,
> recovery, or durability; each retains its independent evidence requirements.
> Every acknowledged state-changing effect, including first backpressure
> bookkeeping and separately identified fixture effects, retains its complete
> durable-effect obligations. No assertion requiring successful recovery of such
> effects may pass on refusal. A missing acknowledgment log is not affirmative
> nonreceipt. Acknowledged or unknown tested-invocation states, deliberate AOF
> alteration, generic/unknown startup failure, an available ordinary online proof
> path, retained/shared resources and operational deployments are ineligible.
> Neither an allowed outcome nor this proof class may be selected after failure.
> No repair, replacement server, fabricated post-state or BOOT result is permitted.

### 8.2 Proposed addition to §17.7(9)

> The narrowly defined §5.1 credential-extinction alternative requires separate
> assertion/cleanup receipts and negative controls rejecting every ineligible
> fault, acknowledgment, reachability, ownership, resource, evidence and authority
> combination. It MUST NOT be reported as online revocation or used to pass any
> recovered-state, acknowledged-write or BOOT assertion whose proof is unavailable.
> All other cases retain the ordinary locally reachable teardown requirement.

### 8.3 Closed EXT eligibility — all E1–E9 must hold

| Predicate | Required value | Every other value |
|---|---|---|
| E1 Authority | Exact amendment, new recipe/schema/implementation and case-specific outcome/proof-class approval all precede execution | Ineligible; today's value is unapproved |
| E2 Fault | P1/P2, canonical CJ2_COMMIT, same AOF volume, no intentional data/AOF alteration or semantics change | C1–C7, G1–G5, client-only, other transition: ineligible |
| E3 Tested ACK | K, exact session/invocation and sealed independent client/held/fault timeline; no contradictory/later received reply admitted as K | A/U: ineligible |
| E4 Startup | Exact predeclared detected-AOF-damage fatal path on pinned version/image and exact restart identity | Timeout, OOM, ACL/config/I/O-only error, wrong identity or successful load: ineligible |
| E5 Reachability | No available ordinary all-role live proof, with actual reachability history; no skipped complete ordinary path | Reachable path/convenience fallback: ineligible |
| E6 Fixture | Fresh, exclusive, per-case credentials/resources; no production/retained/cross-case reuse | Shared/uncertain/reused: ineligible |
| E7 Disposal | Complete inventory, quiescence, safe ownership/attachment checks, independent absence and sealed journal; no unresolved resource | Any missing proof or residual authority: ineligible |
| E8 Reporting | Explicit unobserved online proof, unavailable recovered state, no new BOOT, acknowledgment ledger retained, scoped assertion results | Conflation/fake state/zero-loss credit: ineligible |
| E9 Claim boundary | Only disposal/credential-extinction plus independently proven predeclared refusal facet claimed; all unsatisfied durability/state requirements stay failed/unproven | Full-COMMIT/zero-loss/operational recovery/M4 acceptance inferred from refusal: ineligible |

Even an eligible EXT receipt does not approve execution or full M4. Other
acknowledged fixture writes do not automatically convert the tested invocation
to A, nor can they be silently waived; assertions needing their postfault recovery
do not pass. Prior separately completed evidence remains historically scoped.

## 9. Blockers and recommended owner decision sequence

1. **Parent + D01 crosscheck:** bind source semantics here to D01's exact-image
   mapping/equivalence result and boundary evidence interface. No binary inspection
   or calibration result is claimed here. Keep cancellation source reviews frozen.
2. **Protocol owner chooses coverage first:** (a) strict no-amendment reachable
   process cases, refusals remain failure; (b) separate prefault-retired corruption
   admission negative with the narrow coverage above; (c) additionally pursue
   M05's EXT amendment. These are not interchangeable evidence classes.
3. **Correctness/security owners review §6:** approve or reject the sequencing
   interpretation, no-regrant ACL transition, fixed helper write scopes, complete
   failure-prefix cleanup and bounded refusal diagnostics. If §6 is judged outside
   current wording, stop and request a distinct decision; do not silently call it
   compliant. Success here still does not solve in-flight COMMIT refusal teardown.
4. **Only if (c) is necessary:** independently review §8's exact wording and E1–E9;
   explicit owner approval of changes to both §§5.1/17.7(9) is required. No current
   authorization follows from this review-ready proposal or the old draft's GO.
5. **Harness/evidence owner designs new closed receipts and tests:** preserve
   normal validator fail-closed behavior; implement no-regrant restarts without
   setup ready/regrant, independent client ACK classification, branch oracles,
   startup attribution and exact extinction inventory if approved. Agree bounded
   clock/pause/expiry and diagnostic limits before an executable packet exists.
6. **Then** regenerate affected contract/source/bundle/recipe/image pins; obtain
   independent correctness/security/source/CI review and fresh exact-artifact
   execution approvals. Execute positive/negative controls and required real
   same-volume recoveries under those approvals. No historical evidence is edited
   or promoted to a stronger scope. Failed cuts remain visible and require a new
   approved attempt if repeated.

Remaining unproven requirements: target-binary equivalence; complete actual
held-boundary/client-receipt method; source-matched refusal diagnostic acquisition;
all-role no-regrant behavior across the new lifecycle; durable ACL writer and
ownership proof; exact resource/transport/diagnostic extinction; complete maximum-
shape first-publication/backpressure/replay oracles; BOOT lifecycle; approved
schemas/negative controls; and actual execution results. **Review-ready proposal
is the verdict, not authorized or proven implementation.**
