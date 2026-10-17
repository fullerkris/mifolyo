# C0-ARCH-04 final validator follow-up — 2026-10-17

**Local verification complete; whole-archive hardlink follow-up required.** The
[local manifest](local-manifest.json) binds seven exact exports:

- [Source inventory](source-inventory.json): 55 package + 3 auxiliary files,
  SHA-256 `83288b8f…`. Only the host archive validator and its regression file
  differ from the preceding `14ac223d…` snapshot.
- [022 tests](tests-022.json) / [log](tests-022.log) and
  [077 tests](tests-077.json) / [log](tests-077.log): **286/286**, retaining all 283 prior IDs.
- [Artifact revalidation](artifact-revalidation.json): final-code checks of all
  six retained images, with identical config/layer/archive identities and
  **byte-identical content records**. Image-construction inputs are unchanged;
  no rebuild or engine command was needed.
- [Final source patch](final-source.patch): reconstructs the source on `7ef2041`.

The follow-up checks required-loader execute intent, rejects link-bearing or
inconsistent hardlinks, and joins normalized root aliases to the same root
observation/duplicate predicates. The
[preceding evidence](../m4-c0-runtime-images-remediation-2026-10-17/README.md)
preserves the actual construction records, roots/cleanup, and original re-review
findings. Its source maps retain their recording-time validator/test hashes;
the explicit two-file continuity record joins them to this snapshot.

The [security review](first-security-review.md) closes SEC-01–05 and verifies
loader execute intent, but identifies **SEC-06**: an unused hardlink can bypass
the lazy inode-metadata consistency check. Its [manifest](first-security-manifest.json)
binds 21 unchanged exports. All six actual images still revalidate. The
[correctness review](first-correctness-review.md) returns GO with COR-001–006
closed; its [manifest](first-correctness-manifest.json) binds 18 exports.
The [whole-archive follow-up](../m4-c0-runtime-images-completion-2026-10-17/README.md)
adds global hardlink validation and passes 287 tests. No combined GO is claimed
for this superseded snapshot.
