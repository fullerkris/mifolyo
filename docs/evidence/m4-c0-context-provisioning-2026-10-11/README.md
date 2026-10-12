# C0 pinned-SSH tooling evidence — 2026-10-11

The [implementation report](../../crawl-jobs-v2-m4-c0-context-provisioning-2026-10-11.md)
records local tooling verification on actual merged main `64c98a7`. The
[manifest](manifest.json) binds five byte-identical private-record exports:

- [source-inventory.json](source-inventory.json): all 45 package files, five
  changed/five added paths, source bundles and fresh native/oracle build identities.
- [verification.json](verification.json): 194 C0 / 31 script passes, all 161
  baseline IDs retained, selected tooling-only scope and explicit live-host gates.
- [build-verification.json](build-verification.json): artifact-only builds and
  static joins; no native actor or real host collector execution.
- [c0-tests.json](c0-tests.json): the exact 194-test inventory and 45-file source map.
- [merged-main-ci-gate.json](merged-main-ci-gate.json): independent verification of
  PR #24's actual merge/main workflows, including the 161-test baseline and retained
  image/race/integration/audit evidence.

Private credential fixtures are temporary, excluded from these exports and never
installed on a host. The source snapshot has not received a new independent review
or publication. Operator host identification/installation and live SSH/runtime
admission remain open. Earlier records retain their original bytes and scope.
