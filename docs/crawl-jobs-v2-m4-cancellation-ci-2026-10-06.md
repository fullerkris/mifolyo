# Shared-group cancellation: publication and exact-revision CI — 2026-10-06

**PASS: published `22317dc` on draft PR #20; all fourteen protected checks and
downloaded evidence verified.** The checkpoint contains exactly 39 files on
`feature/crawl-jobs-v2-shared-group-cancellation`, based on `e80d005`. All 108
reviewed source hashes and 372 unrelated pending files remain intact.

PR: https://github.com/fullerkris/mifolyo/pull/20

## Revision and evidence bindings

| Binding | Identity |
|---|---|
| Published commit | `22317dc21017f6157e5338a23c685a908be0bce4` |
| Base | `e80d00538b11c46d021427bcfe1c408e16071c68` |
| Published / tested tree | `914a46ab10c8200adec1ce9a70b4b1878569021d` |
| Tested PR merge ref | `f7a5d3ff2d5c372d035f5792e66ff85d2699e48b` |
| Publication receipt | `10360740a1b4840844ad237731bca9ba8f407e88733112b844d52c0a90970254` |
| CI gate | `7920392b1c5bc87c6cd196d3627c5764b78d6d3143949d040e1db27919398241` |
| Downloaded amd64 image validation | `e7ae0caeef12c71b2a28bb4dc54dbc59a5ca9ba9d9764924f4c3aaceb7e81c3f` |
| Source inventory | `2221812ca56635055b8acb6271b2fd0fbf27f8d603703171c54fc58bb14062d4` |

The tested merge ref has parents `[e80d005…, 22317dc…]` and the same tree as the
published head. Local HEAD, remote feature branch, PR head, changed-file list and
single-commit history were checked. `main` still matches the verified base. The
PR remains draft; no merge or real-case execution follows from this publication.

## Verified CI

| Check | Result |
|---|---|
| Strict protected contexts | 14/14 SUCCESS |
| Compiled Go race inventory | 480 roots, each accounted for once across eight shards |
| Go outcomes | 479 passes; sole allowed optional skip `TestJobLuaNativeFactoryParity` |
| Complete M4-prefixed subset | All 11 roots pass once |
| Final-source Python harness | 183 tests, no skips |
| Python script tests | 21 tests, no skips |
| amd64 image evidence | Exact 74-file inventory, all 20 recipes, normative identities and isolation match |
| Forum PHP | 129 completed / 547 assertions: 85 warning results plus 44 warning-free passes |
| Query PHP | 31 completed / 193 assertions: 30 warning results plus one warning-free pass |
| PHP failures / skips | Zero; both Composer audits pass |

Unlike the earlier local full-183 run before the final ACL guard, this CI run
checks **the final published source, including that guard**. Python and PHP
counts are bound to unique raw job-log command blocks and successful step time
windows. The downloaded Go inventory also matches an independent local compiled
listing; no test selector or assertion was removed.

CI's amd64 preparer explicitly selects **claim/release**, while verifying all
20 recipes. Cancellation-specific arm64 preparation remains separately bound to
harness/stand-in `sha256:e57da19e8b99ffbd630545730b3883d00f6088a5f56068ca0b5aed2bba32d0ee`,
plan `da7ab623673d9b221a8e47a9f4c77fcf983af3636badb90016a03af37542d7c3`
and recipe `b36d4bb7d2a4e657afbcda6a9cc3160b68678b19d590f7fb8c75b1f4de6d7a4f`.
See the [selected preparation](crawl-jobs-v2-m4-shared-group-cancellation-preparation-2026-10-05.md).

## Hosted-runner failure chronology

- [Required Checks, run 37366199532](https://github.com/fullerkris/mifolyo/actions/runs/37366199532)
  completes successfully on attempt 4. Attempts 1–3 retained jobs that never
  acquired a hosted runner: runner ID 0, no steps, and GitHub's explicit startup
  failure annotation. Build passed on attempt 2; smoke passed on attempt 3;
  the final required-test job passed on attempt 4.
- [Unit Tests, run 37366199480](https://github.com/fullerkris/mifolyo/actions/runs/37366199480)
  completes successfully on attempt 2. Its initial hosted-acquisition failures
  and dependent Spider gate are retained. Failed-job retries preserved successful
  jobs and the unchanged commit; all eight final shard artifacts were reconciled.

These were CI infrastructure retries, not a Redis acceptance-case retry. The
original attempts remain evidence. No source, test, workflow or protection rule
was weakened. Read-only polling also encountered network/API errors; those did
not establish a test result or trigger an acceptance invocation.

## Next gate

Fresh exact-artifact cancellation approval and a separate execution decision
remain required. No real cancellation case has run. Full M4 remains open and
application V2 remains dormant. The separate C0 implementation worktree is not
part of the published checkpoint or its CI scope.

The [evidence index](evidence/m4-cancellation-ci-2026-10-06/README.md) binds fifteen
exact records. The [implementation plan](crawl-jobs-v2-plan.md) owns current status.
This post-CI report and status update are local follow-up documentation; they do
not replace the published/tested tree. Original logs and staging/scan records
remain private under
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-cancellation-publication-2026-10-05/`.
