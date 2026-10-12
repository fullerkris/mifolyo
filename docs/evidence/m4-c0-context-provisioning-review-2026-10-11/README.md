# C0 pinned-SSH cleanup review evidence — 2026-10-11

The [review report](../../crawl-jobs-v2-m4-c0-context-provisioning-review-2026-10-11.md)
records COR-SSH-001 and its local regression-verified correction to the pushed
`1e95696` snapshot. The [manifest](manifest.json) binds four exact exports:

- [source-inventory.json](source-inventory.json): corrected 45-file package, two
  changed paths, source identities and unchanged native/oracle build inputs.
- [review.json](review.json): finding, reproduction, correction, checks and remaining gates.
- [red-tests.json](red-tests.json): three targeted tests on original transport bytes;
  one reproduced failure and two passing cleanup controls.
- [c0-tests.json](c0-tests.json): all 197 tests pass on the corrected source map;
  all 194 previous IDs retained.

Tests use benign local processes and simulated faults. Independent final reviewer
verdicts and raw ARM64 artifact review remain pending. Original implementation/build
records in the [preceding evidence set](../m4-c0-context-provisioning-2026-10-11/README.md)
retain their original bytes and recording-time scope.
