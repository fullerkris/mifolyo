# C0-ARCH-03 publication and PR #30 CI — 2026-10-17

[PR #30](https://github.com/fullerkris/mifolyo/pull/30) publishes the reviewed
145-path x64-port increment. All 51 reviewed package hashes and 117 preceding
evidence exports were recovered and verified from the repository after temporary
workspaces were cleared.

Initial commit `eba01379d38f82e8c126fff1d2b3fb2c629beb2f` followed a disjoint
fast-forward to main `142334a`. Its [initial CI gate](ci-gate.json) passed.
Main then advanced through PR #28 to `e26e5c1`; synchronization commit
`7ef2041e2844aa34bec54da61c2e8469c1382bac` preserved all 145 candidate blobs.

The [current gate](current-ci-gate.json) verifies head `7ef2041`, tested merge
`e7ea0ef09424ce256760d0ad21c888e2572cf8dc`, tree
`e5b661ce7a5162fea0090382e42165f3c9c89f14`:

- **14 required contexts / 23 jobs**, all passed on attempt 1.
- **256 C0 tests / 51 reviewed source files**, exact runner and test inventory.
- **31 script / 183 harness tests**, digest vectors and **480 race roots**
  (479 passes / existing optional native-Lua skip), including all 11 M4 roots.
- **74-file / 20-recipe** M4 image evidence, stopped admission and cleanup.

[Required Checks](https://github.com/fullerkris/mifolyo/actions/runs/39033514058)
and [Unit Tests](https://github.com/fullerkris/mifolyo/actions/runs/39033514049).
Gate: `c2c0806d14d02be7aed7ab6729e51c7b88c418ddc2869f73e472850192208dff`.
The [manifest](manifest.json) binds 22 exact exports. Owner merge and actual
merged-main verification are now [complete](../m4-c0-x64-main-2026-10-17/README.md)
at `82cd307`. C0-ARCH-04 is local dependent work.
