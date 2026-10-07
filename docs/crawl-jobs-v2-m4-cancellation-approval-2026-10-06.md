# Shared-group cancellation: owner artifact approval — 2026-10-06

**Owner artifact approval recorded; separate execution decision pending.** In
response to the exact pending packet, the owner stated: **“I approve this change.”**
The reviewed prospective bytes were materialized unchanged, including their
original expiry. No reservation or controller invocation occurred.

| Binding | Identity |
|---|---|
| Request | `0deedc21b8fe5d8297deb7858cafc3a1cb21302bf1ac98d8b0c4c87856940003` |
| Approval | `8a1714be42d78fc688b9fc6bf2c69879e2886f4d36ae3c2e9bce139689269fef` |
| Owner decision | `b7c104479f0c64eff307ea797125a5b218814b623e1d85689c76020bef5dfbca` |
| Activation / fresh checks | `32aa66be6c367e43bc4df4b94bdbd0497c6b98f05b6d7758c7dd298a8b75bf9f` |
| Case | `ledger-shared-group-cancellation-v1` |
| Commit | `22317dc21017f6157e5338a23c685a908be0bce4` |
| Operator / platform | `kfuller`, local Linux/arm64 Docker |
| Expiry | **2026-10-06 20:21:00.864 UTC**, unchanged |
| Full-budget start cutoff | Strictly before **20:16:00.864 UTC**, allowing the full 300-second case budget |

Activation rechecked all sixteen request artifacts, all 108 working/committed
source hashes, fourteen green protected checks, PR head/base, both local immutable
image admissions, empty case/check resource listings and the empty owned 0700
evidence destination. The exact request's plan, recipe, images, 300/30/60-second
bounds and one-use/no-retry policy remain unchanged.

The original [proposal](crawl-jobs-v2-m4-cancellation-artifacts-2026-10-06.md),
false template and recording-time pending states remain immutable. The issued
approval is retained separately at:

```text
/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-cancellation-artifact-review-2026-10-06/approval.json
```

**Next:** a separate explicit execution decision, fresh validity/drift checks and
exclusive one-use reservation before the sole bounded invocation. The artifact
decision does not authorize C0 tracing, merging PR #20, application activation or
another case. If the remaining window cannot accommodate the full budget, a fresh
owner-reviewed approval is required; this file's expiry must not be extended.

See the [exact records](evidence/m4-cancellation-approval-2026-10-06/README.md) and
[current implementation plan](crawl-jobs-v2-plan.md). These approval-status updates
are local follow-up documentation; the published/tested source remains `22317dc`.
