# Refreshed PC01 execution evidence — 2026-09-23

Case: `ledger-claim-release-v1` / PC01.
Fixture: `0a1a9641a6044e4dfde80a5c3d381216`.
Executed commit: `634040131b36e1cbbc2e251364dacbec2ae5dd01`.

This package retains exact report, intent, action-journal, execution-decision
and independent postcheck bytes. The original private evidence remains outside
fixture volumes in `m4-pc01-execution-2026-09-23` under the approved temporary root.

**PASS:** all nine transitions, 46 ACL denials, complete state/expiry checks and
33-counter projections passed. All five runtime stages observed the required
holder-plus-exec process pair. The 28-entry journal includes 11 cleanup actions;
all six credentials were revoked, and separate inspections confirmed all four
containers and two volumes absent.

| Artifact | SHA-256 |
|---|---|
| `0a1a9641a6044e4dfde80a5c3d381216.json` | `2e1d4817d835d5a76a42552ca31645f97b406843b0bd980a0651353843158e86` |
| `0a1a9641a6044e4dfde80a5c3d381216.intent.json` | `e909fa6b577d426b2d66df60dd48a2e9a573d9398d5a2f861451b33d85eeae66` |
| `0a1a9641a6044e4dfde80a5c3d381216.actions.jsonl` | `230dbc52404551d8a6c23ca8ef2f5fa116502c587d0c6eefea9ead9d5dd4383b` |
| `execution-decision.json` | `6636c0d6f4183c31072f4ab1be76202bbdbdfa5130ecd1ea3c93f4d3b392c46a` |
| `postcheck.json` | `9f358d632c79e1bb6ffcc72703998d0984ac50b19eb81395e28f562a8c6b2f94` |

The 49,637-byte report was written at 20:22:51.406 UTC; the postcheck completed
at 20:26:54.614767 UTC. All five exports match their private originals exactly.
The scoped evidence scan found no leaks. The intent's `INCOMPLETE` status is its
pre-mutation snapshot; the final report binds that intent and records PASS.

Approval `3e034bd6f56cc595fd4d8cba566f2722a3428691f185cecfd59228dedde7a695`
is consumed, with `reusable=false` in the private final disposition. One GitHub
HTTP 503 occurred during read-only preflight before reservation or fixture start;
the controller was invoked only once.

See the [dated PC01 result](../../crawl-jobs-v2-m4-pc01-run-2026-09-23.md).
The 13 negative cases remain unrun and full M4 remains open (`m4_accepted=false`).
