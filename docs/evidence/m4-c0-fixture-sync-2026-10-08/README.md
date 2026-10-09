# C0 fixture synchronization evidence — 2026-10-08

**PASS: atomic fault-command publication; nine targeted tests and 113 full-suite
tests pass, with no skips or ResourceWarnings.** Only the fixture/test module changed.

The [manifest](manifest.json) binds six byte-identical records:

- [Controlled reproduction](reproduction.json): empty and partial public commands
  cause peer assertion plus missing acknowledgment; atomic publication is the
  positive control.
- [Before-fix regression](regression-before.json): expected failure in all six fault
  subcases. Its existing raw logs were retained when the recorder was corrected to
  recognize unittest's singular “Ran 1 test” summary; no rerun was substituted.
- [Targeted checks](targeted-tests.json): the nine dispatch tests pass after the fix.
- [Full suite](full-suite.json): 113 tests pass after the fix.
- [Source inventory](source-inventory.json): one changed fixture/test file; all
  production/native sources retain their previous reviewed hashes.
- [Combined verification](verification.json): source/AST/test-inventory preservation,
  unchanged timeouts and the exact limits of historical attribution.

The earlier security-review timeout log remains intact. These probes establish a
concrete race producing the same symptom, but cannot reconstruct that historical
run's unrecorded child stderr or exact interleaving. Earlier independent reviews
keep their original scope; no new independent review is asserted by this follow-up.

Raw probe/test logs and the before-fix source copy remain private under
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-c0-fixture-sync-2026-10-08/`.

See the [fix report](../../crawl-jobs-v2-m4-c0-fixture-sync-2026-10-08.md) and
[retained review evidence](../m4-c0-native-admission-review-2026-10-08/README.md).
