# Claim/release Step 4 independent review — 2026-09-23

**Final decision: correctness GO and security GO for image/CI preparation.**
All three non-blocking follow-ups are corrected and independently re-reviewed.
This does not approve a Redis run or close the broader M4 acceptance matrix.

## Scope and identity

- Branch: `feature/crawl-jobs-v2-claim-release`.
- Base HEAD: `ff2457ebe998707d220e4ce3425aab500c75f5b4`.
- Scope: the current uncommitted Step 2/3 implementation, including untracked
  fixture/executor/tests; runtime sources, related tests, image preparation and
  builder allowlists. Unrelated user work was excluded.
- Both reviewers checked all 71 source inventory entries before and after their
  review. Documentation roll-ups are outside that frozen source inventory.
- [Initial and corrected inventories](evidence/m4-claim-review-2026-09-23/README.md)
  preserve exact-byte file/hash lists.

| Identity | SHA-256 |
|---|---|
| Initial inventory | `9c9436931f99c3dc31f359aa7b2cc7f443d9bbd7ff8b303b0037a70ea24e5060` |
| Corrected inventory | `6d070f91d06fc4e335a176d2f597b69b4c728aa64ca9bb67738267bd07248717` |
| Corrected claim/release recipe | `07d27ddc8218d6c2aa5adf0f05227795a9f097802f50a534585e87206f8a2ad1` |
| Corrected smoke recipe | `8ad72404fff4c09346428ca48607de5d250fd0926f1898ed231f7adc321d3f34` |

The initial recipes were claim `13c6ff7ea55be703f40245756bf2580bd327406800641a3b506c5a119db0b297`
and smoke `862c2f0fdd067135e0197abfd0e82f879cd80c24605d316bf25f0981e7a9def2`.
Only these three files changed during follow-up:

| File | Corrected SHA-256 |
|---|---|
| `tests/crawl-jobs-v2-redis/controller.py` | `edd73e8c9f02eed797feccc9cc79b145509a9ea5590b428fdd6ac9f82a03eea4` |
| `tests/crawl-jobs-v2-redis/claim_executor.py` | `d586ad6b014ca4491fdd6b5b9f48cc5a01e033a1eb87c63f6f55bb542b8144cb` |
| `tests/crawl-jobs-v2-redis/test_claim_execution.py` | `611d2b7a8346522a13f84aebe1bcd77e005a4409a6b79b6d5c3c9232a7eb599e` |

Canonical contract/source/bundle identities are unchanged.

## Initial findings and closure

Both initial reviews returned scoped GO with no blocking defect. The coordinator
addressed each follow-up before seeking review of the final bytes.

| ID | Initial classification | Finding | Final disposition |
|---|---|---|---|
| COR-1 | P2, non-blocking | Private state checks verified counters, but exported step receipts lacked the planned counter measurements/deltas | **Closed:** 33 non-sensitive integer counters now come from verified observed snapshots; each step exports before/after/delta with exact field/type/value validation |
| COR-2 | P3, non-blocking | Cleanup progress was retained only in the eventual final report | **Closed:** journal quiescence, revoker lifecycle, verified credential revocation and each verified resource removal/absence as they finish; journal/callback errors cannot skip subsequent cleanup and invalidate PASS |
| SEC-1 | Informational, non-blocking | A schema-valid failed measurement receipt could carry a different fixture digest and still be retained in an overall FAIL report | **Closed:** compare failed receipt fixture digest with the preceding setup summary before retention; substituted receipts are discarded while teardown continues |

SEC-1 never established a successful-case acceptance bypass. The reproduced
substitution retained a misbound diagnostic while the whole case remained FAIL.
The correction closes that diagnostic-provenance gap without relaxing teardown.

Counter receipts include run/job/group/scope counts, fence and ordinal history,
baseline/generation and zero-start/delivery fields. Exact replay/rejection deltas
are zero; release refunds pending/active capacity without refunding cumulative
creation counts. Their fixed public labels expose no raw keys or identities.

The successful fake lifecycle now has **28 controller actions**, including
**11 cleanup observations**. Stage completion and verified revocation remain
distinct receipts. Missing revocation/removal proof cannot emit its stronger
verified-success event. Journal failure preserves observed cleanup facts and
continues best-effort teardown, but final evidence is invalid.

## Independent verification actually performed

| Reviewer / phase | Verification |
|---|---|
| Correctness, initial frozen scope | 76 harness tests; 21 script tests; targeted Go M4 race (141.227 s); package vet; strict 43-source assembly/bundle/digests; 4,188 snapshot corruptions rejected; bounded SCAN probes; exact 57-file execution allowlist |
| Security, initial frozen scope | 2,325 offline ACL assertions; 34 artifact/approval/input substitutions; six bounded-read attacks; six sensitive-output canary scenarios; eight image-provenance variants; 14 targeted lifecycle/transport tests; journal/file-type checks and scoped sensitive-output scan |
| Correctness, corrected delta | Independent literal 33-counter checks over initial/all nine post-states; 4,491 malformed receipts rejected; all ten valid failure-prefix lengths accepted; real private JSONL writer exercised for all 28 actions; 22 cleanup-journal failure positions/types plus callback/persistent failures; five failed-receipt boundary cases |
| Security, corrected delta | Original fixture-substitution counterexample now rejects at `CLAIM_EVIDENCE_BINDING`; valid partial prefix still retained; 606 malformed-counter variants rejected; four contaminated-controller-receipt probes; private-value checks; seven cleanup-journal failure locations plus combined failures and callback interruption |

Both final reviews found no new actionable issue and verified all corrected
hashes remained unchanged. The observed fake report was 46,758 bytes, below the
2 MiB bound; this is not a target-memory or stage-latency measurement.

Coordinator checks on the corrected scope:

- **79 harness tests PASS**, 160.597 s, including three new regression roots.
- **21 script tests PASS**.
- Targeted Go M4 normal checks **PASS**, 10.621 s.
- Bundle/digest verification and scoped whitespace checks **PASS**.

The corrected delta changes Python evidence/reporting and its regressions, not
the previously race-checked Go/wire/Lua/ACL logic. The final re-review used
focused probes rather than relabeling the earlier full race run as a new run.

## Retained private reproduction material

Root:

```text
/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-claim-review-2026-09-23/
```

- `correctness-astra-01/offline_checks.py`: initial snapshot/SCAN/evidence probes.
- `correctness-astra-01/followup_probes.py`: corrected counter/journal/binding probes.
- `security-independent-7df4a1/review_probe.py`: preserved initial security probes;
  SHA-256 `e6bf5be9cffdfa9e75d5d9bebc9abf9c8be12b69508c7d2eef9c5aa0351af108`.
- `security-corrected-53c08b/review_corrected.py`: separate corrected probes;
  SHA-256 `1353d44d0ebf806e10799060057a9d3f9dd08716e7a875356db0d77c86ad81ad`.

Reviewers used Python `-B` and the approved temporary directory. Reproduction
I/O was fake-backed; no Docker/Redis server, image build/pull, retained datastore,
Git publication or application service was used by these reviews.

## Step 5 handoff

Prepare and validate immutable images from these corrected bytes, then generate
the claim-specific plan/recipe using
`scripts/prepare-crawl-jobs-v2-images.py --case ledger-claim-release-v1`.
The current default CI preparation invocation emits smoke artifacts; its image
checker validates both recipes, but that does not turn a smoke plan into a claim
plan. Preserve the exact selected-case artifact identity.

Complete the authorized scoped publication and exact-revision protected checks.
Target Redis ACL semantics, actual stage/cleanup time bounds, memory, AOF and
container behavior remain unmeasured for the claim case. The fake lifecycle's
28,385 facade calls are not timing evidence. A fresh exact-artifact approval and
separate execution request are still required before an actual run. The earlier
consumed smoke approval and old image/recipe identities cannot be reused.
