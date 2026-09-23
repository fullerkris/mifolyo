"""Whole-database reads for the closed 2/58/71-position negative fixtures."""
import re

import harness as h
import claim_release as cr


def text(value, maximum=2048):
    h.require(type(value) is bytes and len(value) <= maximum, "STATE_TEXT_BOUND")
    try:
        return value.decode("utf-8")
    except UnicodeError:
        raise h.InvalidArtifact("STATE_TEXT") from None


def number(value):
    h.require(type(value) is bytes and re.fullmatch(rb"(?:0|[1-9][0-9]{0,15})", value), "STATE_NUMBER")
    result = int(value)
    h.require(result <= h.MAX_EXACT, "STATE_NUMBER")
    return result


def inventory(client, limit):
    h.require(type(limit) is int and limit in (2, 58, 71), "STATE_LIMIT")
    count = client.call("DBSIZE")
    h.require(type(count) is int and 0 <= count <= limit, "STATE_KEY_COUNT")
    keys, cursor = set(), b"0"
    for _ in range(64):
        reply = client.call("SCAN", cursor, "COUNT", "16")
        h.require(type(reply) is list and len(reply) == 2 and type(reply[0]) is bytes and
                  re.fullmatch(rb"[0-9]{1,20}", reply[0]) and type(reply[1]) is list and len(reply[1]) <= limit, "STATE_SCAN")
        keys.update(text(key, 256) for key in reply[1])
        h.require(len(keys) <= limit, "STATE_SCAN_BOUND")
        cursor = reply[0]
        if cursor == b"0":
            h.require(len(keys) == count, "STATE_SCAN_CHANGED")
            return keys
    raise h.InvalidArtifact("STATE_SCAN_BUDGET")


def snapshot(client, expected):
    h.require(type(expected) is dict and len(expected) in (2, 58, 71), "STATE_INVENTORY")
    h.require(inventory(client, len(expected)) == {key for key, row in expected.items() if row is not None}, "STATE_MEMBERSHIP")
    actual = {}
    for key, row in sorted(expected.items()):
        kind = client.call("TYPE", key)
        if row is None:
            h.require(kind == b"none", "STATE_ABSENCE")
            actual[key] = None
            continue
        h.require(kind == row["type"].encode(), "STATE_TYPE")
        expiry = client.call("PEXPIRETIME", key)
        h.require(type(expiry) is int and expiry == row["expires_at_ms"], "STATE_EXPIRY")
        if row["type"] == "hash":
            fields = [name for name, _ in row["fields"]]
            h.require(0 < len(fields) <= 64 and len(set(fields)) == len(fields), "STATE_HASH_FIELDS")
            size = client.call("HLEN", key)
            h.require(type(size) is int and size == len(fields), "STATE_CARDINALITY")
            for field in fields:
                length = client.call("HSTRLEN", key, field)
                h.require(type(length) is int and 0 <= length <= 2048, "STATE_VALUE_BOUND")
            values = client.call("HMGET", key, *fields)
            h.require(type(values) is list and len(values) == len(fields), "STATE_VALUES")
            actual[key] = cr._hash(list(zip(fields, [text(value) for value in values])), expiry)
        elif row["type"] == "string":
            length = client.call("STRLEN", key)
            h.require(type(length) is int and 0 <= length <= 2048, "STATE_VALUE_BOUND")
            actual[key] = {"type": "string", "value": text(client.call("GET", key)), "expires_at_ms": expiry}
        else:
            h.require(row["type"] in ("set", "zset"), "STATE_KIND")
            count = len(row["members"])
            h.require(0 < count <= 71 and expiry == -1, "STATE_COLLECTION_BOUND")
            observed_count = client.call("SCARD" if row["type"] == "set" else "ZCARD", key)
            h.require(type(observed_count) is int and observed_count == count, "STATE_CARDINALITY")
            values = client.call("SMEMBERS", key) if row["type"] == "set" else client.call("ZRANGE", key, "0", str(count - 1), "WITHSCORES")
            h.require(type(values) is list and len(values) == count * (1 if row["type"] == "set" else 2), "STATE_MEMBERS")
            members = [text(value, 256) for value in (values if row["type"] == "set" else values[::2])]
            h.require(len(set(members)) == count, "STATE_DUPLICATE")
            actual[key] = cr._set(members) if row["type"] == "set" else cr._zset(dict(zip(members, [number(value) for value in values[1::2]])))
    h.require(h.canonical(actual) == h.canonical(expected), "STATE_MISMATCH")
    return actual


def with_boot(state, boot):
    import negative_specs as ns
    h.require(h.AUTH[0] not in state, "BOOT_OWNERSHIP")
    if boot is not None:
        h.exact(boot, set(ns.BOOT_FIELDS))
    return {**state, h.AUTH[0]: None if boot is None else cr._hash([(key, boot[key]) for key in ns.BOOT_FIELDS])}
