# Documentation

Keep README files and operational startup runbooks in the repository.
Project plans, status reports, reviews and repository agent definitions are
maintained in Obsidian.

## Start here

- [Project overview](../README.md)
- [Environment boundaries](environments.md)
- [Search UI local staging](search-ui-local-staging.md)
- Individual service README files linked from the project overview

The separate startup and production-preparation branches also contain
`environment-scripts.md` and `production-deployment.md`. Retain those runbooks
when their source changes are integrated; select the deployment configuration
through the existing production reconciliation.

## Build-required Markdown

Three Markdown files are functional inputs and must retain their exact paths
and bytes:

- [`crawl-jobs-v2.md`](crawl-jobs-v2.md): protocol input to generated bundles,
  Docker builds and conformance tests.
- [`DATASET-LICENSES.md`](../services/indexer/nlp/provenance/DATASET-LICENSES.md)
  and [`LICENSE-OVERVIEW.md`](../services/indexer/nlp/provenance/LICENSE-OVERVIEW.md):
  upstream provenance files pinned by the NLP component and verifier.

Dependency requirements, robots controls, third-party licenses, test fixtures
and machine-readable evidence are also retained as functional assets.

## Archives

The Obsidian archive is:

```text
Projects/MiFolyo/Archive/Archive-GitHub Docs/2026-11-15/
```

Open `00 - Archive Index` and the source manifest for the relevant revision.
Every original Markdown path maps to an unedited snapshot with its Git commit,
blob ID in the local inventory, byte length and SHA-256. Changed versions are
stored separately; identical versions share a snapshot.

| Source | Archived revision |
|---|---|
| Main, including merged onboarding PR #33 | `9a5b7625c640c6a4d3678378aed1549821a429b3` |
| C0 PR #35 | `5a91921b85a7fc72cc02346c1271b356673476f1` |
| Production preparation PR #34 | `6adde2c1ecd79f36f86f1bf53a5414a060f5ee89` |
| Separate startup branch | `5de41fa19e82ac673cf77e988d193a476d12f4e8` |

The archive includes the original README and startup-runbook content, the
Crawl Jobs V2 and Spider/Render plans, legacy release/baseline procedures,
feature implementation reports, engineering reviews and all 233 repository
agent definitions.

### Historical evidence and links

Evidence-directory READMEs and their machine-readable manifests retain their
historical scope. Their local links can refer to Markdown moved to Obsidian.
The test-harness READMEs also contain historical review links. Read the original
revision to resolve those links together with the evidence they describe.

For example, create a separate historical checkout at an unused local path:

```sh
git worktree add --detach ../mifolyo-docs-history \
  9a5b7625c640c6a4d3678378aed1549821a429b3
```

Use the PR-specific revision above for its added evidence. Individual reports
can also be read from Git history, for example:

```sh
git show 9a5b7625c640c6a4d3678378aed1549821a429b3:docs/crawl-jobs-v2-plan.md
```

Historical reproduction scripts and manifest checks must run against the
revision they pin, or an exact restored snapshot. Removing a report from the
current checkout does not change its recorded digest or establish a new
acceptance result. The hash-bound protocol intentionally retains its original
historical plan reference; changing that link would change the protocol input.

Repository cleanup removes tracked current-tree documents. Git history and the
archived source revisions retain their original files.
