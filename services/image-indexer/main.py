import logging
import os
import signal
import sys
import time

from data.mongo_client import MongoClient
from data.redis_client import (
    InvalidImageWork,
    RedisClient,
    claim_reference,
    parse_manifest_key,
)
from utils.constants import (
    IMAGE_CLAIM_POLL_SECONDS,
    IMAGE_LOCK_RENEW_SECONDS,
    IMAGE_RECOVERY_BATCH_SIZE,
)


logging.basicConfig(
    level=logging.INFO, format="%(asctime)s - %(name)s - %(levelname)s - %(message)s"
)
logger = logging.getLogger(__name__)
running = True
LOCK_WAIT_SECONDS = 0.25
COMPLETED = "completed"
QUARANTINED = "quarantined"
TRANSIENT_FAILURE = "transient_failure"


def handle_exit(signum, frame):
    global running
    running = False
    logger.info("Termination signal received; bounded shutdown requested")


signal.signal(signal.SIGTERM, handle_exit)
signal.signal(signal.SIGINT, handle_exit)


def allow_insecure_datastores() -> bool:
    return os.getenv("ALLOW_INSECURE_DATASTORES") == "true"


def renew_owner(redis_client) -> bool:
    try:
        return redis_client.renew_lock()
    except Exception:
        logger.error("Could not renew image-indexer owner lock")
        return False


def acquire_owner(redis_client, sleep=time.sleep) -> bool:
    while running:
        try:
            if redis_client.acquire_lock():
                return True
        except Exception:
            logger.error("Could not acquire image-indexer owner lock")
            return False
        sleep(LOCK_WAIT_SECONDS)
    return False


def recover_claims(redis_client) -> bool:
    while running:
        if not renew_owner(redis_client):
            return False
        try:
            recovered = redis_client.recover(IMAGE_RECOVERY_BATCH_SIZE)
        except Exception:
            logger.error("Could not recover image claims")
            return False
        if recovered < IMAGE_RECOVERY_BATCH_SIZE:
            return True
    return False


def process_claim(redis_client, mongo_client, manifest_key: str) -> str:
    try:
        publication_id, page_url, _ = parse_manifest_key(manifest_key)
    except InvalidImageWork:
        # A malformed key has no trustworthy canonical URL to lock. The queue
        # owner fencing remains the only safe quarantine boundary.
        logger.warning(
            "Quarantining invalid image publication claim=%s",
            claim_reference(manifest_key),
        )
        try:
            return QUARANTINED if redis_client.quarantine(manifest_key) else TRANSIENT_FAILURE
        except Exception:
            return TRANSIENT_FAILURE
    except Exception:
        logger.error("Image publication read failed claim=%s", claim_reference(manifest_key))
        return TRANSIENT_FAILURE

    if not renew_owner(redis_client):
        return TRANSIENT_FAILURE
    canonical_owner_token = MongoClient.lock_token(
        str(redis_client.owner_token), publication_id
    )
    try:
        acquired = mongo_client.acquire_canonical_lock(
            page_url,
            canonical_owner_token,
            publication_id,
            redis_client.owner_epoch,
        )
    except Exception:
        logger.error("Could not acquire canonical image MongoDB ownership")
        return TRANSIENT_FAILURE
    if not acquired:
        return TRANSIENT_FAILURE
    owner_check = lambda: (
        running
        and renew_owner(redis_client)
        and mongo_client.owns_canonical_lock(page_url, canonical_owner_token)
    )
    try:
        try:
            work = redis_client.load_work(manifest_key)
        except InvalidImageWork:
            logger.warning(
                "Quarantining invalid image publication claim=%s",
                claim_reference(manifest_key),
            )
            try:
                return (
                    QUARANTINED
                    if owner_check() and redis_client.quarantine(manifest_key)
                    else TRANSIENT_FAILURE
                )
            except Exception:
                return TRANSIENT_FAILURE
        except Exception:
            logger.error(
                "Image publication read failed claim=%s",
                claim_reference(manifest_key),
            )
            return TRANSIENT_FAILURE

        try:
            keywords = mongo_client.get_keywords(work.page_url)
            reconciled = mongo_client.replace_page_images(
                work.page_url,
                work.publication_id,
                work.images,
                keywords,
                redis_client.owner_epoch,
                canonical_owner_token,
                owner_check=owner_check,
            )
        except Exception:
            logger.error(
                "Image MongoDB operation failed claim=%s",
                claim_reference(manifest_key),
            )
            return TRANSIENT_FAILURE
        if not reconciled or not owner_check():
            return TRANSIENT_FAILURE
        try:
            return COMPLETED if redis_client.ack(work) else TRANSIENT_FAILURE
        except Exception:
            logger.error("Image ACK failed claim=%s", claim_reference(manifest_key))
            return TRANSIENT_FAILURE
    finally:
        try:
            if not mongo_client.release_canonical_lock(
                page_url, canonical_owner_token
            ):
                logger.error(
                    "Canonical image MongoDB ownership release failed; manual review required"
                )
        except Exception:
            logger.error(
                "Canonical image MongoDB ownership release failed; manual review required"
            )


def run_indexer(redis_client, mongo_client, sleep=time.sleep) -> int:
    if not acquire_owner(redis_client, sleep=sleep):
        return 0 if not running else 1
    try:
        if not recover_claims(redis_client):
            return 0 if not running else 1
        last_renewal = time.monotonic()
        while running:
            if time.monotonic() - last_renewal >= IMAGE_LOCK_RENEW_SECONDS:
                if not renew_owner(redis_client):
                    return 1
                last_renewal = time.monotonic()
            try:
                claim = redis_client.claim()
            except Exception:
                logger.error("Image Redis queue or owner-lock failure")
                return 1
            if claim is None:
                sleep(IMAGE_CLAIM_POLL_SECONDS)
                continue
            if not running:
                logger.info("Leaving image claim recoverable claim=%s", claim_reference(claim))
                break
            outcome = process_claim(redis_client, mongo_client, claim)
            if outcome == TRANSIENT_FAILURE:
                try:
                    redis_client.release_claim(claim)
                except Exception:
                    pass
                return 1
        return 0
    finally:
        try:
            redis_client.release_lock()
        except Exception:
            logger.error("Could not release image-indexer owner lock")


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
        mongo_client = MongoClient(
            host=os.getenv("MONGO_HOST", "localhost"),
            port=int(os.getenv("MONGO_PORT", 27017)),
            username=os.getenv("MONGO_USERNAME", ""),
            password=os.getenv("MONGO_PASSWORD", ""),
            db=os.getenv("MONGO_DB", "test"),
            allow_insecure=allow_insecure,
        )
    except (TypeError, ValueError):
        logger.error("Datastore configuration rejected")
        return 1
    if redis_client.client is None or mongo_client.client is None:
        return 1
    return run_indexer(redis_client, mongo_client)


if __name__ == "__main__":
    sys.exit(main())
