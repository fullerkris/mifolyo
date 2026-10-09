# Independent C0 native-admission reviews — 2026-10-08

**GO for continued implementation from both independent reviewers.** All COR-01–03
and SEC-01–02 findings are closed on the final 26-file source snapshot. This is a
source/preparation decision, not native execution authority or full M4 acceptance.

The [manifest](manifest.json) binds ten byte-identical exports:

- [Initial correctness](correctness-initial.json) and [initial security](security-initial.json): NEEDS_REMEDIATION on the original 99-test snapshot.
- [Final correctness](correctness-final.json) and [final security](security-final.json): scoped GO after independent re-review.
- [Final source inventory](source-inventory.json): exact 26-file package and native build/artifact bindings.
- [Local verification](verification.json): 112 tests pass without skips or ResourceWarnings.
- [Artifact rebuild](build.json) and [toolchain](toolchain.json): updated contract input bound; both native ELFs/profiles/BPF unchanged.
- [Combined verdicts](verdicts.json): finding closures, evidence boundaries and the retained nonblocking test-reliability observation.
- [Initial local test-attempt note](local-initial-test-note.json): the observed fixture-teardown failure and its correction; raw output remains in the tool transcript, not a fabricated private log.

The final inventory/verification supersede the initial implementation checkpoint's
source/test state; the historical records are preserved verbatim. Final correctness
includes 15 independent tests and 4,224 static wire joins. Final security includes
15 independent tests, 4,680 BPF simulations, 20 wire joins and a no-leak scoped scan.
These static/simulated observations are distinct from native collector execution.

Security's first final-snapshot suite attempt hit a fixture synchronization timeout
before dispatch assertions; an unchanged confirmation passed all 112 tests. Both
attempts and the unknown-root-cause follow-up are explicitly retained in its report.
They are not described as uniformly passing runs.

Original reports, probes/logs, both read-only source snapshots and both artifact
sets remain private under
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-c0-native-admission-review-2026-10-08/`.

See the [review report](../../crawl-jobs-v2-m4-c0-native-admission-review-2026-10-08.md)
and [package contract](../../../tests/crawl-jobs-v2-observer/README.md).
