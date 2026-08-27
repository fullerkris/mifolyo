import logging
import os
import signal
import sys
import time
from collections import Counter

from data.mongo_client import MongoClient
from data.redis_client import (
    InvalidPublicationKey,
    PageDataDecodeError,
    RedisClient,
    claim_reference,
    parse_page_publication_key,
)
from utils.constants import (
    INDEXER_CLAIM_POLL_SECONDS,
    INDEXER_LOCK_RENEW_SECONDS,
    INDEXER_RECOVERY_BATCH_SIZE,
    MAX_INDEX_WORDS,
)
from utils.utils import get_html_data, split_url


logging.basicConfig(
    level=logging.INFO, format="%(asctime)s - %(name)s - %(levelname)s - %(message)s"
)
logger = logging.getLogger(__name__)
running = True
COMPLETED = "completed"
SKIPPED = "skipped"
QUARANTINED = "quarantined"
TRANSIENT_FAILURE = "transient_failure"
LOCK_WAIT_SECONDS = 0.25
ALLOW_INSECURE_DATASTORES_ENV = "ALLOW_INSECURE_DATASTORES"


def allow_insecure_datastores() -> bool:
    return os.getenv(ALLOW_INSECURE_DATASTORES_ENV) == "true"


def handle_exit(signum, frame):
    global running
    logger.info("Termination signal received - shutting down")
    running = False


signal.signal(signal.SIGTERM, handle_exit)
signal.signal(signal.SIGINT, handle_exit)


def renew_owner(redis_client) -> bool:
    try:
        return redis_client.renew_lock()
    except Exception:
        logger.error("Could not renew indexer owner lock")
        return False


def remove_state_and_skip(
    redis_client,
    mongo_client,
    page_key,
    normalized_url,
    canonical_owner_token,
    owner_check,
):
    """Remove prior searchable state before atomically ACKing a permanent skip."""
    if not owner_check():
        return TRANSIENT_FAILURE
    try:
        removed = mongo_client.remove_search_state(
            normalized_url,
            redis_client.owner_epoch,
            canonical_owner_token,
            owner_check=owner_check,
        )
    except Exception:
        logger.error("Could not remove MongoDB search state")
        return TRANSIENT_FAILURE
    if not removed or not owner_check():
        return TRANSIENT_FAILURE
    return SKIPPED if redis_client.skip_page(page_key) else TRANSIENT_FAILURE


def process_claim(redis_client, mongo_client, page_key) -> str:
    """Process one validated immutable publication without in-memory retries."""
    try:
        publication_id, key_url, _ = parse_page_publication_key(page_key)
    except InvalidPublicationKey:
        # Validation normally happens before this function; never interpret a
        # malformed queue value as a Redis key or URL identity.
        return TRANSIENT_FAILURE

    if not renew_owner(redis_client):
        return TRANSIENT_FAILURE
    canonical_owner_token = MongoClient.lock_token(
        str(redis_client.owner_token), publication_id
    )
    try:
        acquired = mongo_client.acquire_canonical_lock(
            key_url,
            canonical_owner_token,
            publication_id,
            redis_client.owner_epoch,
        )
    except Exception:
        logger.error("Could not acquire canonical MongoDB ownership")
        return TRANSIENT_FAILURE
    if not acquired:
        return TRANSIENT_FAILURE
    owner_check = lambda: (
        running
        and renew_owner(redis_client)
        and mongo_client.owns_canonical_lock(key_url, canonical_owner_token)
    )
    try:
        try:
            page = redis_client.get_page_data(page_key)
        except PageDataDecodeError:
            logger.error("Invalid page payload (claim=%s)", claim_reference(page_key))
            return (
                QUARANTINED
                if owner_check() and redis_client.quarantine_page(page_key)
                else TRANSIENT_FAILURE
            )
        except Exception:
            logger.error(
                "Transient Redis claim fetch failure (claim=%s)",
                claim_reference(page_key),
            )
            return TRANSIENT_FAILURE

        if page is None:
            logger.warning(
                "Page publication is absent; quarantining (claim=%s)",
                claim_reference(page_key),
            )
            return (
                QUARANTINED
                if owner_check() and redis_client.quarantine_page(page_key)
                else TRANSIENT_FAILURE
            )

        normalized_url = page.normalized_url
        if normalized_url != key_url:
            return TRANSIENT_FAILURE

        # The non-expiring Mongo lock covers artifacts, canonical state, and
        # the final owner-fenced Redis ACK/quarantine decision.
        try:
            if page.rendered and not mongo_client.save_page_artifact(
                page,
                redis_client.owner_epoch,
                canonical_owner_token,
                owner_check=owner_check,
            ):
                return TRANSIENT_FAILURE
        except Exception:
            logger.error(
                "Transient artifact failure (claim=%s)", claim_reference(page_key)
            )
            return TRANSIENT_FAILURE

        try:
            html_data = get_html_data(page.html, rendered=page.rendered)
        except Exception:
            logger.error(
                "Transient HTML decode failure (claim=%s)", claim_reference(page_key)
            )
            return TRANSIENT_FAILURE

        if (
            not html_data
            or html_data.get("language") != "en"
            or not html_data.get("text")
        ):
            logger.info(
                "Publication is permanently non-indexable (claim=%s)",
                claim_reference(page_key),
            )
            return remove_state_and_skip(
                redis_client,
                mongo_client,
                page_key,
                normalized_url,
                canonical_owner_token,
                owner_check,
            )

        text = html_data["text"]
        keywords = dict(Counter(text).most_common(MAX_INDEX_WORDS))
        for word in split_url(normalized_url):
            previous = keywords.get(word, 0)
            keywords[word] = previous * 50 if previous else 10

        try:
            outlinks = redis_client.get_outlinks(page_key, normalized_url)
        except Exception:
            logger.error(
                "Transient outlinks fetch failure (claim=%s)",
                claim_reference(page_key),
            )
            return TRANSIENT_FAILURE

        try:
            reconciled = mongo_client.replace_search_state(
                page,
                html_data,
                keywords,
                outlinks,
                redis_client.owner_epoch,
                canonical_owner_token,
                owner_check=owner_check,
            )
        except Exception:
            logger.error(
                "Could not reconcile MongoDB state (claim=%s)",
                claim_reference(page_key),
            )
            return TRANSIENT_FAILURE
        if not reconciled or not owner_check():
            return TRANSIENT_FAILURE

        return (
            COMPLETED
            if redis_client.complete_page(page_key, normalized_url)
            else TRANSIENT_FAILURE
        )
    finally:
        try:
            if not mongo_client.release_canonical_lock(
                key_url, canonical_owner_token
            ):
                logger.error(
                    "Canonical MongoDB ownership release failed; manual review required"
                )
        except Exception:
            logger.error(
                "Canonical MongoDB ownership release failed; manual review required"
            )


def acquire_owner(redis_client, sleep=time.sleep) -> bool:
    """Wait in bounded intervals while another healthy indexer owns the lock."""
    while running:
        try:
            if redis_client.acquire_lock():
                return True
        except Exception:
            logger.error("Could not acquire indexer owner lock")
            return False
        sleep(LOCK_WAIT_SECONDS)
    return False


def recover_claims(redis_client) -> bool:
    """Recover in bounded Lua batches, renewing ownership between batches."""
    while running:
        if not renew_owner(redis_client):
            return False
        try:
            recovered = redis_client.recover_abandoned_claims(
                limit=INDEXER_RECOVERY_BATCH_SIZE
            )
        except Exception:
            logger.error("Could not recover abandoned claims")
            return False
        if recovered < INDEXER_RECOVERY_BATCH_SIZE:
            return True
    return False


def run_indexer(redis_client, mongo_client, sleep=time.sleep) -> int:
    global running
    if not acquire_owner(redis_client, sleep=sleep):
        return 0 if not running else 1

    try:
        if not recover_claims(redis_client):
            return 0 if not running else 1

        resume_signal_sent = False
        last_renewal = time.monotonic()
        while running:
            if time.monotonic() - last_renewal >= INDEXER_LOCK_RENEW_SECONDS:
                if not renew_owner(redis_client):
                    return 1
                last_renewal = time.monotonic()
            try:
                queued, processing = redis_client.get_work_sizes()
                if queued == 0 and processing == 0 and not resume_signal_sent:
                    if not redis_client.signal_crawler():
                        return 1
                    resume_signal_sent = True
                elif queued or processing:
                    resume_signal_sent = False
                page_key = redis_client.claim_page()
            except Exception:
                logger.error("Redis queue or owner-lock failure")
                return 1

            if page_key is None:
                sleep(INDEXER_CLAIM_POLL_SECONDS)
                continue
            resume_signal_sent = False
            if not running:
                logger.info(
                    "Leaving claim recoverable during shutdown (claim=%s)",
                    claim_reference(page_key),
                )
                break

            try:
                parse_page_publication_key(page_key)
            except InvalidPublicationKey:
                logger.error(
                    "Quarantining invalid queue value (claim=%s)",
                    claim_reference(page_key),
                )
                if not redis_client.quarantine_page(page_key):
                    return 1
                continue

            outcome = process_claim(redis_client, mongo_client, page_key)
            if outcome == TRANSIENT_FAILURE:
                # This operation is owner-fenced. Lock loss leaves the claim in
                # processing for the replacement owner rather than moving it.
                redis_client.release_page(page_key)
                return 1
        return 0
    finally:
        try:
            redis_client.release_lock()
        except Exception:
            logger.error("Could not release indexer owner lock")


def main() -> int:
    allow_insecure = allow_insecure_datastores()
    try:
        redis_client = RedisClient(
            host=os.getenv("REDIS_HOST", "localhost"),
            port=int(os.getenv("REDIS_PORT", 6379)),
            username=os.getenv("REDIS_USERNAME", ""),
            password=os.getenv("REDIS_PASSWORD", ""),
            db=int(os.getenv("REDIS_DB", 0)),
            allow_insecure=allow_insecure,
        )
    except (TypeError, ValueError):
        logger.error("Datastore configuration rejected")
        return 1
    if redis_client.client is None:
        return 1
    try:
        mongo_client = MongoClient(
            host=os.getenv("MONGO_HOST", "localhost"),
            port=int(os.getenv("MONGO_PORT", 27017)),
            password=os.getenv("MONGO_PASSWORD", ""),
            db=os.getenv("MONGO_DB", "test"),
            username=os.getenv("MONGO_USERNAME", ""),
            allow_insecure=allow_insecure,
        )
    except (TypeError, ValueError):
        logger.error("Datastore configuration rejected")
        return 1
    if mongo_client.client is None:
        return 1
    return run_indexer(redis_client, mongo_client)


if __name__ == "__main__":
    sys.exit(main())
