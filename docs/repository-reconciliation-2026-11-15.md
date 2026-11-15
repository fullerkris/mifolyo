# Repository preservation and reconciliation — 2026-11-15

## Current position

The reviewed October 24 C0 candidate was recovered intact on the original Linux
workstation and checkpointed locally. Main remains at
`e8a0a415ac2b370319cd57f4c5262ce616a536fb` (PR #32).

| Local checkpoint | Commit | Scope |
|---|---|---|
| `feature/crawl-jobs-v2-c0-native-calibration` | `358173c5784d5b1ef7717ef8e78aae094fe8c647` | Exact 322-path reviewed preflight/adapter candidate |
| `feature/production-deploy-2026-11-01` | `6adde2c1ecd79f36f86f1bf53a5414a060f5ee89` | Exact nine-file November 1 production preparation |

Both commits have parent `e8a0a41`. Their committed path sets, contents and
executable modes were checked against the preserved inventories. This document
and the mutable README/plan updates are a subsequent documentation-only change.
The October 24 evidence retains its original recording-time status.

The owner requested local-only work and explicit approval before changes to the
remote repository. No new checkpoint has been pushed and no server files were
changed. C0 runtime remains owner-paused since October 27; separate test-server
work was deferred on November 1 and native ARM64 calibration remains deferred.

## Verified preservation snapshot

Location on this workstation:

```text
/var/home/kfuller/Documents/MiFolyo-Backups/20261115T183759Z-reconciliation/
```

The private, same-filesystem snapshot contains the entire repository, `.git`
including unreachable objects/reflogs/stash/worktree metadata, both existing
worktrees, ignored images/evidence and local dependencies. It is a local recovery
copy, not an off-device backup.

| Item | Verification |
|---|---|
| `repository.tar` | 1,649,971,200 bytes; 11,874 regular files, 1,189 directories including root, seven symlinks |
| Archive SHA-256 | `e95280b12fa5341571c61fd8e4d9e0d4611d468322e7c706de08c05cd7a84871` |
| `filesystem-manifest.json` SHA-256 | `8c1e822f17cd7c2751062e0d39ad7836dbb321fe21b45f5bf672c4aa269db81b` |
| Content and metadata | All archived file hashes/types/modes/ownership and symlink targets match; GNU tar comparison passes |
| Source stability | Before/after inventories identical; zero source changes during capture |

ACLs and extended attributes are retained. `verification.json`, `SHA256SUMS`,
the preservation script, Git audit captures and the earlier test/integrity
reports accompany the archive. Later checkpoint records are under `checkpoints/`.
The original `SHA256SUMS` describes the initial snapshot package; later
checkpoint records are separately inventoried.

Recovery should use a separate empty destination and verify these hashes first.
The nested production worktree's `.git` file and registrations contain original
absolute paths; restoring elsewhere requires checking/repairing those worktree
links before using it. A refs-only Git bundle cannot replace this snapshot.

## C0 identity and verification

- Exact candidate commit: `358173c5784d5b1ef7717ef8e78aae094fe8c647`.
- Tree: `f52c88103a5303fc6d70b7d6d88020426a8df4e0`.
- Candidate inventory: `out/c0-runtime-adapter-2026-10-24/final-candidate.json`.
- At the exact checkpoint, all 322 current/preserved candidate paths, 63 reviewed source/script bindings,
  444 review inputs, 816 retained files and three evidence manifests verified.
- Fresh **338/338 tests pass under umasks 022 and 077**; both reports are
  byte-identical to the original reviewed reports.
- Source/current documentation whitespace checks pass. The full staged check
  reports **110 trailing-whitespace diagnostics in immutable historical evidence**
  such as patch context lines, logs and Markdown review line breaks. Those bytes
  were preserved to retain their manifest bindings.

Verification records are `checkpoints/c0.json` and
`checkpoints/c0-staged-whitespace.txt` in the preservation directory.
The local checkpoint has no new hosted CI result. Native runtime admission,
calibration and full M4 acceptance remain open; source-test success does not
complete those gates.

The subsequent mutable-status update changes `docs/crawl-jobs-v2-plan.md`;
validate the full 322-path inventory against commit `358173c`, rather than the
later branch tip. All 63 reviewed source/script bindings and immutable evidence
remain unchanged by the documentation update.

## Branch and recoverability audit

The pre-change audit covered eight local branches, 16 live remote branches,
33 cached remote-tracking branches excluding `origin/HEAD`, 23 tags, worktrees,
stash, reflogs, unreachable objects and all 33 PR records.

- Every local branch commit was already reachable through an advertised remote
  branch or tag before the two new checkpoints.
- All 16 live heads matched cached refs. All 17 stale remote-tracking tips have
  exact remote archive tags; all 22 archive tags and `v0.0.2-rc.1` match remotely.
- Integrated branches were checked using tree/path equality where squash merges
  obscure ancestry. Voting patches `2e3eac2`/`18ff77d` match their
  `631d830`/`e2e0621` replays in merged PR #31.
- The local runtime-images branch and C0 backup branch at `babb218` are preserved
  in the remote runtime-images history; the local runtime-images branch is five
  commits behind its remote, without divergence.
- Stash `2475fda26223c43485b933089274476e497be3ab` contains the earlier 120-path
  preflight fallback, exactly matching `out/c0-main-sync-2026-10-24/before/`.
  Its three commits were the only locally reachable commits absent from
  advertised remote heads/tags before checkpointing.
- All 119 reflog entries were checked; there are no additional reflog-only
  commits. Three missing temporary-worktree registrations have clean surviving
  indexes and preserved recorded commits.
- `git fsck` reported no corruption. Four unreachable commits have trees matching
  reachable history. Three unreachable trees and 22 blobs are included in the
  snapshot; seven blobs have exact filesystem copies, while 15 appear to be
  intermediate C0 versions without exact copies in the inspected files.

The two genuinely pending published branches are
`feature/search-ui-onboarding` at `b0ed5a2` (open PR #33, 32 changed files) and
`feature/environment-startup-scripts` at `5de41fa` (15 changed files, no PR).
PR #33 had 23 successful checks and CodeRabbit skipped at inspection.

## Production reconciliation

Three source states must be accounted for:

| State | Evidence and topology |
|---|---|
| Local November 1 preparation | Exact nine-file checkpoint `6adde2c`, tree `8721e67b3dfcad741bda31a4cf2f2bc8c80308b5`; adopts retained MongoDB, uses per-service environment files, a PostgreSQL backup container and `ProductionCommunitySeeder` |
| Published November 2 startup scripts | Cached commit `5de41fa19e82ac673cf77e988d193a476d12f4e8`; provisions a MongoDB store/query user and forum queue worker, uses a different environment contract and `production-env.sh` |
| Production host checkout | Earlier read-only inspection found main with one modified tracked file and eight untracked files; the November 11 notebook describes operational private HTTPS and `production.sh` |

The local preparation and published startup branch overlap on
`scripts/docker/production.Caddyfile`, `production.compose.yml` and
`production.env.example`. **All three files differ byte-for-byte.** Neither
version should be selected as the deployed source merely because its filename
matches. The comparison and complete overlap diff are retained locally as
`checkpoints/production-comparison.json` and `production-overlap.patch`.

The observed host-only delta was:

```text
docs/environments.md
docs/production.md
scripts/docker/php-production.Dockerfile
scripts/docker/production-postgres-init.sh
scripts/docker/production.Caddyfile
scripts/docker/production.compose.yml
scripts/production-backup.sh
scripts/production-forum-init.php
scripts/production.sh
```

No host files were modified. A source-download request was denied and was not
retried; deployed bytes are not in the workstation snapshot and have not been
compared. Complete that comparison from an owner-provided local source export.
The dated November 1 preparation report describes its original pre-cutover
state; it does not override the notebook's later operational status.

## Onboarding-basics recovery

The notebook records the October 27 guidelines/interests/topic-suggestion work as
locally verified but uncommitted on `feature/search-ui-onboarding-basics`, based
on `b0ed5a2`. It is absent from this repository and the advertised remote.

A targeted search under this workstation's `Documents/Git_Projects` and
`Documents/Git_Pulls` found no onboarding-basics artifacts or
`TopicCommunitySeeder.php`, with no filesystem errors. An earlier read-only
probe of the production/legacy checkouts and retained release directory also
found no marker files. These are bounded searches, not evidence the work is lost.

The notebook's historical import provenance names the macOS checkout
`/Users/kfuller/Documents/Git_Projects/mifolyo_test`; the November 2 startup
record also identifies macOS/ARM64 development. The exact October 27 checkout
location remains unverified. Next input is a local copy/export of that original
development worktree, including untracked source and its test/source-binding
artifacts.

## Remaining delivery

1. Obtain explicit approval before publishing the local C0/documentation and
   production-preparation feature branches. Any PR, merge or cleanup is a later
   decision.
2. Compare an owner-provided local export of the deployed source, then choose
   the production configuration and migration approach.
3. Recover/checkpoint onboarding-basics from the original development source.
4. Select the next development milestone; C0's retained resume prerequisites
   are in the [V2 plan](crawl-jobs-v2-plan.md).

Obsidian authority: `Projects/MiFolyo/MiFolyo - Repository Preservation and
Reconciliation - 2026-11-15.md`, with links from the roadmap, C0 admission,
production, Search UI, work journal and documentation index. Updates are made
through MCPVault.
