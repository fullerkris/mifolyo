# M4 D01/D02: independent static/design review — 2026-10-05

**Correctness GO and security GO for scoped implementation preparation.** Both
reviewers independently accepted the frozen design package for a separately
commissioned **C0-only synthetic target/observer/oracle/validator** preparation
and D02 lifecycle/receipt contract work. Neither found a blocking design issue.

This is technical review, not owner authorization to implement or execute the
next scope. D01/D02 and full M4 remain open. The original research, September 25
proposal, normative protocol and current runtime validators retain their bytes.

## Reviewed identities

| Binding | SHA-256 / identity |
|---|---|
| Base | `e80d00538b11c46d021427bcfe1c408e16071c68` |
| Read-only snapshot intake, 1,460 files / 16 design additions | `99e404745aa25412d587ac7f76af56a737bf97d92cb756f31ff966a46e0c1731` |
| Correctness review | `4b45a26298196becef3397ac0d45a71e4a65713df4dddedf4088da12d277199c` |
| Security review | `4cf0fd2cc1ea1e190e25e6538c840f22fc742c3b9ec572dc8b939fd600ca87cd` |
| Combined decisions and follow-up requirement | `283dc44fed5d90de7b053b8d18d387de4f7aa7bb974357fcc16e3c616821aa41` |

The entry point was the [consolidated design package](crawl-jobs-v2-m4-design-gates-2026-10-05.md),
including its four parent interface clarifications. Both reviewers checked all
1,460 snapshot files, all 16 additions and 114 indexed D01 artifacts before and
after. Parent reconciliation independently repeated their hashes. Reviews wrote
only private scripts/logs/reports; no Docker, Redis, target-binary execution,
network acquisition or tracing was performed by either reviewer.

## Correctness evidence and COR-01

Independent checks covered all seven retained image layers and effective ELF
recovery, load-segment offsets, 37 native function bodies and 302 PLT relocation
joins, including the inlined AOF-write site. Canonical COMMIT bytes were reproduced
without running the original generator. The 1,050-descriptor branch, three
first-backpressure descriptors, zero-write replay branches and 276 proposed
calibration trials reconciled. Static/native/semantic/AOF/client-receipt quantities
remain separate; canonical runtime Proto/PC correlation is still unproven.

**COR-01 is a non-blocking clarification carried into the executable-artifact
requirements, not a completed runtime fix.** The calibration draft's shorter
wording about release/kill dispatch must be read under its stricter conservative
pre/post-witness rule. Prepared artifacts must enforce:

1. For resume, close the interval with an independent post-continuation witness
   or a demonstrated upper bound on effective release.
2. For kill, independently confirm actual target stop/exit.
3. Keep dispatch as a separate timestamp; it is never an actual-end witness.
4. Include clock uncertainty in the conservative enclosing bound; reject late,
   absent, ambiguous or incorrectly bound endpoints.
5. Include a negative timing vector where dispatch is timely but effective
   completion is late or missing. Reject that timing claim.
6. Label a conservatively bounded quantity as a bound, not measured slowdown.

The review's illustrative arithmetic shows why: a 15 ms dispatch may precede
effective completion at 50 ms and cannot establish a 20 ms hold bound. That is a
static counterexample, not observer timing evidence. Future executable-artifact
review must verify this requirement before any execution request.

## Security evidence and implementation prerequisites

All **66 static checks** passed, including illustrative Boolean/timeline probes.
They are not implemented-validator or runtime feasibility results. The review
checked six D02 local inputs and eleven pinned upstream source references, all
19 matrix rows, nine exception predicates, nine receipt contracts and 36 grouped
negative-control requirements.

Five informational findings retain concrete work for the next scope:

| ID | Required preparation / later gate |
|---|---|
| SEC-M4-001 | Durable no-regrant ACL lifecycle; explicit deny-all policy and bounded writer proof for the corruption-only candidate; owner sequencing decision; no setup-based readiness after retirement |
| SEC-M4-002 | Independently bound client closure through actual target termination and client retirement, including buffered/late complete ACKs and separate prior-effect accounting |
| SEC-M4-003 | C0-only compact manifest, process/TID identity, reviewed filter generation/instantiation and post-entrypoint zero-capability admission; no C1 fallback |
| SEC-M4-004 | Bounded redacted source-matched refusal diagnostics and full owned-resource/process/transport extinction; disputed ownership blocks affected deletion while other safe cleanup continues |
| SEC-M4-005 | Optional extinction exception stays unavailable under current authority; cleanup supplies neither recovered atomicity/durability nor BOOT or full-M4 acceptance |

The limited prefault-retired corruption-negative route still needs an owner
sequencing interpretation and new helper proof. It cannot cover an authenticated
COMMIT killed in flight. Any proposed teardown-only exception remains unapproved
and unapplied; current acceptance rules continue to require ordinary proof.

## Next gate

The recommended next commission is implementation-only preparation of fixed C0
synthetic artifacts and closed D02 contracts, carrying the requirements above.
Review their exact source, image/profile/manifest identities and validators before
requesting any synthetic execution. C1 privileges, tracing, Redis fault cases and
normative changes need their own decisions; this review does not close them.

The [review evidence index](evidence/m4-design-review-2026-10-05/README.md) binds
three exact records. This work remains local on `docs/crawl-jobs-v2-m4-design-gates`.
The separate cancellation branch has completed selected arm64 image preparation;
that does not establish observer feasibility or D02 fault acceptance. The primary
[implementation plan](crawl-jobs-v2-plan.md) owns broader status; its current
roll-up is maintained in the cancellation worktree.

Private originals, probes and logs are retained under
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-design-review-2026-10-05/`.
