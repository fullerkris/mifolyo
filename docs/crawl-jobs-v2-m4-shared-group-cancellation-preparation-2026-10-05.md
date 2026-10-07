# Shared-group cancellation: selected arm64 image preparation — 2026-10-05

**PASS; ready for scoped publication and exact-revision CI.** Fresh preparation
explicitly selected `ledger-shared-group-cancellation-v1` on the independently
reviewed 108-file source. Both image checks matched all 74 files and all 20 case
recipes. Stopped-role admission and independent resource-absence checks passed.

This is image preparation. Four metadata containers stayed stopped; the three
executed checks were networkless Python init/executor validation and Redis
`--version`. No acceptance Redis server or cancellation case ran. Full M4 is open.

## Exact artifacts

| Artifact | Identity |
|---|---|
| Case | `ledger-shared-group-cancellation-v1` |
| Base commit | `e80d00538b11c46d021427bcfe1c408e16071c68` |
| Reviewed source inventory, 108 files | `2221812ca56635055b8acb6271b2fd0fbf27f8d603703171c54fc58bb14062d4` |
| Harness / stand-in, Linux/arm64 | `sha256:e57da19e8b99ffbd630545730b3883d00f6088a5f56068ca0b5aed2bba32d0ee` |
| Redis 7.4.11, Linux/arm64 | `sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c` |
| Inputs | `39b666dda815c76a1bcb6e42127544114b69c37a58d22eae2b79d1a4dd8b13ab` |
| Plan | `da7ab623673d9b221a8e47a9f4c77fcf983af3636badb90016a03af37542d7c3` |
| Recipe | `b36d4bb7d2a4e657afbcda6a9cc3160b68678b19d590f7fb8c75b1f4de6d7a4f` |
| Image validation | `0a3b8b3298b961160ed704091b86f46fc30ce748d4f5999c01ce1a7fbf0ce2c9` |
| Independent image postcheck | `75491d78eb7aececb955b84579a16513a0ef7beb66f7793bfd64adb24db7b8cb` |
| Build record | `3e4805bd06420ef2279815a88503fe7d9aa907a45436993cb1990afde216a603` |
| Preparation invocation | `e6c04717a78a06bb2dedd5316bc1442b0505837021f6d4d6dc4dcd8764ac7c0b` |

The [source review](crawl-jobs-v2-m4-shared-group-cancellation-review-2026-10-05.md)
retains both GO decisions and the precise pre-/post-ACL-guard test chronology.
Preparation required no runtime source correction and does not claim a new full
harness or protected-CI run.

## Build and selected checks

The cached immutable Python 3.13.15 arm64 platform manifest was:

```text
docker.io/library/python@sha256:ad4c34ff79289506e235b40dce75d629e25f226b597a2455804220f037e07531
```

Its local image is
`sha256:adc3d531c29fbd69fa9ca49cd8e241c68aaf4e1dd7462be6c2432044b7a39ea8`;
the reviewed multi-platform index remains
`sha256:2325bb286ec344af3e5898cc224b5844e2707ac6e26b1632516fd3edc84a5e26`.
The build used `--network none --pull=false --platform linux/arm64` and the
explicit immutable base override. Its single attempt passed in **1,945 ms**.

The preparer received `--case ledger-shared-group-cancellation-v1` and the exact
image IDs above. Its single invocation passed in **18,250 ms**. The postcheck
independently recompiled the plan from its inputs, matched the unchanged recipe
and source inventory, and checked the nine-step/60-counter/46-denial bounds.

Both Python checks verified the exact 74-file inventory, all 20 recipes, normative
identities, Python 3.13.15 and admitted isolation receipts. Both exited 0 without
OOM and verified their cleanup. Observed memory peaks were **48,668,672 bytes**
for init and **48,644,096 bytes** for executor, below their respective 128 MiB and
256 MiB limits. These are image-check observations, not maximum-shape workload or
latency acceptance.

## Stopped admission and cleanup

Metadata fixture: `e1b45e81c5babbf5e35a57b0e04e48a7`.

Init, executor, Redis and revocation containers passed closed admission while in
`created` state with PID 0. Only init admitted CHOWN; the other roles had no added
capabilities. Images, commands, users, mounts, network and volume attachments
matched the selected recipe.

The independent postcheck found all four exact
`cj2-prestart-check-e1b45e81c5babbf5e35a57b0e04e48a7-<role>` container names and
the corresponding control/data volume names absent. Six fixture/case/image-check
label queries were empty. This absence claim is by names and labels, without
unrecorded by-ID checks. The preparer separately verified removal of its three
image-check containers.

All 108 reviewed source files, the prior 29-path source batch, the isolated
16-path design batch and all 372 unrelated pending files remained intact through
preparation. Concurrent design reviewers used their own read-only snapshot.

## Next gate and evidence

The branch remains `feature/crawl-jobs-v2-shared-group-cancellation`, based on
merged `e80d005`, with local uncommitted source and evidence. Next is scoped
publication and protected CI on the exact new revision. Fresh exact-artifact
approval and a separate execution decision still precede a real invocation.
Baseline main CI and historical approvals do not cover this new case.

The [evidence index](evidence/m4-shared-group-cancellation-preparation-2026-10-05/README.md)
binds seven byte-identical records. The [implementation plan](crawl-jobs-v2-plan.md)
owns current status. Private preflight, originals, build/preparation logs and
postchecks are retained under
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-cancellation-image-prep-2026-10-05/`.
