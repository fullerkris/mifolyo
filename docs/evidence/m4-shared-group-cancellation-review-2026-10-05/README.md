# Shared-group cancellation source-review evidence — 2026-10-05

**GO for image/CI preparation only.** Both independent reviews have no actionable
findings or blockers for the exact 108-file cancellation source snapshot.

The [manifest](manifest.json) binds three byte-identical records:

- [Correctness review](correctness-review.json).
- [Security review](security-review.json).
- [Combined review decisions](verdicts.json).

Source inventory: `2221812ca56635055b8acb6271b2fd0fbf27f8d603703171c54fc58bb14062d4`.
Recipe: `b36d4bb7d2a4e657afbcda6a9cc3160b68678b19d590f7fb8c75b1f4de6d7a4f`.

The [initial source/verification exports](../m4-shared-group-cancellation-2026-10-05/README.md)
remain byte-identical, including their recording-time pending-review fields.
The full 183-test run preceded the final ACL guard; the final 33-test run and independent final-byte
checks followed it. A final-byte full 183-test invocation is not claimed. Review logs
and scripts remain private and hash-bound by the review records.

The manifest/index are derived. These verdicts authorize no real case or tracing,
do not validate new images or protected CI, and do not close full M4. See the
[review report](../../crawl-jobs-v2-m4-shared-group-cancellation-review-2026-10-05.md)
and [current plan](../../crawl-jobs-v2-plan.md).
