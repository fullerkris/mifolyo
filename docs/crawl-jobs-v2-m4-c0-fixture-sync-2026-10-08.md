# C0 fixture synchronization follow-up — 2026-10-08

**Fixed a reproducible fault-command publication race.** The targeted dispatch
suite passes nine tests, and the full C0 suite passes **113 tests with zero skips
or ResourceWarnings**. The package change is confined to
`tests/crawl-jobs-v2-observer/test_admission_dispatch.py`.

## Confirmed mechanism

The fixture producer used `Path.write_text()` directly on the public `fault`
pathname. Opening that file makes it visible before its complete command is
written. The polling Python peer can therefore read an empty or partial fault
kind, raise `AssertionError: fixture fault`, and exit before creating `fault-ready`.
The producer then reaches its unchanged two-second acknowledgment timeout.

Controlled pre-fix probes paused the producer while the public file contained
either no bytes or the prefix `he`. Both caused the actual benign Python peer's
fixture assertion and the missing-acknowledgment timeout. A complete-before-visible
atomic-publication control succeeded. No `A` or `G` command was dispatched in
these probes, and Docker/native actors were not used.

This deterministically reproduces the retained review timeout's symptom. The
earlier run did not retain the peer's stderr, so its unique historical interleaving
cannot be established retroactively. That original log and both independent review
reports remain unchanged.

## Fix and regression

`PipePair.fault()` now writes the complete command to a sibling `fault.pending`
file, closes it, then atomically replaces `fault` on the same filesystem. The peer
continues polling only the final pathname. The acknowledgment wait, polling interval,
readability assertion and peer implementation are unchanged.

The new regression forces both the empty and partial-write states and asserts that
neither is visible through the peer's `fault` pathname. It exercises all six fault
modes through real Python pipes, verifies acknowledgment, and retains the zero-write
admission-refusal assertion.

- Before the fix, the new test fails in all six subcases at the visibility assertion.
- After the fix, all **nine targeted tests** pass.
- The full suite passes **113 tests**. All prior 112 test IDs remain present.
- AST comparison confirms existing test methods/assertions, the peer script and
  the acknowledgment timeout/polling tail were preserved.
- All other 25 package files, nine native build inputs and eleven native artifacts
  retain their reviewed hashes. No native rebuild was needed.

| Record | SHA-256 |
|---|---|
| Controlled reproduction | `222c78c52c018b76ae572fdd6a40a879f8aebfdb92f416b62d9bd92ef92285d4` |
| Before-fix regression | `bbe64138674f25ff40e2f12633596f79635d3a5d56614196b0ee520a10c0c69e` |
| Targeted checks | `cfd73728ff4fe2f5174ebff623c0193cf710d9373a9986a4bbb8299805ee2c9c` |
| Full-suite result | `4ee29e180fdab6c6da5c41c858afddb02acc31af47a02bb48fc845f441360746` |
| Updated source inventory | `01c5a7318274d26d3868d40ff71205f1193de123914dc9907ffb62eb0f3693ac` |
| Combined verification | `d9826afc2c064de36ea7a223b9bd14da171d209fe9ddd5ff7e9cc5c0a343c129` |

## Scope and status

The confirmed publication race is resolved without changing production admission,
dispatch, cleanup or timeout behavior. Existing independent GO reports retain
their exact reviewed-source bindings; this fixture-only follow-up has its own
local verification and does not claim a new independent review.

The changes are local and unstaged on `feature/crawl-jobs-v2-c0-observer`, based at
`ab5f21a`. Primary-worktree unrelated changes, the original design worktree and all
historical evidence are preserved. Trusted runtime-context acquisition and
oracle/client-closure integration remain the next substantive implementation work;
native experiments and full M4 acceptance remain separate.

See the [evidence index](evidence/m4-c0-fixture-sync-2026-10-08/README.md) and
[independent admission review](crawl-jobs-v2-m4-c0-native-admission-review-2026-10-08.md).
