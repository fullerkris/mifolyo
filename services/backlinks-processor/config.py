from dataclasses import dataclass


BACKLINK_KEY_PREFIX = b"backlinks:"
MAX_CANONICAL_URL_BYTES = 2048
MAX_BACKLINK_KEY_BYTES = len(BACKLINK_KEY_PREFIX) + MAX_CANONICAL_URL_BYTES
MAX_MEMBERS_PER_ACK_BATCH = 256
MAX_ACK_BATCH_URL_BYTES = 512 * 1024
MAX_MONGO_DOCUMENT_BYTES = 12 * 1024 * 1024
MAX_SCAN_RESPONSE_ITEMS = 1024
MAX_PENDING_TARGET_KEYS = 2048
MAX_PENDING_MEMBER_BYTES = 8 * 1024 * 1024
DATASTORE_TIMEOUT_SECONDS = 5
POLL_INTERVAL_SECONDS = 10
RETRY_BACKOFF_SECONDS = 2


@dataclass(frozen=True)
class ProcessingLimits:
    scan_count_hint: int = 100
    max_scan_calls_per_cycle: int = 100
    max_targets_per_cycle: int = 100
    max_pending_target_keys: int = MAX_PENDING_TARGET_KEYS
    max_scan_response_items: int = MAX_SCAN_RESPONSE_ITEMS
    max_pending_member_bytes: int = MAX_PENDING_MEMBER_BYTES
    sscan_count_hint: int = 256
    max_members_per_target_per_cycle: int = 256
    max_members_per_ack_batch: int = MAX_MEMBERS_PER_ACK_BATCH
    max_ack_batch_url_bytes: int = MAX_ACK_BATCH_URL_BYTES
    max_mongo_document_bytes: int = MAX_MONGO_DOCUMENT_BYTES

    def __post_init__(self) -> None:
        values = vars(self)
        if any(value <= 0 for value in values.values()):
            raise ValueError("processing limits must be positive")
        if self.max_pending_target_keys < self.max_targets_per_cycle:
            raise ValueError(
                "max_pending_target_keys must be at least max_targets_per_cycle"
            )
        if self.max_pending_target_keys < self.max_scan_response_items:
            raise ValueError(
                "max_pending_target_keys must be at least max_scan_response_items"
            )
        if (
            self.max_pending_member_bytes
            < self.max_scan_response_items * MAX_CANONICAL_URL_BYTES
        ):
            raise ValueError(
                "max_pending_member_bytes must hold a full member scan response"
            )


DEFAULT_LIMITS = ProcessingLimits()
