import hashlib
import logging
import os
import signal
import sys
import threading
from collections import Counter, deque
from dataclasses import dataclass, field
from typing import Callable, Literal, Mapping, Optional, Sequence

from pymongo.errors import ConnectionFailure, PyMongoError, WTimeoutError
from redis.exceptions import RedisError

from config import (
    DEFAULT_LIMITS,
    MAX_CANONICAL_URL_BYTES,
    POLL_INTERVAL_SECONDS,
    RETRY_BACKOFF_SECONDS,
    ProcessingLimits,
)
from data.mongo_client import (
    BacklinkDocumentRejected,
    MongoClient,
    MongoWriteConflict,
    MongoWriteNotAcknowledged,
)
from data.redis_client import (
    InvalidBacklinkSet,
    RedisClient,
    RedisScanLimitExceeded,
    parse_backlink_key,
    parse_backlink_member,
)
from models.backlinks import BacklinkBatch, BacklinkMember


logging.basicConfig(
    level=logging.INFO, format="%(asctime)s - %(name)s - %(levelname)s - %(message)s"
)
logger = logging.getLogger(__name__)


@dataclass(frozen=True)
class BatchResult:
    status: Literal["acked", "rejected", "retry_later"]
    requested_members: int
    removed_members: int
    reason: Optional[str] = None


@dataclass(frozen=True)
class CycleResult:
    targets_visited: int
    batches_acked: int
    members_acked: int
    members_removed: int
    rejections: Mapping[str, int]
    retries: Mapping[str, int]
    interrupted: bool


@dataclass
class TargetScanState:
    cursor: int = 0
    scan_complete: bool = False
    pending_members: deque[bytes] = field(default_factory=deque)


@dataclass
class ProcessorState:
    key_cursor: int = 0
    pending_keys: deque[bytes] = field(default_factory=deque)
    queued_keys: set[bytes] = field(default_factory=set)
    targets: dict[bytes, TargetScanState] = field(default_factory=dict)
    member_scan_waiter: Optional[bytes] = None


def work_reference(value: object) -> str:
    raw = value if isinstance(value, bytes) else str(value).encode("utf-8", "replace")
    return hashlib.sha256(raw).hexdigest()[:16]


def allow_insecure_datastores() -> bool:
    return os.getenv("ALLOW_INSECURE_DATASTORES") == "true"


def process_batch(
    redis_client: RedisClient,
    mongo_client: MongoClient,
    batch: BacklinkBatch,
    *,
    max_members: int,
    max_batch_url_bytes: int,
    max_document_bytes: int,
) -> BatchResult:
    try:
        mongo_client.add_backlinks(
            batch.target_url,
            [member.url for member in batch.members],
            max_members=max_members,
            max_batch_url_bytes=max_batch_url_bytes,
            max_document_bytes=max_document_bytes,
        )
    except BacklinkDocumentRejected as error:
        return BatchResult("rejected", len(batch.members), 0, error.reason)
    except MongoWriteConflict:
        return BatchResult("retry_later", len(batch.members), 0, "mongo_write_conflict")
    except MongoWriteNotAcknowledged:
        return BatchResult(
            "retry_later", len(batch.members), 0, "mongo_write_not_acknowledged"
        )
    except (ConnectionFailure, WTimeoutError):
        return BatchResult("retry_later", len(batch.members), 0, "mongo_unavailable")
    except PyMongoError:
        return BatchResult("rejected", len(batch.members), 0, "mongo_write_rejected")

    raw_members = tuple(member.raw for member in batch.members)
    try:
        removed = redis_client.srem_backlink_members(batch.redis_key, raw_members)
    except RedisError:
        return BatchResult("retry_later", len(batch.members), 0, "redis_ack_unavailable")
    return BatchResult("acked", len(batch.members), removed)


def _queue_key(state: ProcessorState, key: bytes) -> None:
    if key not in state.queued_keys:
        state.pending_keys.append(key)
        state.queued_keys.add(key)


def _restore_members(
    target_state: TargetScanState, members: Sequence[BacklinkMember]
) -> None:
    for member in reversed(members):
        target_state.pending_members.appendleft(member.raw)


def _fill_pending_keys(
    redis_client: RedisClient,
    state: ProcessorState,
    stop_event: threading.Event,
    limits: ProcessingLimits,
) -> None:
    scan_calls = 0
    scan_capacity = limits.max_pending_target_keys - limits.max_scan_response_items
    while (
        scan_calls < limits.max_scan_calls_per_cycle
        and len(state.pending_keys) <= scan_capacity
        and not stop_event.is_set()
    ):
        next_cursor, keys = redis_client.scan_backlink_keys(
            state.key_cursor,
            count_hint=limits.scan_count_hint,
            max_items=limits.max_scan_response_items,
        )
        state.key_cursor = next_cursor
        for key in keys:
            _queue_key(state, key)
        scan_calls += 1
        if next_cursor == 0 or len(state.pending_keys) >= limits.max_targets_per_cycle:
            break


def _admit_members(
    target_state: TargetScanState,
    limits: ProcessingLimits,
) -> tuple[list[BacklinkMember], Counter]:
    admitted = []
    rejections = Counter()
    examined = 0
    encoded_bytes = 0
    while (
        target_state.pending_members
        and examined < limits.max_members_per_target_per_cycle
    ):
        raw_member = target_state.pending_members.popleft()
        examined += 1
        try:
            member = parse_backlink_member(raw_member)
        except InvalidBacklinkSet as error:
            rejections[error.reason] += 1
            logger.warning(
                "Rejected backlink member reason=%s ref=%s",
                error.reason,
                work_reference(raw_member),
            )
            continue
        next_bytes = encoded_bytes + len(member.raw)
        if (
            len(admitted) >= limits.max_members_per_ack_batch
            or next_bytes > limits.max_ack_batch_url_bytes
        ):
            if not admitted and next_bytes > limits.max_ack_batch_url_bytes:
                rejections["member_batch_byte_limit_exceeded"] += 1
                logger.warning(
                    "Rejected backlink member reason=%s ref=%s",
                    "member_batch_byte_limit_exceeded",
                    work_reference(raw_member),
                )
                continue
            target_state.pending_members.appendleft(raw_member)
            break
        admitted.append(member)
        encoded_bytes = next_bytes
    return admitted, rejections


def process_cycle(
    redis_client: RedisClient,
    mongo_client: MongoClient,
    state: ProcessorState,
    stop_event: threading.Event,
    *,
    limits: ProcessingLimits = DEFAULT_LIMITS,
) -> CycleResult:
    targets_visited = 0
    batches_acked = 0
    members_acked = 0
    members_removed = 0
    rejections = Counter()
    retries = Counter()
    # Recompute once per cycle; retries and shutdown restore the same raw bytes.
    pending_bytes = sum(
        len(raw)
        for target in state.targets.values()
        for raw in target.pending_members
    )
    scan_byte_limit = limits.max_scan_response_items * MAX_CANONICAL_URL_BYTES
    try:
        _fill_pending_keys(redis_client, state, stop_event, limits)
    except RedisScanLimitExceeded:
        retries["redis_key_scan_response_limit_exceeded"] += 1
    except RedisError:
        retries["redis_key_scan_unavailable"] += 1

    targets_this_cycle = min(
        len(state.pending_keys), limits.max_targets_per_cycle
    )
    for _ in range(targets_this_cycle):
        if stop_event.is_set():
            break
        key = state.pending_keys.popleft()
        state.queued_keys.discard(key)
        targets_visited += 1
        try:
            target_url = parse_backlink_key(key)
        except InvalidBacklinkSet as error:
            rejections[error.reason] += 1
            logger.warning(
                "Rejected backlink target reason=%s ref=%s",
                error.reason,
                work_reference(key),
            )
            continue

        target_state = state.targets.setdefault(key, TargetScanState())
        previous_bytes = sum(len(raw) for raw in target_state.pending_members)
        if not target_state.pending_members and not target_state.scan_complete:
            # Reserve a full valid page, giving the first waiter priority while
            # buffered targets drain. Never advance a cursor for an unheld page.
            if (
                state.member_scan_waiter not in (None, key)
                or pending_bytes + scan_byte_limit > limits.max_pending_member_bytes
            ):
                if state.member_scan_waiter is None:
                    state.member_scan_waiter = key
                _queue_key(state, key)
                retries["pending_member_byte_limit"] += 1
                continue
            state.member_scan_waiter = None
            try:
                cursor, members = redis_client.scan_backlink_members(
                    key,
                    target_state.cursor,
                    count_hint=limits.sscan_count_hint,
                    max_items=limits.max_scan_response_items,
                )
                if sum(len(raw) for raw in members) > scan_byte_limit:
                    raise RedisScanLimitExceeded(
                        "member_scan_response_byte_limit_exceeded"
                    )
            except InvalidBacklinkSet as error:
                rejections[error.reason] += 1
                logger.warning(
                    "Rejected backlink set reason=%s ref=%s",
                    error.reason,
                    work_reference(key),
                )
                state.targets.pop(key, None)
                continue
            except RedisError:
                _queue_key(state, key)
                raise
            target_state.cursor = cursor
            target_state.scan_complete = cursor == 0
            target_state.pending_members.extend(members)

        members, member_rejections = _admit_members(target_state, limits)
        rejections.update(member_rejections)
        if members and stop_event.is_set():
            _restore_members(target_state, members)
        elif members:
            batch = BacklinkBatch(
                redis_key=key,
                target_url=target_url,
                members=tuple(members),
                encoded_url_bytes=sum(len(member.raw) for member in members),
            )
            result = process_batch(
                redis_client,
                mongo_client,
                batch,
                max_members=limits.max_members_per_ack_batch,
                max_batch_url_bytes=limits.max_ack_batch_url_bytes,
                max_document_bytes=limits.max_mongo_document_bytes,
            )
            if result.status == "acked":
                batches_acked += 1
                members_acked += result.requested_members
                members_removed += result.removed_members
                if result.removed_members != result.requested_members:
                    logger.warning(
                        "Backlink ACK removed fewer members requested=%d removed=%d ref=%s",
                        result.requested_members,
                        result.removed_members,
                        work_reference(key),
                    )
            elif result.status == "retry_later":
                _restore_members(target_state, members)
                retries[result.reason or result.status] += 1
                logger.warning(
                    "Backlink batch retained status=%s reason=%s ref=%s",
                    result.status,
                    result.reason,
                    work_reference(key),
                )
            else:
                rejections[result.reason or result.status] += 1
                logger.warning(
                    "Backlink batch retained status=%s reason=%s ref=%s",
                    result.status,
                    result.reason,
                    work_reference(key),
                )

        pending_bytes += (
            sum(len(raw) for raw in target_state.pending_members) - previous_bytes
        )
        if target_state.pending_members or not target_state.scan_complete:
            _queue_key(state, key)
        else:
            state.targets.pop(key, None)

    return CycleResult(
        targets_visited=targets_visited,
        batches_acked=batches_acked,
        members_acked=members_acked,
        members_removed=members_removed,
        rejections=dict(rejections),
        retries=dict(retries),
        interrupted=stop_event.is_set(),
    )


def run_processor(
    redis_client: RedisClient,
    mongo_client: MongoClient,
    stop_event: threading.Event,
    *,
    wait: Callable[[float], bool] = None,
    limits: ProcessingLimits = DEFAULT_LIMITS,
) -> int:
    wait = wait or stop_event.wait
    state = ProcessorState()
    while not stop_event.is_set():
        try:
            result = process_cycle(
                redis_client,
                mongo_client,
                state,
                stop_event,
                limits=limits,
            )
        except (PyMongoError, RedisError) as error:
            logger.error(
                "Backlink cycle datastore failure type=%s", type(error).__name__
            )
            wait(RETRY_BACKOFF_SECONDS)
            continue
        logger.info(
            "Backlink cycle targets=%d batches=%d acknowledged=%d removed=%d rejections=%s retries=%s",
            result.targets_visited,
            result.batches_acked,
            result.members_acked,
            result.members_removed,
            dict(result.rejections),
            dict(result.retries),
        )
        if stop_event.is_set():
            break
        if result.retries:
            wait(RETRY_BACKOFF_SECONDS)
        elif not state.pending_keys:
            wait(POLL_INTERVAL_SECONDS)
    return 0


def _int_env(name: str, default: int) -> int:
    value = int(os.getenv(name, str(default)))
    if value < 0 or value > 65535:
        raise ValueError(f"{name} is out of range")
    return value


def main(argv: Optional[Sequence[str]] = None) -> int:
    del argv
    stop_event = threading.Event()

    def request_shutdown(signum, frame):
        del signum, frame
        logger.info("Termination signal received; bounded shutdown requested")
        stop_event.set()

    signal.signal(signal.SIGTERM, request_shutdown)
    signal.signal(signal.SIGINT, request_shutdown)

    redis_client = None
    mongo_client = None
    try:
        allow_insecure = allow_insecure_datastores()
        redis_client = RedisClient(
            host=os.getenv("REDIS_HOST", "localhost"),
            port=_int_env("REDIS_PORT", 6379),
            username=os.getenv("REDIS_USERNAME", ""),
            password=os.getenv("REDIS_PASSWORD", ""),
            db=_int_env("REDIS_DB", 0),
            allow_insecure=allow_insecure,
        )
        if stop_event.is_set():
            return 0
        mongo_client = MongoClient(
            host=os.getenv("MONGO_HOST", "localhost"),
            port=_int_env("MONGO_PORT", 27017),
            username=os.getenv("MONGO_USERNAME", ""),
            password=os.getenv("MONGO_PASSWORD", ""),
            db=os.getenv("MONGO_DB", "test"),
            allow_insecure=allow_insecure,
        )
        return run_processor(redis_client, mongo_client, stop_event)
    except Exception as error:
        logger.error("Backlinks Processor failed type=%s", type(error).__name__)
        return 1
    finally:
        if mongo_client is not None:
            mongo_client.close()
        if redis_client is not None:
            redis_client.close()


if __name__ == "__main__":
    sys.exit(main())
