# Remaining bootstrap/ACL cases — 2026-09-24

**All 12 remaining cases PASS, with scoped evidence reviews accepted.**
P01's scoped review and all case-specific preparations also passed.
The immutable `manifest.json` identifies the proposed exact per-case execution
scope. Its preparation-time authority fields remain false; approval and separate
execution decisions must be recorded externally before any case invocation.

| Shared artifact | SHA-256 |
|---|---|
| `manifest.json` | `8e43dfb7475e7fa5b42b5fbbc79e69b0b14272cdeaa4950fee7a85a263ebaca3` |
| `p01-review.json` | `3eff5a0a48cea36645289ca10976b0065f827a99ff9df674e5e1bb468c3561f5` |
| `execution-sequence-decision.json` | `1cf9b1b03f8cf5c110ceec52277ddb8d406f4e2e547d721d462f3aa2bb8970e1` |
| `results.json` | `0d175db16e1e6fe0729a1c20dbe1f6f1c81158b9e877b98f732e8e809c707b3d` |
| `coverage.json` | `4cf0ff3823dab5aa164f5401cd16f645bb8a849f3df0e27b622f62c6aa5b99ca` |

Each `preparation/<label>/` directory contains exact `inputs.json`, `plan.json`,
`recipe.json`, `image-validation.json` and independent `postcheck.json` copies.
The manifest lists every file hash, case/scenario, source list, role inventory,
expected response sequence, memory/time bounds and private evidence path.

| Label | Plan SHA-256 | Recipe SHA-256 |
|---|---|---|
| P02 | `f25520b6a2527a1256596a926b7e8508f554855ae5b349fb7cbc3e3c415bebf8` | `932e95290ed1c3465436aecc8cc8e35efce587995530ef5c61ffb9f2a625daa5` |
| P03 | `dcc8328f2da3c5c424478e9b2f9c43e45e425ea214ee8f8f2654781fde691e42` | `6cd3792500c215a4aab6c2e45351fb93ee57becea024529ded9e4949fafc245b` |
| S01 | `02874a4898931cd59a113e9b7041e12f351bdf5bcee72246a7acfa3156d19cf3` | `08c3c826a46be1c88b85868f7cd3c73d9aea43cd70ed04feeadaa3956c8fb3c3` |
| S02 | `fccd16ddf28c49444dc42cbc3cdb6ff7cfc45c1d326f642817b8369a04adf979` | `355668fd41039c6a376a610500042b15cd1cc6c31316b1b4932050cdf099d0da` |
| S03 | `29f4e7018d312add50539a9c506217e641f1895ee563548060b4dcde60dbe982` | `02ac0f6c8f187b7e441b35e4f7375a8d00fb8d830094be8c8f0485b06863742c` |
| S04 | `0d224247d6a49dae6bd7fbd7e7c9bcd160e9d9c7b1f5278ef29f8b7351411b47` | `34fa599e6d50583ac548be2c64afa3af7e2d033dad3bad26bf8612fcc407ec46` |
| S05 | `a7e52054614e4ae500370a5b3f0aeeb8cd4d78419b35226d3fc0b543288cc2a6` | `307aecd4395cb043937b46c83ba05b10698ccc50e2d86c54c51295fb38d6bf28` |
| W | `d7e33fbb53534ed40c128a6bbd760e2f8d5d34eae9546b5014d6b5bdedfe659b` | `127bc729f88867d5db880d855ec9d5e1024e6b4be5dc39f45e5e8cd45d09327f` |
| B | `a6a015ea8ba97ff1d7ecde0875f0eede6aff2b36c55404886f85f439c758c7a1` | `a1215aa5c14bd60585d60bb482aeb719cbe30f3712a02b433dac391000303ae4` |
| A01 | `c57d1c56f59967df140aad9d27c83dd0023148ae3457c621464f964f52ff7d20` | `2d3190d274a92e0704d32bb8c3ea675504e317c7ad1a1240b3dcbc7740288a40` |
| A02 | `9857639d1b3fab3ece85d115089327f8e4bd48cb5039753c6a3112ddd4c38a14` | `5b295b4b00d89d3a2495d9a567cc4d3e888a544f04824024772dab535e6d683c` |
| A03 | `22145f40aef820a8c64c7beec150fdbfadbbf4080fda97322c537992d35be447` | `0d5e6e5208c051f88cee0c388e83381ac88b6e566f149d22b389a32d95f4f693` |

The selected revision is `963b67b73e2cd67ff73f559fa9984813b9d5946a`, Linux/arm64,
operator `fullerkris`, harness
`sha256:51bc4896057b8015e1bb67ff7e57448ed6356449c98a6df9100c06693de8e265`
and Redis 7.4.11
`sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c`.
All 81 reviewed sources and 15 recipes match; fourteen protected checks pass.

Each case had its own plan and one-use approval, fresh instance, two exclusive
volumes and credentials. Execution was explicitly supervised and
sequential, with a postchecked PASS required before the next separately invoked
case. There is no automatic batch dispatcher or retry. Stopping on failure,
ambiguity, source drift or incomplete cleanup was required. The approval window was two
hours from recording; each case retains 300-second execution, 30-second stages
and separate 60-second cleanup limits, with 128/256/528 MiB role limits and 400 MiB
Redis maxmemory. The absolute serial case/cleanup budgets sum to 72 minutes.

All twelve preparations used their explicit case selectors. All 48 metadata
containers stayed stopped and all 72 metadata resources were independently
confirmed absent. Each image check verified 62 files and 15 recipes plus the
memory/isolation predicates. The preparation scan's 24 matches were exactly two
copies per case of the unchanged public canonical RETIRE source digest; there
are zero unresolved findings and no suppression changes.

## Actual results and exact receipts

The owner approved all twelve exact cases, then separately selected
**Execute all 12 sequentially**. Approval-set SHA-256:
`851d86e76168fee51d6fd6ffe149d1e50b0d01182f5df5c136de18f70ceebbdb`.
The window was **18:50:06.241–20:50:06.241 UTC**; actual case decisions/reports
spanned **18:57:18.383–19:02:38.794 UTC**. Each controller was invoked once, with
its predecessor's reviewed PASS and cleanup verified first. All approvals are
**consumed and non-reusable**.

| Label | Result / exact report | Negative calls | Measured positive controls | Direct ACL denials | Roles revoked |
|---|---|---:|---:|---:|---:|
| P02 | [PASS](runs/P02/7211450ed1ef0a985304b8d04436c3b7.json) | 2 | 0 | 46 | 6 |
| P03 | [PASS](runs/P03/ea6ea28830ef4c147d8c1cde30f0473b.json) | 2 | 0 | 46 | 6 |
| S01 | [PASS](runs/S01/bfe2b0785bbe5c7168894b943085a810.json) | 2 | 0 | 46 | 6 |
| S02 | [PASS](runs/S02/d25715f213a08819b4bffac95c1135e0.json) | 2 | 0 | 46 | 6 |
| S03 | [PASS](runs/S03/8d34e553681bcbdc22978259b116aae7.json) | 2 | 0 | 46 | 6 |
| S04 | [PASS](runs/S04/8bb942e8d65449c39c2744b2d90af0f4.json) | 2 | 0 | 46 | 6 |
| S05 | [PASS](runs/S05/d81c83a619938073ef0a0b65221314ac.json) | 2 | 0 | 46 | 6 |
| W | [PASS](runs/W/6da1976759b5c13a364efacc65d42113.json) | 24 | 2 | 46 | 6 |
| B | [PASS](runs/B/0d30d3a2df17f886b8526a499f950fea.json) | 18 | 2 | 0 | 6 |
| A01 | [PASS](runs/A01/937a41c5fbe6fc0f4aed70886070817b.json) | 2 | 1 | 0 | 7 |
| A02 | [PASS](runs/A02/fe3818317dcf62457f33f85805f1999f.json) | 2 | 1 | 0 | 8 |
| A03 | [PASS](runs/A03/6ea3d793c20384c8a96c61ab9e927831.json) | 2 | 1 | 0 | 8 |
| **Total** | **12 PASS** | **62** | **7** | **368** | **77** |

Each `runs/<label>/` directory contains exact copies of the final report,
pre-mutation intent, action journal, case execution decision, scoped postcheck
and single-invocation receipt. `results.json` binds all six file hashes per case,
every approval hash, per-case timing/counters and all consumed dispositions.
There were also three canonical administrative prefix operations, 336 controller
actions and 132 cleanup actions. All 48 actual containers and 24 volumes were
independently confirmed absent. Each run-evidence secret scan had zero findings.
The full exported-packet scan's 25 matches were 24 public RETIRE source digest
occurrences and one checksum of the preparation-scan triage artifact. Exact-byte
checks resolved every match without suppression or approved-artifact changes.

`coverage.json` joins these twelve accepted scoped results to accepted PC01/P01,
so all **14 package Redis cases** have real passing evidence and **zero negative
cases remain unrun**. It preserves the seven partial requirement mappings and
104-requirement/52-operation-variant reference without closing full requirements.
H/I remains offline/simulated evidence; package-level reconciliation and remaining
M4-P3 obligations precede the wider recovery matrix. Full M4 remains open.

Private originals and single-case supervision/verification helpers:
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-negative-remainder-2026-09-24/`.
Per-case working directories are `cases/<label>/`; actual approvals and one-use
reservation/disposition originals remain private.

See the [dated scope and results](../../crawl-jobs-v2-m4-negative-remainder-2026-09-24.md)
and [primary plan](../../crawl-jobs-v2-plan.md). Preparation alone supplies no
execution authority; actual scoped results remain distinct from full M4/release acceptance.
