# Shared-group cancellation: exact artifact review request — 2026-10-06

**Ready for owner artifact review; approval and execution remain pending.** The
16-artifact private packet binds the published cancellation source, selected
arm64 preparation, exact protected CI, operator, destination and bounds. Its
approval template has `approved=false` and is rejected by the runtime validator.

## Exact proposed decision

| Binding | Identity |
|---|---|
| Request | `0deedc21b8fe5d8297deb7858cafc3a1cb21302bf1ac98d8b0c4c87856940003` |
| Case | `ledger-shared-group-cancellation-v1` |
| Published source | `22317dc21017f6157e5338a23c685a908be0bce4` |
| Source inventory, 108 files | `2221812ca56635055b8acb6271b2fd0fbf27f8d603703171c54fc58bb14062d4` |
| Operator / platform | `kfuller`, local Linux/arm64 Docker |
| Harness / stand-in | `sha256:e57da19e8b99ffbd630545730b3883d00f6088a5f56068ca0b5aed2bba32d0ee` |
| Redis 7.4.11 | `sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c` |
| Plan | `da7ab623673d9b221a8e47a9f4c77fcf983af3636badb90016a03af37542d7c3` |
| Recipe | `b36d4bb7d2a4e657afbcda6a9cc3160b68678b19d590f7fb8c75b1f4de6d7a4f` |
| Unapproved template | `03f1c3f545993e97d4ae2f26263256e950f80bd8ab24fad24fdf4c6d12fa1230` |
| Prospective approved bytes, not materialized | `8a1714be42d78fc688b9fc6bf2c69879e2886f4d36ae3c2e9bce139689269fef` |
| Proposed expiry | **2026-10-06 20:21:00.864 UTC**, epoch ms `1791318060864` |
| Proposal verification | `3f89c4d2fa58de74f73e5daa6de0fa4d1bcbeca78422e4f6035c27a4b072ad80` |

The proposed window starts at 19:21:00.864 UTC and lasts one hour. Pending review
does not extend it. If expiry or source/image/CI drift occurs, preserve this
request as unused and obtain a fresh owner-reviewed packet; do not silently
rewrite its timestamps or turn a broad instruction into execution authority.

Fresh read-only checks confirm all 108 source hashes, the tracked runtime inputs,
both immutable image admissions, fourteen green PR checks and empty case/check
resource listings. The plan recompiles exactly, the recipe matches and the
private evidence directory is empty, owned and mode 0700:

```text
/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-cancellation-execution-2026-10-06/evidence
```

## Scope and limits

Nine fixed calls: A CLAIM; B blocked; A CANCEL and immediate replay; B CLAIM;
historical A CANCEL preserving B's reservation; B START, FINISH and maintenance.
The checks cover five mutations, one capacity denial, two cancellation replays,
60 counters, 46 ACL denials and the complete 90-position inventory. A records zero
starts; B records one synthetic unused start. All external I/O remains zero.

Bounds: 300-second case, 30-second stages/full measurement and separate 60-second
cleanup; four networkless containers, two volumes and six credential roles.
Memory caps remain init 128 MiB, executor/revocation 256 MiB and Redis 528 MiB with
400 MiB maxmemory. This is serial logical-owner evidence, not simultaneous-worker,
per-owner-credential, elapsed-one-day, performance or full-M4 acceptance.

## Required decisions

1. **Owner artifact approval:** approve this exact request and prospective
   approval digest while the proposal is still valid. Only that decision may
   materialize the exact `approved=true` bytes; none have been written now.
2. **Separate execution decision:** authorize the sole bounded invocation after
   fresh validity/source/CI/image/resource checks and exclusive one-use reservation.
   No automatic retry is permitted, including after ambiguous failure.

No approval, execution decision, reservation or controller invocation exists for
this case. Previous approvals remain consumed or expired-unused. This request
does not authorize C0 tracing, merging PR #20, application activation or a
normative amendment.

The [selected evidence index](evidence/m4-cancellation-artifacts-2026-10-06/README.md)
binds the request, false template, scope, fresh preflight and proposal check. All
sixteen exact input records remain private under
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-cancellation-artifact-review-2026-10-06/artifacts/`.
Supporting historical review metrics retain their original bytes; only runtime
plan/recipe/approval shapes go through the strict runtime artifact validator.
