# M4 immutable image preparation — 2026-09-21

**Image validation: PASS. Publication/CI and execution approval are separate gates.**

The owner requested image preparation and applicable CI/approval work, then
explicitly authorized a scoped M4 commit, push and draft update of existing
PR #10. No merge or real-Redis fixture execution is part of this preparation.

## Selected artifacts

The actual Linux/arm64 evidence and exact-byte offline inputs/plan/recipe are in
[`docs/evidence/m4-image-prep-2026-09-21/`](evidence/m4-image-prep-2026-09-21/README.md).

| Identity | Value |
|---|---|
| Python base | 3.13.15, immutable platform manifest `ad4c34ff79289506e235b40dce75d629e25f226b597a2455804220f037e07531` |
| Redis | 7.4.11, immutable platform manifest `cd953e4e9b4725f0d87a2b170c3d313ad641be5370033b46262e07f8010788a3` |
| Harness daemon-local image ID | `sha256:4b0ca8a3cca08646616bfe6952fc2f2423cb3bd55496ee5e0a1312be223f480a` |
| Redis daemon-local image ID | `sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c` |
| Offline plan SHA-256 | `bf22f79cd2b23e09aa77fa288b70c190c7c04ef24384d5c48ea5d5a885c362c4` |
| Recipe SHA-256 | `2cfb26736d94c9c05188989e8da814272c7a662ac0441d045ff941c5508b078b` |
| Validation report SHA-256 | `fca10b61d20ee67020474d2258fddc4617c5a8cfc9567e0f7867f0bc07307c76` |

Both init and executor checks verified exactly **55 files**, every included file
hash, the canonical source/contract identities, environment isolation and the
real request validator. Init peak cgroup memory was **48,918,528 bytes** under
134,217,728; executor peak was **49,221,632 bytes** under 268,435,456. There were no
OOMs. Every validation container was removed and its absence checked.

Redis was invoked only with `--version`. No Redis server, volume-backed M4
fixture, existing datastore query, seed feed, crawl or rendering activation ran.
The already-running local retained stack was inventoried through Docker metadata;
no command targeted its services or data for mutation.

## Corrections discovered by target validation

1. **Build-context scope:** directory-wide negative patterns accidentally
   re-included unrelated subtrees. The first 858 MB candidate was rejected and
   its tag/image deleted. File-only exclusions now produce the exact intended
   55-file image. The Spider builder allowlist received the corresponding parent-
   directory correction; its runtime COPY rules remain unchanged.
2. **Kernel interfaces:** Docker Desktop's Linux kernel creates nine DOWN fallback
   tunnel devices and a non-interface `bonding_masters` sysfs entry in a new
   `network=none` namespace. The validator now accepts only the finite known DOWN,
   not-IFF_UP fallback names, rejects other/active interfaces, requires no IPv4
   routes and only loopback/rejected-default IPv6 routes, and retains exact
   capability checks. Added positive/negative kernel-observation tests.
3. **Memory margin:** the initial validator passed but peaked at 133,292,032 bytes
   under the 128 MiB init limit. Bundle hashing now streams the identical framing
   one record at a time. All pinned digests remain unchanged, and measured peak
   memory fell to about 47 MiB without increasing the limit.

Separate correctness and security reviewers returned scoped GO for the image
changes. They compared both recorded 55-file inventories against source bytes;
their checks did not themselves start Docker or Redis.

## CI completion work

Existing PR #10's `Test Spider Service` failed in run `35377072774` at the full
90-minute package timeout. Four tests were running and others still awaited
parallel slots. This was an aggregate CI-duration failure, not a Redis latency
measurement or an omitted test requirement.

The complete compiled `-race` V2 inventory now partitions deterministically into
eight disjoint shards. The observed inventory has **470 top-level cases**, split
**67/53/54/70/76/52/49/49**. Each keeps `-race -count=1 -timeout 90m`. Every actual
top-level terminal outcome is required; every full skip name is recorded, including
nested subtests. Only the pre-existing optional native Lua factory root may skip.

The protected `Test Spider Service` job runs even if dependencies fail, rejects
any non-success shard, validates complete same-revision coverage, and runs every
remaining Spider package. No assertion, subtest, race instrumentation or package
is removed. The required-build job also builds/validates the M4 image on Linux/amd64
without starting a Redis server and retains the validation report.

Independent review caught a MEDIUM nested-skip collector gap. It was corrected;
actual-collector regressions and independent re-review now pass. Both reviewers
permit scoped publication. Full protected CI still needs to run on the published
revision; local/synthetic shard checks do not substitute for it.

Local pre-publication checks include 45 harness tests, 19 script tests,
independent digest/bundle checks and actionlint. Exact-index verification and a
scoped credential scan are required before the authorized checkpoint is pushed.

The scoped staged Gitleaks 8.24.3 scan identified two occurrences of the public
SHA-256 for `cj2_retire_legacy_keys.lua` in the image file inventories. Both were
verified against that canonical source and triaged as digest false positives;
no exposed credential was identified and no broad suppression was added.
The narrowed Spider builder also completed its full normal Go suite locally.

## Remaining gate

Record the published revision and all fourteen required GitHub contexts, verify
local/remote/source-image identity, and present the exact first-case artifact
package for owner approval. `plan.json` and the image report explicitly retain
`execution_authorized=false`; this document does not grant run authority.

The current mutable status is maintained in [`crawl-jobs-v2-plan.md`](crawl-jobs-v2-plan.md).
