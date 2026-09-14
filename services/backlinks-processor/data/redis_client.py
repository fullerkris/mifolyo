import redis

from config import (
    BACKLINK_KEY_PREFIX,
    DATASTORE_TIMEOUT_SECONDS,
    DEFAULT_LIMITS,
    MAX_BACKLINK_KEY_BYTES,
    MAX_CANONICAL_URL_BYTES,
)
from models.backlinks import BacklinkMember
from url_validation import CanonicalURLValidationError, validate_canonical_url


class InvalidBacklinkSet(ValueError):
    def __init__(self, reason: str):
        super().__init__(reason)
        self.reason = reason


class RedisScanLimitExceeded(redis.RedisError):
    pass


def validate_redis_auth(username: str, password: str, allow_insecure: bool) -> None:
    if username and not password:
        raise ValueError("REDIS_USERNAME requires REDIS_PASSWORD")
    if not password and not allow_insecure:
        raise ValueError(
            "Redis authentication is required unless "
            "ALLOW_INSECURE_DATASTORES=true is explicitly set for local testing"
        )


def parse_backlink_key(raw_key: bytes) -> str:
    if not isinstance(raw_key, bytes) or not raw_key.startswith(BACKLINK_KEY_PREFIX):
        raise InvalidBacklinkSet("target_key_invalid")
    if len(raw_key) > MAX_BACKLINK_KEY_BYTES:
        raise InvalidBacklinkSet("target_url_too_long")
    raw_url = raw_key[len(BACKLINK_KEY_PREFIX) :]
    if not raw_url:
        raise InvalidBacklinkSet("target_key_invalid")
    try:
        target_url = raw_url.decode("utf-8")
    except UnicodeDecodeError as error:
        raise InvalidBacklinkSet("target_key_invalid") from error
    try:
        validate_canonical_url(target_url)
    except CanonicalURLValidationError as error:
        raise InvalidBacklinkSet("target_url_noncanonical") from error
    return target_url


def parse_backlink_member(raw_member: bytes) -> BacklinkMember:
    if not isinstance(raw_member, bytes):
        raise InvalidBacklinkSet("member_invalid_utf8")
    if len(raw_member) > MAX_CANONICAL_URL_BYTES:
        raise InvalidBacklinkSet("member_url_too_long")
    try:
        url = raw_member.decode("utf-8")
    except UnicodeDecodeError as error:
        raise InvalidBacklinkSet("member_invalid_utf8") from error
    if not url:
        raise InvalidBacklinkSet("member_invalid_utf8")
    try:
        validate_canonical_url(url)
    except CanonicalURLValidationError as error:
        raise InvalidBacklinkSet("member_url_noncanonical") from error
    return BacklinkMember(raw=raw_member, url=url)


class RedisClient:
    def __init__(
        self,
        host: str = "localhost",
        port: int = 6379,
        username: str = "",
        password: str = "",
        db: int = 0,
        *,
        allow_insecure: bool = False,
        socket_timeout_seconds: float = DATASTORE_TIMEOUT_SECONDS,
    ) -> None:
        username = username or ""
        password = password or ""
        validate_redis_auth(username, password, allow_insecure)
        self.client = redis.Redis(
            host=host,
            port=port,
            username=username or None,
            password=password or None,
            db=db,
            decode_responses=False,
            socket_connect_timeout=socket_timeout_seconds,
            socket_timeout=socket_timeout_seconds,
            health_check_interval=30,
        )
        self.client.ping()

    def scan_backlink_keys(
        self,
        cursor: int,
        *,
        count_hint: int = 100,
        max_items: int = DEFAULT_LIMITS.max_scan_response_items,
    ) -> tuple[int, tuple[bytes, ...]]:
        next_cursor, keys = self.client.scan(
            cursor=cursor,
            match=BACKLINK_KEY_PREFIX + b"*",
            count=count_hint,
        )
        if len(keys) > max_items:
            raise RedisScanLimitExceeded("key_scan_response_limit_exceeded")
        return int(next_cursor), tuple(keys)

    def scan_backlink_members(
        self,
        key: bytes,
        cursor: int,
        *,
        count_hint: int = 256,
        max_items: int = DEFAULT_LIMITS.max_scan_response_items,
    ) -> tuple[int, tuple[bytes, ...]]:
        key_type = self.client.type(key)
        if key_type == b"none":
            return 0, ()
        if key_type != b"set":
            raise InvalidBacklinkSet("redis_key_wrong_type")
        try:
            next_cursor, members = self.client.sscan(
                key,
                cursor=cursor,
                count=count_hint,
            )
        except redis.ResponseError as error:
            if "WRONGTYPE" in str(error):
                raise InvalidBacklinkSet("redis_key_wrong_type") from error
            raise
        if len(members) > max_items:
            raise RedisScanLimitExceeded("member_scan_response_limit_exceeded")
        return int(next_cursor), tuple(members)

    def srem_backlink_members(self, key: bytes, members: tuple[bytes, ...]) -> int:
        if not members:
            raise ValueError("at least one backlink member is required")
        return int(self.client.srem(key, *members))

    def close(self) -> None:
        self.client.close()
