# First passing bounded M4 smoke case — 2026-09-22

Fixture: `f9692c58d9f07689660a97fbc70ea973`.
Approved/executed revision: `a02991c322c3472f7460adbb94b2a15d82b77af6`.
PR #10 squash merge: `ff2457ebe998707d220e4ce3425aab500c75f5b4`.
Both use Git tree `6c448ac59e70453bb5a10ebc7d781a5a2e00fb2b`.

This directory retains exact controller report/intent bytes, the separate owner
execution decision, fixture-filtered Docker events and an independent postcheck.
Original private files remain outside Docker volumes under
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-execution-2026-09-22/`.

The immutable intent says `INCOMPLETE` because it precedes mutation; the separate
final report binds its hash and records the completed result. A valid passing
`ledger-smoke-v1` case is not full M4 acceptance. The earlier failed init attempt
is preserved separately in `docs/evidence/m4-smoke-2026-09-22/`.

Report SHA-256: `6152a302a95da89eaee3340f1376ec75c8c1833fe80763a587ce4307d6b3b7c5`.
Intent SHA-256: `a672893ae0c91e0f8697a2f9601b71cc6d1dbecb5fb7135c658659fab117fe77`.
Execution-decision SHA-256: `a054b29782b7a2720f3d8f6d81d8646c37d5b2b5993967c0fe37cd2dcda7133c`.

`docker-events.jsonl` is empty: the post-run historical query returned zero
fixture events. `postcheck.json` explicitly records that this is not independent
lifecycle-timeline proof. All six direct resource-absence checks passed.
See the [dated result](../../crawl-jobs-v2-m4-smoke-pass-2026-09-22.md).
