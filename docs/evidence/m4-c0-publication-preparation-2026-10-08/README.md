# C0 publication-preparation evidence — 2026-10-08

The [dated report](../../crawl-jobs-v2-m4-c0-publication-preparation-2026-10-08.md)
describes the local 124-path candidate on `feature/crawl-jobs-v2-c0-observer`, based
at `ab5f21a`. The [manifest](manifest.json) binds three byte-identical private-record
exports:

| Export | Meaning | SHA-256 |
|---|---|---|
| [publication-scope.json](publication-scope.json) | 118 hashed source/history/integration paths plus six named closeout paths | `e9b5a368c73e64ac7028cdc975b32b254985db6436eec6774eb4601b6d8a3ffa` |
| [verification.json](verification.json) | 113 C0 / 31 script passes, digest/workflow checks and preserved broader-suite timeout | `cb1edb12de0f5606cc2e55996d42c9aa2bb39ef857d3a947bb1004243a868ab6` |
| [c0-offline-tests.json](c0-offline-tests.json) | Exact discovered/started/passed IDs and package/runner hashes from the new runner | `be5850259e9b03e5bcecc1c48ded2238aca83575d54397601ac4a0dd9cef11c6` |

The local report's revision is `local`; exact hosted-CI revision evidence remains
pending publication. The additional Redis-harness recheck exceeded its private
300-second budget; it does not supply a new full-suite result. The unchanged
183-test harness retains its exact-main CI evidence, whose recorded duration is
2,496.716 seconds. The full disposition and absent partial-output qualification are
included in `verification.json`.

The source scope excludes its own closeout contents to avoid recursive hashes.
The private final handoff binds all 124 final file hashes, including this index,
dated report, manifest and exports. Historical design/source/review records retain
their original bytes and scopes. The fixture successor and new CI integration are
not silently folded into the older independent GO verdicts. This is publication
preparation, not execution approval or full M4 acceptance.
