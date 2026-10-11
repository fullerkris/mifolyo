# C0 context/oracle local implementation evidence — 2026-10-09

The [dated report](../../crawl-jobs-v2-m4-c0-context-oracle-2026-10-09.md) describes
the local increment on verified merge `2ff03af`. The [manifest](manifest.json)
binds three byte-identical private-record exports:

- [source-inventory.json](source-inventory.json): all 39 package files, changed/new
  paths, native build inputs/artifacts and context/oracle source-bundle identities.
- [verification.json](verification.json): 149 C0 / 31 script passes, all 113
  baseline IDs preserved, 36 new tests, bounds and remaining review/runtime gates.
- [build-verification.json](build-verification.json): artifact-only native and
  oracle builds, static joins and exact resulting identities.

The current sources are locally verified, uncommitted and unpublished at recording
time. Earlier reviews and source inventories retain their original versions.
No native C0 actor, oracle worker in its runtime sandbox, or tracing experiment
was executed for these records. Source provisioning, runtime admission/calibration,
complete trial assembly and full M4 acceptance remain open.

**Successor:** the [independent-review evidence](../m4-c0-context-oracle-review-2026-10-09/README.md)
records three corrected findings, the 40-file / 161-test source and two final
GO_FOR_CONTINUED_IMPLEMENTATION verdicts. The three original JSON exports and
their manifest above remain byte-identical; their pending-review fields describe
the original implementation checkpoint.
