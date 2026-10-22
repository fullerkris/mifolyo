# C0-ARCH-04 whole-archive closure — 2026-10-17

**Local checks pass; final scoped reviews pending.** The
[local manifest](local-manifest.json) binds seven exact exports:

- [Completion inventory](source-inventory.json): 55 package + 3 auxiliary files,
  SHA-256 `2afe7326…`.
- [022 tests](tests-022.json) / [log](tests-022.log) and
  [077 tests](tests-077.json) / [log](tests-077.log): **287/287**, all previous 286 IDs retained.
- [Artifact continuity](artifact-revalidation.json): all six actual retained
  images revalidated under the completion code, with unchanged content records,
  config/layer/archive identities and construction inputs. No rebuild was needed.
- [Source patch](final-source.patch): exact code on base `7ef2041`.

The final constructor check validates every hardlink after parsing, including
unused aliases and forward targets, closing the locally reproduced SEC-06 gap.
The [preceding snapshot](../m4-c0-runtime-images-final-2026-10-17/README.md)
retains the correctness GO and security follow-up evidence unchanged.
