# Shared-group lifecycle source evidence — 2026-10-01

**Independent correctness/security GO for image/CI preparation**, case
`ledger-shared-group-capacity-v1`, on base
`7da55b14b32c79ec4b2b95b0a9fd31dda40f0571`. The source is implemented and locally
verified; no new image or real shared-case run exists.

The [manifest](manifest.json) binds five byte-identical records:

- [107-file source inventory](initial.json).
- [Verification results](verification.json).
- [Combined review decisions](verdicts.json).
- [Independent correctness review](correctness-review.json).
- [Independent security review](security-review.json).

The 13-path lifecycle delta adds the bounded 90-position reader, 45-entry setup,
fixed 21-call measurement and 60-counter/redacted receipt contract, 46 authority
denials, failure-prefix retention and cleanup integration. Runtime has no selector
for cancellation/reversed offline traces. Four containers, two volumes, six roles
and the existing 300/30/60-second bounds remain.

Full **171 harness tests**, **21 script tests**, **81 canonical Lua invocations
under race**, vet and source/bundle pins pass. Ten lifecycle test methods exercise
15 fault profiles, all 22 prefix boundaries, actual worker serialization and
Docker parser paths. Both source reviews are GO, with no actionable findings.

The manifest/index are derived; originals retain review-time status/qualifications.
Aggregate verification combines the final check outcomes with the reviews. The
[earlier offline foundation](../m4-shared-group-capacity-2026-10-01/README.md)
remains a separate immutable checkpoint.

These are source/simulation/in-memory conformance records. They do not establish
target Redis ACL/isolation behavior, physical parallel workers, per-owner
credential isolation, actual network I/O or full M4 acceptance. The next gate is
selected image preparation and then publication/CI and separate execution approval.

See the [dated lifecycle checkpoint](../../crawl-jobs-v2-m4-shared-group-lifecycle-2026-10-01.md)
and [implementation plan](../../crawl-jobs-v2-plan.md). This batch is local and
uncommitted.
