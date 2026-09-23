# Bootstrap/ACL Step 3 review inventories — 2026-09-23

This package records exact-byte source inventories for the independent correctness
and security reviews of `bootstrap-acl-negatives-v1`.

The initial inventory contains the 81 Step 2 implementation/source/test/planning
paths. It binds all 15 recipe hashes, the 62-file execution-image scope and the
unchanged canonical protocol/source identities. Mutable status roll-ups are
outside the frozen source inventory.

**Final result: correctness GO and security GO for image/CI preparation only.**
Both reviewers found no actionable findings and verified all source hashes
unchanged. No correction or re-review delta was necessary.

| Exact-byte artifact | SHA-256 |
|---|---|
| `initial.json` — also the final reviewed source identity | `dbe881b2c269535b633188986c6f3adbdac0226df23528290bc4554c93277113` |
| `correctness-checks.json` | `a69159bfcc6b4b84e8409a8966350b6c1a476b3027d0dbbc4639676654fd1b86` |
| `security-results.json` | `59a1241d5bbeebaa9aeb884dc67edac9846c227c583e3de0d8d9e5e49f33e9b0` |
| `verdicts.json` | `1e0e8d4ccd0aa27abc18e538731020563b3617b2d00c643aaf4052aecd374eb7` |

The correctness summary explicitly preserves one incomplete 360-second review
command; it is not a full-module PASS. Completed checks and their limits are in
the [dated review report](../../crawl-jobs-v2-m4-bootstrap-acl-review-2026-09-23.md).
The summaries match the private reviewer originals byte for byte. Full reports,
probes and source copies remain under:

```text
/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-bootstrap-acl-review-2026-09-23/
```

The scoped export scan's single generic-key detection is the existing public
canonical RETIRE Lua source hash in `initial.json`; its bytes were independently
verified. No unresolved credential finding remains.

These records are neither image validation nor execution approval. The
[primary plan](../../crawl-jobs-v2-plan.md) owns current status.
