# PR #26 owner merge and exact main CI — 2026-10-12 local / 2026-10-13 UTC

Owner merge `d8016469a5ffcceeb04329deec4e3dea2c72dd97` preserves reviewed tree
`f5d215c7b0e0656649364d315bcecf87e213b92a`, with parent `ced6e1c` and original
PR head `d6b5bdd`. The [main gate](ci-gate.json) verifies the actual merged
commit's own push workflows, separately from the [prior PR gate](pr-ci-gate.json).

- [Required Checks](https://github.com/fullerkris/mifolyo/actions/runs/38329378950)
  and [Unit Tests](https://github.com/fullerkris/mifolyo/actions/runs/38329379007):
  **14 required contexts / 23 jobs**, all passed on attempt 1.
- [Hosted C0 report](c0-offline-tests.json): **238/238 tests**, all **47 source
  files** and original runner match the reviewed architecture-contract package.
- Eight `shard-N.json` reports: **480 roots**, 479 passes and the existing optional
  native-Lua skip, including all **11 M4 roots**.
- [M4 image validation](image-validation.json): exact **74 source files / 20
  recipes**, amd64 harness, stopped-role admission and cleanup verified.
- Required-job logs also verify **31 script / 183 harness tests** and independent
  digest vectors. Full logs are retained in the private merge workspace; their
  hashes are in the gate.

Gate SHA-256: `e11f449a2483677e3b7642e97df12a4b65cc615af9f263ccbd4e69b6e2aaa111`.
The [manifest](manifest.json) binds 19 exact merge, publication, workflow and
artifact exports. This closes C0-ARCH-02's merge/main checkpoint. C0-ARCH-03's
subsequent local x64 source package has its own tests/review evidence.
