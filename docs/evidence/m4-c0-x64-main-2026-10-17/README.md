# PR #30 owner merge and exact main CI — 2026-10-17

PR #30 was owner-merged at **2026-10-17 13:44:52 UTC** as
`82cd307899a1b3eab7f97fe660b1121be763f2c1`, parent `e26e5c1`. It preserves the
published `7ef2041` tree exactly: `e5b661ce7a5162fea0090382e42165f3c9c89f14`.

The [main gate](ci-gate.json) verifies the merge's own push workflows:

- [Required Checks](https://github.com/fullerkris/mifolyo/actions/runs/39037832313)
  and [Unit Tests](https://github.com/fullerkris/mifolyo/actions/runs/39037832248):
  **14 required contexts / 23 jobs**, all passed on attempt 1.
- **256 C0 tests / all 51 reviewed source files**, exact runner and inventory.
- **31 script / 183 harness tests**, digest vectors and **480 race roots**
  (479 passes / existing optional native-Lua skip), including all 11 M4 roots.
- **74-file / 20-recipe** M4 image validation, stopped admission and cleanup.

Gate SHA-256: `fa674a604010ca9f95743e1964cad78a539aaf4e53b0887153a21354375e914a`.
The [manifest](manifest.json) retains 17 exact merge, workflow and artifact exports.
The [prior PR gate](prior-pr-ci-gate.json) remains separate. Initial pending fields
in the merge-record file retain their recording-time meaning.

C0-ARCH-03's publication/merge/main checkpoint is closed. C0-ARCH-04 remains local
reviewed work based on the equivalent published PR tree; its hosted image matrix
will run after its own source publication.
