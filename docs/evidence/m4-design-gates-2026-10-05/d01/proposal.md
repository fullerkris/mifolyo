# D01 increment: exact-binary boundary candidates and OBS1 calibration

**2026-10-05 · DRAFT FOR PARENT / INDEPENDENT REVIEW · D01 ONLY**

## Decision in one paragraph

**GO for the next static/design review; NO-GO for any experiment.** The missing
raw Redis executable has been recovered from the **exact cached image**, without
creating a container, and its SHA-256, size and GNU build ID match the frozen
report. There is now a version-bound ELF/symbol/disassembly manifest, an
explicitly incomplete boundary map, a canonical COMMIT ordinal reconciliation,
and a closed proposed OBS1 calibration suite. No observer image/profile exists
here, no tracing privilege or stop precision has been demonstrated, and no
canonical Lua VM PC/ordinal correlation is proven. This increment does **not**
close D01, OBS0–OBS4, M4 or any execution/acceptance gate.

## 1. Scope and frozen baseline

Read-only baseline: `design-worktree`, branch
`docs/crawl-jobs-v2-m4-design-gates`, HEAD
`e80d00538b11c46d021427bcfe1c408e16071c68`. Initial Git status was clean.
The following inputs were hash-verified, not edited:

| Frozen artifact | SHA-256 |
|---|---|
| `docs/crawl-jobs-v2.md` | `835e98db86e4d0cba224bc9fb9c3a3408514e4730d832773b7448f9b238a69c9` |
| `docs/crawl-jobs-v2-m4-observer-and-failure-design-2026-09-25.md` | `f6568eb6b322fa84fc9164d1b616d983a4f71cdc1dc85d42bccc4cb59b1479ae` |
| `docs/evidence/m4-readiness-2026-09-25/observer-static-inspection.json` | `fa3c1c00fa0392438f748d7902ba2e0e083a05311c84cbeeaa60921f8a728ff3` |

All output is confined to the pre-existing private `d01/` directory. No proposal,
normative document, cancellation source, approval, accepted record or snapshot was
changed. No pull/build/create/start/exec, Redis process, ELF execution, debugger
attach, trace, capability grant, acceptance test, commit/push/PR or D02 matrix was
performed. Scripts here are **offline analysis tools**, not observer implementation.

### Evidence classes

- **E0 / frozen-input verification:** bytes and Git identities checked locally.
- **E1 / exact-image static:** cached image inspection/save; config and all seven
  uncompressed layer hashes verified; selected effective filesystem bytes and
  ELF identity checked. No runtime observation.
- **E2 / embedded-binary static:** symbols, DWARF, instruction decoding and PLT
  relocation joins from those exact bytes. No runtime address or stop proof.
- **E3 / pinned-source derivation:** fetched upstream bytes and canonical Lua
  reasoning; explicitly not a reproducible binary build or observed execution.
- **P / proposed calibration:** requirements and decisions only; no measured
  privileges, counters, latency, cleanup or pass result.

## 2. Recovery and provenance

The supplemental filename search under the approved temporary root found no
`redis-server` file. This is a scoped search, not proof of absence everywhere.
The exact cached image was then inspected and saved through
`unix:///var/run/docker.sock`; **no container was needed**.

| Identity | Result |
|---|---|
| Docker image **config ID** supplied as the pin | `sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c` |
| Inspected RepoDigest (distinct identity; not substituted for the config ID) | `redis@sha256:cd953e4e9b4725f0d87a2b170c3d313ad641be5370033b46262e07f8010788a3` |
| Platform | Linux arm64/v8 |
| Effective archive member | `usr/local/bin/redis-server` |
| Recovered file | `redis-server.elf`, retained non-executable mode `0600` |
| Bytes / SHA-256 | **16,892,616** / `772f79e9154598fe509961928dc4d7c1ac577a948be6e86b793fdb7b0599e1b6` |
| GNU build ID | `a6678635938881e640c42ca2824a014aea114c4f` |
| ELF | ELF64 little-endian AArch64, ET_DYN (PIE), type 3 |

`recovery.json` records layer order, whiteout checks and the selected regular
member. Saved config bytes hash to the image ID. Every layer's raw tar hash
matches its corresponding RootFS diff ID; a later whiteout/replacement would
invalidate an earlier selection. Only the selected file is materialized from the
layers; no archive executable or symlink is followed. `exact-image.tar` is retained
for private reinspection, not an image import or repository artifact.

The upstream `7.4.11` tag resolved to Git commit
**`aaf0ce63b3239f4b51f86ca1da8711b055721993`**. Twenty-five selected source files
from the Redis release tarball match raw files fetched at that commit; five
additional build/header files also match. Retrieval URLs, final URLs, response
metadata, times and content hashes are retained. In addition, the archive URL
recorded in the image's own build history was fetched: its SHA-256 is
**`e973da69febfea096ab94690b44bf976482788a1b5e070df373b4f27697d57d4`**, exactly the
history's expected value; its 25 compared source files match too.

This is stronger than an unpinned version-tag citation, **not reproducible-build
proof**. Image history reports changes to `config.c` (protected-mode default) and
`deps/Makefile` (jemalloc configuration), TLS build, and removal of the build tree.
The upstream files are not falsely labeled the complete post-patch build tree.
Embedded compiler comment identifies GCC Debian `12.2.0-14+deb12u1`; Redis CUs
report `-O3 -flto -g -ggdb` and DWARF5. Exact historical toolchain/dependency
packages, all generated headers, post-patch source and independent build
attestation/reproduction remain unverified. Build ID is a correlation identifier,
not an authenticated source provenance statement.

### Important negative finding: Lua VM debug coverage

37 selected function bodies were decoded. There are 200 DWARF compilation units,
but this inspection found **no `lvm.c` CU or DWARF line rows for `luaV_execute`**.
Lua function symbols still exist. Lua structure layouts are present through the
Redis C compilation units' included headers. The upstream dependency Makefile
defaults `LUA_DEBUG=no` and uses `-O2` without `-g`, which is consistent with the
observed gap but is not proof of historical build flags. “Debug sections present”
must not be upgraded to “every Lua VM instruction has a verified source location.”

## 3. Version-bound candidate boundary map

`locations.json` contains actual symbol values/sizes/bindings, PT_LOAD segments,
file offsets, instruction bytes and per-symbol hashes, DWARF rows, and decoded
direct/indirect control transfers. `candidate-disassembly.txt` is a readable
companion; `dwarf-candidates.json` retains raw candidate DIE attributes/layouts.
`boundary-map.json` supplies 17 candidate boundary families and 11 prevalidation
classes, with confidence and missing evidence on each.

**Every address below is an ELF-relative virtual address.** In these code
segments the numerically equal file offset is independently derived from PT_LOAD;
that coincidence is not a general rule. Runtime VA is `verified load_bias +
ELF_VA`. No load bias, ASLR map, target PID or runtime breakpoint is claimed.
For example, `server` has ELF VA `0x388ec0`, size 5,776, but **no file-backed
offset** because it is in zero-filled memory. Its runtime contents were not read.

| Candidate | ELF VA(s) | What is statically supported / what is not |
|---|---|---|
| Outer EVALSHA entry | `evalShaCommand=0x14a7a4` (288 bytes) | Script/invocation entry candidate; cache miss/debug rejection and other scripts still need discrimination. |
| Lua bridge | `luaRedisGenericCommand=0x1cb5b0`; `bl scriptCall` at `0x1cb710` | Argv converted before Redis-side checks. Prevalidation reads also pass here. |
| Inner call entry/return | `scriptCall=0x1cb000`; call instruction `0x1cb1c8`, continuation `0x1cb1cc` | Redis checks precede call. Return still precedes Lua reply conversion/cleanup. |
| Native procedure before/after | `call=0xa3cc0`; **`blr x1` at `0xa3e0c`**, continuation **`0xa3e10`** | Concrete instruction cuts around `c->cmd->proc(c)`. Native return is not call bookkeeping completion, success proof, fsync or acknowledgment. |
| VM fetch candidate | `luaV_execute=0x1f9024` (4,736 bytes); **`0x1f9078`: `ldr w19,[x27],#4`** | Disassembly suggests current bytecode pointer in x27 before fetch, L in x20, closure in x26. No Lua line-DWARF, canonical Proto/PC or live register-role proof. |
| Propagation unit | `propagatePendingCommands.lto_priv.0=0xa3640` | Exact LTO clone exists although the unsuffixed symbol does not. Effects queue/MULTI/EXEC context must be bound. |
| AOF in-memory append | `feedAppendOnlyFile=0x11d180` | Buffer construction, not disk durability. |
| AOF write | `flushAppendOnlyFile=0x11c4a0`; **`0x11c584 → write@plt`**, continuation `0x11c588` | The actual inline write-loop path; standalone `aofWrite=0x1152e0` would miss it. A returned iteration can be short/error/EINTR-related. |
| AOF sync | **`0x11c894 → fdatasync@plt`**, continuation **`0x11c898`** | Exact PLT/GOT relocation resolves `fdatasync`; no exported `redis_fsync` function (Linux macro). Return site is reached on error too: require return 0 and matching fd/offset/context. |
| Lua result conversion | `luaCallFunction=0x1ca490`; **`0x1ca608 → luaReplyToRedisReply`** | Success-result buffering on original client; not receipt. |
| Client transport | `writeToClient=0xc3680`, `_writeToClient=0xc1ed0`; socket write/writev symbols retained | Connection/session/frame binding and thread/partial-write handling missing. Kernel accepted bytes are not client acknowledgment. |
| Client acknowledgment | **No redis-server ELF address** | Independent exact-session/invocation evidence required. |

Core call/AOF candidates have high confidence in **bytes and native location**,
with source interpretation corroborated by DWARF and fetched source. Their
runtime-held-stop confidence is **unproven**. VM register roles are a lower-level
disassembly inference. Canonical Lua bytecode PCs are deliberately `null` in the
map rather than invented addresses.

Notable optimization hazards: LTO changes symbol names and inlines `aofWrite`,
post-unit operations and networking helpers; many source rows map to multiple
addresses and rows from inlined files. A symbol-entry breakpoint or “next source
line” does not necessarily bracket the intended event. PLT call sites here are
not libc/kernel syscall-entry addresses; those separate binaries were not mapped.

### VM-specific next static questions

Embedded candidate layouts include `lua_State.savedpc=48`, `.ci=40`, `.base=24`;
`CallInfo.func=8`, `.savedpc=24`; `LClosure.p=32`; `Proto.code=24`, `.lineinfo=40`,
`.locvars=48`, `.sizecode=80`; `TValue` size 16. These are exact embedded layout
facts, not permission to dereference them or proof that optimized x-registers
always hold those roles. In `lvm.c`, the local `pc` advances on fetch and `savedpc`
is updated at particular protected paths; blindly reading `L->savedpc` can be
stale/off by one. The future mapping must resolve the loaded script root and
nested prototype tree, bytecode instruction index, locvar live interval and stack
slot for `execution` and `i`, plus repeated item ordinal during prevalidation.
Do not insert Lua debug hooks or change the canonical interpreter to obtain it.

## 4. Canonical COMMIT reconciliation

An in-memory use of the pinned generator's `assemble` function reproduced the
canonical bytes exactly. It did not execute Lua, write the worktree, or generate
Lua bytecode. All 17 source chunks and their canonical line/byte ranges are pinned
in `canonical-map.json`.

| Identity | Value |
|---|---|
| Canonical SHA-256 | `e6d9f14ccc566cff43148790af96f1b2d65e890a2a4e7b33e3bb475c1427d4fe` |
| Exact-byte Redis script SHA-1 | `d187ed1bda07f50f6e15ffc7fa5e515bf3bfe9fa` |
| Size | 827,166 bytes, 9,106 lines |
| Generator SHA-256 | `9b0aab29d20d2622913ade9da70b65e958e6dff05ef98a698f34eda3fc79fc05` |
| Operation fragment | `lua_src/ops/cj2_commit.lua:1–8` → canonical `9099–9106` |

### Prevalidation is not command dispatch

`Stage.prepare` constructs an execution plan or rejects. Key/wire/time/guard
checks, retained-job/purge and early replay, live lease/run/authorization/request
checks, stage B/G/fence/slot/content checks, destinations/discovery/alias branches,
queue/memory admission, aggregate simulation, response construction, descriptor
bounds and **every** ACL preflight all precede the mutation tail. The prevalidation
map names source anchors, not a complete validated set of machine/VM stop cuts.
Pure-Lua hashes, parses, comparisons, loops and arithmetic never pass through
`scriptCall`; command-dispatch observation alone cannot cover them.

`plan.lua:525` constructs `{calls,count,reply}` **before** the ACL loop at 526–530
and final seal at 531–534. An observed table named `execution` is not sufficient
to declare mutation-ready. `add(plan,...)`, `Run.hset` and `Run.flush` in the
preparation phase append **inert descriptors**, not live Redis writes.
`Plan.add` can split wide descriptors at 258 arguments; empty/unchanged HSETs
can disappear. The actual sealed `execution.count` is authoritative per invocation.

### Concrete branch-derived ordinal inventory, not a maximum fixture

For 64 images, 256 nonempty outlinks, 128 **fresh** discoveries, five aliases
requiring both URL-witness/depth hashes, and three nonempty aggregate hash writes,
the unsplit branch derives **1,050 command attempts**:

| Ordinals | Descriptors |
|---|---|
| 1–2 | Page RENAME/PERSIST |
| 3–4 | Outlinks RENAME/PERSIST |
| 5–132 | 64 image RENAME/PERSIST pairs |
| 133–134 | Manifest RENAME/PERSIST |
| 135–390 | 256 backlink SADDs |
| 391–1030 | 128 × (job HSET, jobs SADD, order ZADD, ready ZADD, ready-at ZADD) |
| 1031–1032 | Visited URLs HSET, visited depth HSET |
| 1033 | One pages_queue LPUSH |
| 1034 | Source-job completion HSET |
| 1035–1039 | Leased/leased-at removals, completed add, global lease removal, backpressure removal |
| 1040–1042 | Sorted Run.flush hashes: disposition reasons, group open jobs, run |
| 1043 | Stage-expiry ZADD |
| 1044–1049 | Six residual PEXPIREATs |
| 1050 | Final global stage-slot HDEL, appended by Plan.assess/finish_slots |

`mutation-ordinals.json` materializes all 1,050 semantic descriptors, with source
anchors but **no invented fixture keys or argument digests**. The run's maximum
64 groups avoids a group-map split here. This is neither a constructed valid
maximum-count fixture nor a combined maximum-byte/memory/allocator proof.

The shared mutation line is canonical **9104**. Every selected cut must privately
match the exact script/prototype/PC, sealed count, **i**, argc and unambiguous
length-framed command/argument digest; raw native hit count is insufficient.
Prevalidation reads, ACL helper activity, outer EVALSHA and nested calls can
reuse native dispatch sites. Adjacent identical commands are still distinct
ordinals. Native procedure return and Lua `redis.call` return are distinct cuts;
the latter still needs actual VM continuation mapping.

Branch differences must remain explicit:

- Empty outlinks omit their pair and all backlinks; zero images still have a
  manifest pair. Existing discoveries have no five-call admission group, yet
  nonempty staged discovery data still adds three residual expiries.
- New aliases, shallower existing aliases and unchanged aliases yield different
  HSETs; zero net group delta can omit the group hash. Recalculate count/ordinals.
- The first backpressure branch derives **three** descriptors: job bookkeeping
  HSET, backpressure ZADD, and slot-remainder HSET appended during assessment.
  Its Run.flush changes index projections but emits no run HSET for an unchanged
  run. Backpressure replay/deadline refusal has **zero** descriptors.
- Exact `ALREADY_COMMITTED` has zero descriptors, using retained receipt checks
  before live lease/stage/destination admission; rejected preparation has no
  execution object. Neither can borrow the successful-publication cut inventory.
- The count refers to **attempted descriptors**, not dirty effects. A no-op
  ZREM/PERSIST or a command rewriting its propagation can break any 1:1 mapping
  between Lua ordinal, Redis dirty count and AOF record count.

## 5. Mutation, persistence and acknowledgment are separate dimensions

```text
prepare + reads/pure Lua + reply/ACL seal
  → descriptor i native call → native return → bookkeeping → Lua conversion
  → ... descriptor N ... → Lua result buffered on original client
  → outer execution unit finishes → accumulated effects propagated (MULTI/EXEC)
  → AOF memory buffer → write loop → always-policy fdatasync success
  → client transport write(s) → independent client complete-frame receipt
```

This is the normal source-derived path, not a proof that every configuration,
error/busy-script/reentrant event-loop or background-thread path follows a single
observed timeline. `server.c:3392–3459` propagates the **effects** of a completed
execution unit and wraps multiple pending effects as a transaction. The inner
Lua descriptor loop is not an incremental “one durable AOF record per i” oracle.
`luaCallFunction` buffers the Lua result before the outer call's later propagation;
the beforeSleep normal path flushes AOF before pending client writes.

For the normative `appendfsync always` policy, `aof.c:1228–1244` synchronously
calls Linux `fdatasync` and exits on failure. Both a successful return and an
error reach `0x11c898`; reaching that PC is not itself “persisted.” An AOF fd,
multipart file generation/offset, expected byte interval, complete transaction,
successful return and same-case identity still need binding. Storage/crash/restart
claims require the later actual same-volume post-state oracle. This report makes
none. Background I/O and other threads may continue while the main thread is held.

Likewise, successful Lua/native return, reply buffering and socket write do not
prove complete client receipt. Independently classify the tested session and
invocation as `known_pre_acknowledgment`, `acknowledged` or `unknown`. Silence or
a missing receipt is **unknown**. Acknowledged first backpressure bookkeeping is
a state-changing outcome even though it is not `COMMITTED`; setup/BOOT/probe
acknowledgments belong to different invocations. D01 provides those bindings to
the parent. **D02 alone owns startup-refusal/credential-proof eligibility and
its full matrix; this draft neither amends it nor supplies a substitute proof.**

## 6. Proposed OBS1 decision record

**Status: Proposed.** Use a separate, purpose-built synthetic target to calibrate
the minimal demonstrated target-only hardware-stop observer *before* any Redis
trace request. Full plan: [calibration-plan.md](calibration-plan.md).

The two confinement options are capless same-UID sibling observation (preferred
if sufficient) and separately approved non-root `SYS_PTRACE`-only observation.
Both use only the owned target PID namespace, a narrow seccomp request/regset/TID
filter, read-only root, no network/host socket/data mount, fixed programs and
bounded private reads/receipts. Actual UID/capability sets, hardware debug slots,
seccomp and user-namespace feasibility are **not demonstrated**.

The proposed suite has 276 trials per admitted candidate: 140 native ordinal
cuts, 60 fake-VM identity cuts, 40 thread/ack distinctions, 24 loss/cleanup trials
and 12 closed negative controls. It includes EXITKILL failure handling backed by
independent controller/watchdog cleanup, actual absence receipts, no arbitrary
debugger inputs, and explicit pause/event/read/output budgets. Thresholds are
proposed rejection limits, not measured Redis timing equivalence. All target,
observer, profile and validator artifacts must be frozen/reviewed and separately
approved before these tests can become executable.

**Trade-off:** hardware stops preserve unmodified target instructions and can
hold before dispatch, but add trusted memory/debug authority and perturb timing;
fake-VM success will not prove the real optimized Lua mapping. Command-only
observation is simpler and cheaper, but cannot satisfy pure-Lua cuts. Cooperative
hooks/modified binaries could simplify correlation but violate the current
equivalence scope. Prefer an honest blocked D01 over claiming partial coverage as
complete or silently expanding privileges.

## 7. Actionable next-review packet / blockers

| Decision | Present result | Required next evidence / owner |
|---|---|---|
| Raw-byte availability blocker | **Resolved, E1** | Parent/reviewer independently rehash retained ELF and recovery chain. |
| Version-bound primitive location manifest | **Delivered, E2** | Independent review of decoding, PT_LOAD offset math, LTO/PLT joins and source interpretation. Not all canonical cuts mapped. |
| Exact build provenance | **Partial, E3** | Parent chooses attested historical post-patch build provenance or a separately reviewed bounded evidence standard; version tag/build ID alone never accepted as reproduction. |
| Lua prevalidation/loop mapping | **Blocked** | Resolve loaded canonical prototype/bytecode/locvar/ordinal binding and optimized VM dataflow without changing semantics. No line-only shortcut. |
| Confined observer feasibility | **Blocked** | Freeze OBS1 source/images/config/seccomp/receipt/cleanup contracts; independent correctness/security review; explicit owner execution decision. No guessed UID/cap success. |
| Runtime stop precision / bounds | **Unmeasured** | Actual approved synthetic OBS1 suite, including scheduler-delay bounds and observer-loss controls. |
| Redis correlation and all cuts | **Not authorized** | Separate OBS2 then OBS3/OBS4 decisions. Fresh small-operation proof precedes full COMMIT. |
| D02 integration | **Parent handoff only** | Supply case/invocation/ack/stop/resource bindings, not a duplicate refusal matrix or weakened teardown rule. |

Recommended next decisions: accept this as a **static research increment**;
request independent static/security review; decide whether to authorize a later
OBS1 implementation-only artifact-preparation task. Do **not** authorize tracing
from this packet. If a minimal confined profile cannot work, return to target or
mechanism selection rather than increase privileges or change canonical Lua.

## 8. Reproduction record and retained outputs

Exact commands/subprocess return codes/stdout/stderr and fetched URL byte hashes
are in `acquisition.jsonl`; the command index and final hash verification are
referenced by canonical `evidence.json`. `development-notes.md` retains the one
corrected analysis-script byte-assertion transcription error and intermediate
analysis versions; no failed attempt is presented as an experiment.

Top-level operations actually used in this private directory:

```text
python3 acquire.py recover
python3 acquire.py deps
python3 acquire.py sources
.venv/bin/python analyze.py                         # initial analysis
.venv/bin/python analyze.py                        # additional symbol revision
.venv/bin/python -B reconcile.py summaries
python3 -B reconcile.py canonical
python3 -B reconcile.py image-source
.venv/bin/python -B analyze.py                      # final clone/PLT revision
.venv/bin/python -B reconcile.py summaries
python3 -B reconcile.py extra-sources
.venv/bin/python -B build_maps.py                   # one failed assertion, corrected rerun
python3 -B finalize.py                             # final evidence serialization/verification
```

The acquisition script's Docker allowlist was only:

```text
docker --host unix:///var/run/docker.sock image inspect sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c
docker --host unix:///var/run/docker.sock image save --output <this-private-d01>/exact-image.tar sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c
```

Analysis dependencies are pinned `pyelftools==0.32`, `capstone==5.0.3` in this
directory's private venv only; downloaded wheel bytes are hashed in
`dependency-artifacts.json`. No global or repository dependency was changed.
Do not blindly rerun network acquisition against mutable tags: verify the retained
identities or fetched bytes against this packet. Scripts do not execute the ELF.

Parent integration should consume the draft, canonical evidence, source/boundary
and ordinal manifests and selected review excerpts. Large image archive, venv,
wheels and fetched source archives are private reproduction material, not a
request to commit them. `artifact-index.json` binds retained evidence artifacts;
`evidence.json` excludes its own hash by design. Final delivery provides that hash
out of band and in `SHA256SUMS`.

### Primary source citations (pinned bytes retained)

All Redis code links below use resolved commit
`aaf0ce63b3239f4b51f86ca1da8711b055721993`, not a floating tag:

- [EVALSHA / execution / effects comment, eval.c:551–663](https://github.com/redis/redis/blob/aaf0ce63b3239f4b51f86ca1da8711b055721993/src/eval.c#L551-L663).
- [Lua bridge, script_lua.c:878–981](https://github.com/redis/redis/blob/aaf0ce63b3239f4b51f86ca1da8711b055721993/src/script_lua.c#L878-L981), [result conversion:1657–1742](https://github.com/redis/redis/blob/aaf0ce63b3239f4b51f86ca1da8711b055721993/src/script_lua.c#L1657-L1742).
- [scriptCall:575–635](https://github.com/redis/redis/blob/aaf0ce63b3239f4b51f86ca1da8711b055721993/src/script.c#L575-L635).
- [Native call and propagation, server.c:3389–3802](https://github.com/redis/redis/blob/aaf0ce63b3239f4b51f86ca1da8711b055721993/src/server.c#L3389-L3802), [AOF-before-reply:1733–1757](https://github.com/redis/redis/blob/aaf0ce63b3239f4b51f86ca1da8711b055721993/src/server.c#L1733-L1757).
- [AOF write/sync, aof.c:1007–1253](https://github.com/redis/redis/blob/aaf0ce63b3239f4b51f86ca1da8711b055721993/src/aof.c#L1007-L1253), [buffer append:1307–1345](https://github.com/redis/redis/blob/aaf0ce63b3239f4b51f86ca1da8711b055721993/src/aof.c#L1307-L1345).
- [Client write semantics, networking.c:1863–2068](https://github.com/redis/redis/blob/aaf0ce63b3239f4b51f86ca1da8711b055721993/src/networking.c#L1863-L2068).
- [Lua VM fetch, lvm.c:379–408](https://github.com/redis/redis/blob/aaf0ce63b3239f4b51f86ca1da8711b055721993/deps/lua/src/lvm.c#L379-L408), [Lua dependency build flags](https://github.com/redis/redis/blob/aaf0ce63b3239f4b51f86ca1da8711b055721993/deps/Makefile#L79-L102).
- Normative frozen local source: §§2.2, 10.1, CJ2_COMMIT (lines 3392–3525),
  §17.2 (4615–4638), §17.7; immutable prior design §§2.1–2.3. These remain
  authoritative; this proposal changes none of them.
