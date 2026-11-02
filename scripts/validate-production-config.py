#!/usr/bin/env python3
"""Validate resolved Compose configuration without printing its secrets."""

import base64
import json
import re
import sys
from urllib.parse import urlparse


def validate(config):
    services = config["services"]
    query = services["query-engine"]["environment"]
    forum = services["forum-engine"]["environment"]
    for name, env in (("QUERY_APP_KEY", query), ("FORUM_APP_KEY", forum)):
        key = env["APP_KEY"]
        try:
            raw = base64.b64decode(key.removeprefix("base64:"), validate=True)
        except (ValueError, TypeError):
            raw = b""
        if not key.startswith("base64:") or len(raw) != 32 or len(set(raw)) < 8:
            raise ValueError(f"{name} must be a unique random 32-byte base64 Laravel key.")
    if query["APP_KEY"] == forum["APP_KEY"]:
        raise ValueError("Use separate Query Engine and Forum Engine application keys.")

    mongo = services["mongo"]["environment"]
    passwords = [mongo["MONGO_INITDB_ROOT_PASSWORD"], mongo["MONGO_QUERY_PASSWORD"],
                 forum["DB_PASSWORD"], query["REDIS_PASSWORD"]]
    if any(not re.fullmatch(r"[a-fA-F0-9]{64}", password) for password in passwords):
        raise ValueError("Generate each datastore password with: openssl rand -hex 32")
    if len(set(passwords)) != len(passwords):
        raise ValueError("Use a different password for each datastore role.")

    for service in ("query-engine", "forum-engine"):
        image = services[service]["image"]
        digest = re.fullmatch(r"[^\s@]+@sha256:[a-f0-9]{64}", image)
        tag = image.rsplit("/", 1)[-1].partition(":")[2]
        if not digest and (not tag or tag == "latest" or "REPLACE" in tag):
            raise ValueError(f"{service} needs a release-specific image tag or sha256 digest.")

    url = query["APP_URL"]
    parsed = urlparse(url)
    if (parsed.scheme != "https" or not (parsed.hostname or "").endswith(".ts.net")
            or parsed.username or parsed.password or parsed.port
            or parsed.path not in ("", "/") or parsed.query or parsed.fragment):
        raise ValueError("MIFOLYO_PUBLIC_URL must be an HTTPS Tailscale .ts.net origin.")
    if forum["MAIL_SCHEME"] not in ("smtp", "smtps") or "@" not in forum["MAIL_FROM_ADDRESS"]:
        raise ValueError("Set MAIL_SCHEME to smtp/smtps and MAIL_FROM_ADDRESS to an email address.")
    if not 1 <= int(forum["MAIL_PORT"]) <= 65535:
        raise ValueError("MAIL_PORT must be between 1 and 65535.")
    ports = services["caddy"]["ports"]
    if len(ports) != 1 or ports[0]["host_ip"] != "127.0.0.1":
        raise ValueError("Production ingress must bind only to loopback.")
    port = int(ports[0]["published"])
    if not 1 <= port <= 65535:
        raise ValueError("MIFOLYO_HTTP_PORT must be between 1 and 65535.")
    for name, service in services.items():
        if name != "caddy" and service.get("ports"):
            raise ValueError(f"Unexpected published port on {name}.")
    return port, url.rstrip("/")


if __name__ == "__main__":
    try:
        port, url = validate(json.load(sys.stdin))
    except (ValueError, KeyError, TypeError) as error:
        sys.exit(f"Invalid production configuration: {error}")
    print(port)
    print(url)
