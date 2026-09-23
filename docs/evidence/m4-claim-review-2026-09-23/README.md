# Claim/release independent review identities — 2026-09-23

`initial.json` and `corrected.json` are exact-byte copies of the source inventories
checked by the independent correctness and security reviewers. Each records the
base commit, both case recipes, canonical identities and 71 scoped file hashes.
Documentation/status roll-ups are outside the frozen source inventory.

| Inventory | SHA-256 |
|---|---|
| `initial.json` | `9c9436931f99c3dc31f359aa7b2cc7f443d9bbd7ff8b303b0037a70ea24e5060` |
| `corrected.json` | `6d070f91d06fc4e335a176d2f597b69b4c728aa64ca9bb67738267bd07248717` |

Only `controller.py`, `claim_executor.py` and `test_claim_execution.py` differ
between these inventories. Both reviewers rechecked the corrected bytes and
closed all three non-blocking follow-ups. See the
[consolidated review](../../crawl-jobs-v2-m4-claim-review-2026-09-23.md).

These are source-review identities, not OCI identities or execution approval.
The raw private fixture/credential material is not included.
