# Page Vote Search Ranking Implementation Plan

**Status:** Proposed and deferred
**Owners:** Forum and search teams
**Activation:** Disabled until the product decisions and rollout gates in this document are approved

## Goal

Allow an authenticated user to upvote or downvote an indexed web page from a
search result. The aggregate page vote should provide a bounded quality signal
that can move the page up or down among similarly relevant results.

This feature must preserve search availability when the forum or vote
projection is unavailable. Missing, invalid, stale, frozen, or suppressed vote
data must have neutral ranking influence.

## Scope

### In Scope

- One page vote per eligible user and canonical web page.
- Upvote, downvote, change-vote, and remove-vote operations.
- Aggregate counts and the current viewer's vote on search results.
- A durable projection of page-vote aggregates into the search database.
- A bounded page-quality signal in the existing search ranking pipeline.
- Moderation, rate limiting, auditability, reconciliation, and rollout metrics.

### Non-Goals

- Reusing or converting votes on forum posts and comments.
- Making vote writes directly to the search index.
- Calling the forum service once per result during search ranking.
- Fetching or validating the remote page as part of a vote request.
- Allowing page popularity to replace textual relevance.
- Query-specific or topic-specific votes in the first version.

## Current Architecture

The current search path is implemented in
`services/query-engine/app/Http/Controllers/QuerySearchController.php`.

1. The Query Engine finds candidate URLs in MongoDB's `words` collection.
2. It groups matching terms by URL and sums their TF-IDF `weight` values.
3. It joins the `pagerank` collection by canonical URL.
4. It normalizes TF-IDF and PageRank across the complete candidate set.
5. It calculates:

   ```text
   baseScore = 0.60 * normalizedTfidf
             + 0.40 * normalizedPageRank
   ```

6. It orders results by:

   ```text
   matched query terms DESC
   baseScore DESC
   canonical URL ASC
   ```

The Forum Engine owns community identity, bearer-token authentication, and the
existing polymorphic `votes` table. Those votes apply only to posts and
comments. A page vote has different semantics and must remain a separate domain
model.

Canonical page identity is defined by `docs/url-canonicalization-v1.md` and
implemented for the forum in
`services/forum-engine/app/Support/SourceUrlNormalizer.php`.

## Architecture Decision

### Source of Truth

The Forum Engine owns page-vote truth in PostgreSQL because it already owns:

- User identity and authentication.
- Account standing and moderation state.
- Write rate limits.
- Community vote behavior and audit context.

The Query Engine must not activate its separate scaffolded user table for this
feature and must not read Forum PostgreSQL directly.

### Search Projection

A search-owned projector copies aggregate page-vote signals into a MongoDB
`page_vote_scores` collection. Search ranking reads this local projection with
a MongoDB `$lookup`.

The projection is eventually consistent. Vote mutation responses provide
read-your-write feedback to the voting user, while ranking changes appear after
the projector processes the committed aggregate version.

```text
Browser
  -> Query Engine same-origin proxy
  -> Forum Engine page-vote API
  -> PostgreSQL vote transaction and outbox event
  -> Search-owned projector
  -> MongoDB page_vote_scores
  -> Query Engine ranking lookup
```

Search must continue with a neutral vote signal if any component after the
Forum transaction is delayed or unavailable.

## PostgreSQL Data Model

### `web_pages`

Stores the page identity and authoritative aggregate.

| Column | Purpose |
| --- | --- |
| `id` | Internal primary key. |
| `canonical_url` | Exact canonical V1 HTTP/HTTPS URL. |
| `url_id` | SHA-256 V1 URL identity. |
| `canonicalization_version` | Canonicalization contract version. |
| `source_domain` | Canonical host for moderation and analysis. |
| `source_path` | Canonical path for moderation and analysis. |
| `upvote_count` | Raw upvote count shown to users. |
| `downvote_count` | Raw downvote count shown to users. |
| `eligible_upvote_count` | Upvotes currently eligible for ranking. |
| `eligible_downvote_count` | Downvotes currently eligible for ranking. |
| `ranking_signal` | Precomputed bounded quality signal. |
| `ranking_algorithm_version` | Version used to compute the signal. |
| `aggregate_version` | Monotonic version used by the projector. |
| `ranking_state` | `active`, `frozen`, or `suppressed`. |
| timestamps | Creation and last-update times. |

Required constraints:

- Unique `(canonicalization_version, url_id)`.
- Compare the exact canonical URL when a URL ID already exists. Fail closed on
  an identity mismatch.
- Counts cannot be negative.
- `ranking_signal` must be finite and within `[-1, 1]`.

### `page_votes`

Stores one user's vote on one canonical page.

| Column | Purpose |
| --- | --- |
| `id` | Internal primary key. |
| `web_page_id` | Foreign key to `web_pages`. |
| `user_id` | Foreign key to the Forum user. |
| `value` | `1` for upvote or `-1` for downvote. |
| `ranking_eligible` | Whether this vote currently affects ranking. |
| `eligibility_policy_version` | Policy used for the eligibility decision. |
| timestamps | Vote creation and last-change times. |

Required constraints:

- Unique `(user_id, web_page_id)`.
- Check `value IN (-1, 1)`.
- Preserve change history in the audit log or outbox.

### `integration_outbox`

Stores durable aggregate-change events in the same PostgreSQL transaction as
the vote. Each event needs a unique event ID, page identity, aggregate version,
event type, payload, creation time, and delivery state.

Do not use Redis Pub/Sub as the only delivery mechanism. A dropped event would
silently leave search ranking inconsistent.

## API Contract

The Forum Engine should expose dedicated page-vote endpoints rather than add a
`page` type to the existing post/comment `VoteController`.

```text
PUT    /api/page-votes/{url_id}
DELETE /api/page-votes/{url_id}
POST   /api/page-votes/lookup
```

### Create or Change Vote

```json
{
  "canonical_url": "https://example.com/article",
  "value": 1
}
```

The Forum Engine must:

1. Authenticate the user.
2. Canonicalize `canonical_url` on the server.
3. Recompute its URL ID.
4. Verify the computed ID matches `{url_id}`.
5. Lock the page aggregate and the user's existing vote.
6. Insert, update, or idempotently retain the vote.
7. Recompute counts and the ranking signal.
8. Increment `aggregate_version`.
9. Append an outbox event in the same transaction.

Example response:

```json
{
  "url_id": "<64-character-v1-url-id>",
  "canonical_url": "https://example.com/article",
  "viewer_vote": 1,
  "upvote_count": 12,
  "downvote_count": 3,
  "ranking_signal": 0.18,
  "aggregate_version": 27
}
```

`DELETE` removes the viewer's vote and emits a new aggregate version. It should
be idempotent when no vote exists.

`lookup` accepts a bounded list of canonical page identities and returns all
visible aggregates plus the authenticated viewer's vote. This avoids one API
request per search result.

The Query Engine should expose same-origin proxy endpoints and forward the
Forum authentication context. It must not calculate or trust client-supplied
counts or ranking signals.

## Ranking Signal

Raw net score (`upvotes - downvotes`) must not be used directly. It gives a
single early vote too much influence and makes brigading inexpensive.

The first version should use a symmetric Bayesian prior and uncertainty
penalty. For eligible upvotes `U` and eligible downvotes `D`:

```text
a = U + 5
b = D + 5

mean = (a - b) / (a + b)

sigma = 2 * sqrt(
  (a * b) / ((a + b)^2 * (a + b + 1))
)

quality = sign(mean) * max(0, abs(mean) - 1.645 * sigma)
```

Expected properties:

- No votes produce `0`.
- A small number of votes have little or no ranking influence.
- Consistent votes gradually gain influence.
- Mixed votes remain near neutral.
- Upvotes and downvotes are symmetric.
- The result remains bounded near `[-1, 1]`.

Store the precomputed signal and its algorithm version. Do not reproduce this
floating-point calculation independently in PostgreSQL, the projector,
JavaScript, and the MongoDB query.

## Search Ranking Integration

Create the MongoDB collection `page_vote_scores` with documents shaped like:

```json
{
  "_id": "https://example.com/article",
  "url_id": "<64-character-v1-url-id>",
  "canonicalization_version": 1,
  "upvote_count": 12,
  "downvote_count": 3,
  "eligible_upvote_count": 10,
  "eligible_downvote_count": 2,
  "ranking_signal": 0.18,
  "ranking_state": "active",
  "ranking_algorithm_version": "page-quality-v1",
  "aggregate_version": 27,
  "computed_at": "<timestamp>"
}
```

Use canonical URL as `_id` because it matches `words.url`, `metadata._id`, and
`pagerank._id`.

The Query Engine should join this collection before pagination, validate and
clamp the signal, and calculate:

```text
baseScore = 0.60 * normalizedTfidf
          + 0.40 * normalizedPageRank

finalScore = baseScore + voteWeight * pageVoteQuality
```

Recommended initial settings:

- `voteWeight = 0` during shadow evaluation.
- `voteWeight = 0.05` for the first live experiment.
- Maximum initial production weight of `0.10` without a new review.
- Missing, invalid, non-finite, stale, frozen, or suppressed signals become
  `0`.
- Do not normalize page-vote quality against each query's candidate set.

Keep the existing primary sort boundary:

```text
matched query terms DESC
finalScore DESC
canonical URL ASC
```

This lets votes reorder similarly relevant pages without allowing popularity
to substitute for query relevance. Changing that boundary requires a separate
ranking-quality decision.

## Projection and Reconciliation

The projector must:

- Consume outbox records at least once.
- Apply an event only when `aggregate_version` is newer than the projected
  document.
- Safely ignore duplicate and out-of-order events.
- Reject canonical URL, URL ID, or canonicalization-version mismatches.
- Retry transient MongoDB failures with bounded backoff.
- Expose projection lag, last applied event, and failure metrics.
- Support a complete reconciliation/export from Forum PostgreSQL.
- Repair missing and stale projection documents without changing vote truth.

The concrete transport can be selected during implementation. An authenticated
incremental outbox endpoint polled by a search-owned worker is acceptable for
the first version. Direct Forum writes to search MongoDB are not.

## Search Result UI

Extend the search result component under
`services/query-engine/resources/views/components/search-result.blade.php` to
show:

- An accessible upvote button.
- An accessible downvote button.
- Aggregate counts or net score, depending on the approved product decision.
- The viewer's selected state.
- Pending and disabled states during mutation.
- A login prompt for unauthenticated users.
- A degraded state when Forum reads or writes are unavailable.

Use optimistic updates only when failures restore the authoritative previous
state. Batch aggregate and viewer-state reads for the full result page.

Authentication should move away from long-lived bearer tokens in
`localStorage` before broad release. Prefer an HttpOnly, Secure, SameSite cookie
or another reviewed same-origin session arrangement. Cookie authentication
requires CSRF protection for mutations.

## Abuse and Moderation Controls

- Require authenticated users.
- Permit only one active vote per user and canonical page.
- Initially use one eligible account as one ranking unit; do not weight votes
  by user level.
- Define ranking eligibility separately from the ability to express a vote.
- Consider verified email, minimum account age, healthy standing, and active
  sanctions in the eligibility policy.
- Rate-limit vote creation, direction changes, and removal by user, IP range,
  page, and domain.
- Record vote reversals and retain an audit trail.
- Recompute aggregates when a user's eligibility changes.
- Allow moderators to freeze or suppress ranking influence without deleting
  vote history.
- Monitor domain-level vote bursts, new-account concentration, repeated
  reversals, projection lag, and aggregate divergence.
- Never expose voter identities in the search projection.
- Never perform a network request to the submitted page while processing a
  vote.

## Delivery Phases

### Phase 0 - Product and Architecture Approval

- Decide what a page vote means.
- Approve account eligibility and moderation policy.
- Approve displayed counts and vote-removal behavior.
- Approve indexed-only voting and the initial ranking weight.
- Record the service-ownership decision in an ADR.

### Phase 1 - Forum Domain and API

- Add `web_pages`, `page_votes`, and `integration_outbox` migrations.
- Add models, relationships, request validation, policies, and a dedicated
  page-vote service.
- Implement transactional aggregate updates and outbox writes.
- Add mutation, removal, and batch lookup endpoints.
- Add page-vote-specific rate limits and audit events.

### Phase 2 - UI Without Ranking Influence

- Add Query Engine proxy endpoints.
- Add batched aggregate/viewer-state loading.
- Add upvote/downvote controls to search results.
- Display votes while `voteWeight` remains `0`.
- Measure usage, error rates, and abuse patterns.

### Phase 3 - Durable Search Projection

- Add the search-owned outbox consumer/projector.
- Add the `page_vote_scores` collection, schema validation, and indexes.
- Add replay, reconciliation, lag monitoring, and failure recovery.
- Backfill existing page aggregates.

### Phase 4 - Shadow Ranking

- Join vote signals in the complete candidate pipeline.
- Calculate shadow scores without changing result order.
- Log aggregate ranking movement without storing user queries or other
  unnecessary personal data.
- Compare relevance and abuse metrics against the current ranking.

### Phase 5 - Controlled Activation

- Enable a reviewed nonzero weight behind configuration or a feature flag.
- Start at `0.05` or lower.
- Monitor click quality, result displacement, abuse, and projection health.
- Roll back to `voteWeight = 0` on any ranking or integrity concern.

## Test Plan

### Forum Tests

- Authentication and expired-token rejection.
- Equivalent URLs map to one page identity.
- URL ID and exact canonical URL mismatch rejection.
- One vote per user and page.
- Duplicate mutation idempotency.
- Upvote-to-downvote counter changes.
- Vote removal and repeated removal.
- Concurrent writes preserve aggregate integrity.
- Eligibility and standing changes recompute ranking counts.
- Vote and outbox event commit or roll back together.
- Rate limiting and moderator freeze/suppression behavior.
- Existing post/comment vote tests remain unchanged.

### Ranking Formula Tests

- No-vote neutrality.
- Symmetry between upvotes and downvotes.
- Low-count uncertainty protection.
- Monotonic movement as consistent votes increase.
- Bounded output and non-finite input rejection.
- Versioned deterministic output.

### Projector Tests

- Duplicate and out-of-order event handling.
- Retry after MongoDB failure.
- Newer aggregate versions replace older versions only.
- Identity/version mismatch rejection.
- Reconciliation repairs missing or stale documents.
- Removing the final vote produces a neutral projection.

### Query Engine Tests

- Missing projection preserves the current score.
- Positive and negative signals have bounded influence.
- Invalid, frozen, or suppressed signals are neutral.
- Vote scoring occurs before pagination.
- Vote signals do not cross the matched-term boundary.
- Tie ordering remains deterministic.
- Batch viewer-state loading avoids per-result requests.
- Forum downtime does not prevent search results from rendering.

### UI and Security Tests

- Keyboard and screen-reader operation.
- Selected, pending, success, failure, and signed-out states.
- Optimistic-update rollback.
- CSRF or bearer-token enforcement, depending on the approved auth model.
- Escaping of page URLs and API messages.
- Per-user and per-IP rate-limit behavior.

## Observability

Track at minimum:

- Vote writes, changes, removals, and rejected writes.
- Eligible versus raw votes.
- Projector lag, retries, and dead-letter events.
- PostgreSQL aggregate versus MongoDB projection divergence.
- Search results with missing or invalid vote projections.
- Result-position movement caused by vote quality.
- Domain-level concentration and suspected coordinated voting.
- Query latency before and after the MongoDB lookup.

Do not include bearer tokens, email addresses, or complete raw vote payloads in
logs.

## Rollback Strategy

Ranking activation must be independently reversible from vote collection.

1. Set `voteWeight` to `0` to restore the current TF-IDF/PageRank ordering.
2. Keep collecting and displaying votes if the source-of-truth system remains
   healthy.
3. Disable mutations separately if vote integrity or abuse controls fail.
4. Rebuild the MongoDB projection from PostgreSQL after projector corruption.
5. Never attempt to reconstruct Forum vote truth from the search projection.

## Product Decisions Required Before Implementation

1. Does a vote mean page quality/trustworthiness, general usefulness, or
   relevance to a specific query? This plan assumes general page quality.
2. May users vote only on indexed search results, or on any canonical URL?
3. Which accounts are eligible to influence ranking?
4. Should the UI display net score, separate up/down counts, or only the
   viewer's selection?
5. Does clicking an active vote remove it? The recommendation is yes.
6. Should page votes apply to image search?
7. Is eventual ranking consistency of seconds or minutes acceptable?
8. Is page movement between paginated requests acceptable as votes change?
9. Who may freeze or suppress a page's ranking signal?
10. Is a maximum initial influence of +/-10 percent acceptable?
11. Should matched query-term count remain the primary ranking boundary?
12. Confirm that existing post/comment votes will not be imported as page
    votes.

## Definition of Done

- Forum PostgreSQL is the authoritative and auditable source of page votes.
- Existing post/comment voting behavior remains unchanged.
- Canonical URL identity is enforced consistently across services.
- Vote writes and outbox events are atomic.
- The search projection is idempotent, replayable, and reconcilable.
- Search remains available with a neutral signal during Forum or projector
  outages.
- Ranking influence is bounded, configurable, versioned, and reversible.
- Abuse, moderation, accessibility, and security controls are tested.
- Shadow evaluation is complete before any nonzero ranking weight is enabled.
- Required CI, integration, migration, and rollback checks pass.
