# M4-P3 readiness reconciliation — 2026-09-25

**Scoped M4-P3 bootstrap/ACL readiness accepted** on checkpoint
`963b67b73e2cd67ff73f559fa9984813b9d5946a`. Independent correctness and defensive
security reviews both returned GO. Full M4-P4/P5 acceptance remains open.

This package combines the existing fourteen real-Redis package cases with the
explicitly classified 82 H/I variants, seven supplemental host predicates and
nine networkless target-image predicate controls. Four metadata containers stayed
stopped; a separate Python predicate container ran. **No new Redis server or
acceptance case was started by this reconciliation.**

Files:

- `admission-reconciliation.json`: exact supplemental result.
- `target-predicate-results.json`: exact target-image predicate result.
- `review-verdicts.json`: coordinator-recorded independent review decisions,
  session identities, exact reviewed hashes and limitations.
- `readiness.json`: scoped phase decision and full-inventory identity.
- `recovery-oracle-verification.json`: final corrected offline checks, with integer
  millisecond durations and the original private verification hash retained.
- `recovery-oracle-review.json`: exact five-file independent correctness GO after
  both nonblocking findings were corrected and re-reviewed.
- `observer-design-review.json`: owner-selected local Docker direction and
  corrected proposal-intake GO decisions; no amendment or tracing approval.
- `observer-static-inspection.json`: static ELF/symbol inspection from a stopped
  owned Redis-image container, including exact container identity and absence.

| Reconciliation artifact | SHA-256 |
|---|---|
| `admission-reconciliation.json` | `6e74e8291b578463f774edc921b44f400cc93c94e01496c786543d26bcdf94f4` |
| `target-predicate-results.json` | `081f30e41d4125a7c9b82738f6780a2a8e09e6eb111eaba6ac474fe82072d4fe` |
| `review-verdicts.json` | `a1ab951eb1ad4c3256b30eba5c0c0236cbd0268fc1f95a57b3dcba34bb9d4066` |
| `readiness.json` | `4ea4a7ee41f17fe56563526c9101d4f47ae09e8fd154e04e2c3ffae33e183c76` |
| `recovery-oracle-verification.json` | `0b7a1a66d46de86221ebe2a63057bcd6a1963fb2284cf32d043d94ff8ab3055a` |
| `recovery-oracle-review.json` | `165f1c2b28c48280b587302d7e130f38d4c01203510e7692347116772c680d4a` |
| `observer-design-review.json` | `5a93f4e492641b65652d536c26db70e93a3bd5f1b439336c8a1d7a6b35e28ae9` |
| `observer-static-inspection.json` | `fa3c1c00fa0392438f748d7902ba2e0e083a05311c84cbeeaa60921f8a728ff3` |

The recovery foundation is offline only: six Python tests and 26 Go/canonical-Lua
invocations pass, including the race check. It neither registers a runnable case
nor observes worker death. The executable registry/image remain unchanged; only
the Spider builder copies the new vector modules.

The [observer/AOF-failure proposal](../../crawl-jobs-v2-m4-observer-and-failure-design-2026-09-25.md)
has exact SHA-256 `f6568eb6b322fa84fc9164d1b616d983a4f71cdc1dc85d42bccc4cb59b1479ae`.
Both independent reviewers returned proposal-intake GO after acknowledgment,
fault-class and cleanup ambiguities were corrected. Its proposed normative
amendment remains unapplied and unapproved.

Static inspection found an AArch64 ELF with symbol and debug sections. No Redis
process started, no tracing occurred and no new capability was granted. This
captures prerequisites, not a verified source/address map or held-boundary method.

The driver and raw metadata remain private in
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-full-acceptance-2026-09-24/`.
The workspace was opened on September 24; the resumed checks/reviews occurred on
September 25. Raw metadata is not exported.

The predicate helper's cleanup is a producer-recorded assertion: its exported
receipt lacks a separate resource-identity/absence record. Reviewers found this
nonblocking for narrow P3, which also retains the actual case teardown evidence;
it cannot establish independently auditable helper cleanup or all-exit coverage.

See the [readiness assessment](../../crawl-jobs-v2-m4-readiness-2026-09-25.md),
the [full M4 planning inventory](../../../tests/crawl-jobs-v2-redis/planning/full-m4-acceptance-v1.json),
and the primary [implementation plan](../../crawl-jobs-v2-plan.md).
