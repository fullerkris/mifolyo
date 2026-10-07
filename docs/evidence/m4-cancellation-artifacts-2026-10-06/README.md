# Cancellation artifact-review proposal — 2026-10-06

**Owner artifact approval pending; no execution authorized.** The
[manifest](manifest.json) binds five exact selected exports:

- [16-artifact request](request.json).
- [Unapproved template](proposed-approval.json), with `approved=false`.
- [Scope and bounds](scope.json).
- [Fresh read-only preflight](fresh-preflight.json).
- [Proposal reconstruction and refusal check](packet-check.json).

Request: `0deedc21b8fe5d8297deb7858cafc3a1cb21302bf1ac98d8b0c4c87856940003`.
Proposed expiry: **2026-10-06 20:21:00.864 UTC**. The prospective approved-byte
hash is a reviewed proposal identity, not an issued approval. Expiry/drift requires
a fresh request and owner decision.

The complete private packet also contains byte-identical source/review,
preparation and CI records already linked from the
[source checkpoint](../m4-shared-group-cancellation-2026-10-05/README.md),
[source review](../m4-shared-group-cancellation-review-2026-10-05/README.md),
[selected preparation](../m4-shared-group-cancellation-preparation-2026-10-05/README.md)
and [CI result](../m4-cancellation-ci-2026-10-06/README.md). Artifact filenames in
the request refer to that full private bundle; they are not all duplicated here.

No runtime approval file, reservation or case invocation was created. See the
[decision scope](../../crawl-jobs-v2-m4-cancellation-artifacts-2026-10-06.md) and
[implementation plan](../../crawl-jobs-v2-plan.md). The manifest/index are derived;
the five exports preserve original bytes.
