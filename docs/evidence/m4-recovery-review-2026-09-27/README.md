# Recovery lifecycle source review — 2026-09-27

**Corrected candidate: correctness GO and defensive-security GO for image/CI
preparation only.** The original NO-GO and its scope are preserved.

| Artifact | SHA-256 |
|---|---|
| `initial.json` | `c6a833939a33daa1bafe87b8f706927267e8f98f71ee8b994d82fccb2a770322` |
| `corrected.json` | `b5fe64eee5fded5bd3a946d7a58790617d67c95a0447138ba54a79c709aeb888` |
| `verification.json` | `25b020b039ef9e96b28000e7f75ae6067262edaa497e1b1859e0cbba3696b6b5` |
| `verdicts.json` | `06110c785a327a8015a8b755226a65f6f04155c2d9dde8b42dfe74abb0ca0008` |

`verdicts.json` records both initial reviews, corrected dispositions, reviewer
sessions, independent probe hashes and the limits of approval. The corrected
freeze contains **90 files**, **16 recipes** and **66 execution-image inputs**.

The local corrected checks passed: **128 harness tests**, **21 script tests**,
two Go roots under race with **52 canonical recovery invocations**, package vet,
and strict complete Lua/bundle checks. Actual Docker-worker death and real Redis
expiry/recovery remain unmeasured. Simulated lifecycles retain
`case_evidence_valid=false`.

Private originals, immutable source snapshots, local logs and independent probes:

- `/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-recovery-review-2026-09-25/`
- `/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-recovery-review-2026-09-27/`

See the [dated review](../../crawl-jobs-v2-m4-recovery-review-2026-09-27.md).
No source correction here changes canonical Lua, the normative protocol, or the
unapproved observer/AOF-failure proposal.
