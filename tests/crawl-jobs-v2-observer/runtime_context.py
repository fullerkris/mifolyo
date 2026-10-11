"""Authenticated independent Linux-host context and controller-local expiry leases.

The owner provisions the source anchor/key and an authenticated transport to that
Linux host. An isolated actor's namespace is never inferred to be the host's.
No Docker resources, namespace joins, privilege changes or fallback probes occur.
"""
import copy
import hashlib
import hmac
import secrets

from contracts import LIMITS, TRIALS, Invalid, canonical, clock_ns, decode, digest, exact, integer, require, sha, validate_preparation
from linux_observation import LinuxReader, source_hash
from runtime_admission import CONTEXT_HASHES, HOST_NAMESPACES, NAMESPACES, validate_context

DOMAIN = "mifolyo:obs1:c0:linux-context"
PHASES = ("KERNEL", "NAMESPACE", "WINDOW")
SOURCE_FILES = ("contracts.py", "runtime_admission.py", "linux_observation.py", "runtime_context.py", "context_worker.py")
_TOKEN = object()


def source_digest():
    return source_hash(SOURCE_FILES)


def validate_anchor(value):
    exact(value, ("daemon_sha256", "source_sha256", "kernel_boot_sha256", "namespaces", "ns_device", "seccomp_filters"))
    for key in ("daemon_sha256", "source_sha256", "kernel_boot_sha256"):
        digest(value[key])
    exact(value["namespaces"], NAMESPACES)
    for number in value["namespaces"].values():
        integer(number, 1)
    integer(value["ns_device"], 1)
    exact(value["seccomp_filters"], ("target", "observer"))
    base = integer(value["seccomp_filters"]["target"], 1, 15)
    integer(value["seccomp_filters"]["observer"], base + 1, base + 1)
    return copy.deepcopy(value)


def mac(key, raw):
    require(type(key) is bytes and len(key) == 32 and key != bytes(32), "CONTEXT_KEY")
    return hmac.new(key, DOMAIN.encode() + b"\0" + raw, hashlib.sha256).hexdigest()


def request(raw, anchor, key):
    value = decode(raw, LIMITS["receipt_bytes"])
    exact(value, ("body", "mac"))
    require(type(value["mac"]) is str and hmac.compare_digest(value["mac"], mac(key, canonical(value["body"]))), "CONTEXT_AUTH")
    body = value["body"]
    exact(body, ("version", "case_id", "invocation_sha256", "preparation_sha256", "anchor_sha256", "challenge", "window_ns"))
    integer(body["version"], 1, 1)
    require(type(body["case_id"]) is str and body["case_id"] in TRIALS and not body["case_id"].startswith("N."), "CONTEXT_CASE")
    for name in ("invocation_sha256", "preparation_sha256", "anchor_sha256", "challenge"):
        digest(body[name])
    require(body["anchor_sha256"] == sha(canonical(anchor)), "CONTEXT_ANCHOR")
    integer(body["window_ns"], 1, LIMITS["admission_ms"] * 1000000)
    return body


class LinuxContextEndpoint:
    """Run on the separately admitted Linux host; the anchor is provisioned out of band."""
    def __init__(self, anchor, key, reader=None):
        self.anchor, self.key = validate_anchor(anchor), key
        mac(key, b"")
        self.reader = LinuxReader() if reader is None else reader

    def exchange(self, raw, deadline):
        try:
            deadline.check()
            body = request(raw, self.anchor, self.key)
            require(source_digest() == self.anchor["source_sha256"], "CONTEXT_SOURCE")
            begin = self.reader.monotonic_ns()
            before = self.reader.kernel()
            require(before["kernel_boot_sha256"] == self.anchor["kernel_boot_sha256"] and
                before["namespaces"] == self.anchor["namespaces"] and before["ns_device"] == self.anchor["ns_device"], "CONTEXT_HOST_PLACEMENT")
            require(self.reader.kernel() == before, "CONTEXT_CHANGED")
            sampled = self.reader.monotonic_ns()
            end = clock_ns(begin + body["window_ns"], begin + 1)
            require(begin <= sampled < end, "CONTEXT_EXPIRED")
            fields = [{name: before[name] for name in CONTEXT_HASHES},
                {name: before["namespaces"][name.removeprefix("host_")] for name in HOST_NAMESPACES} |
                    {name: before["namespaces"][name] for name in ("user_ns", "time_ns")} | {"ns_device": before["ns_device"]},
                {"not_before_ns": begin, "not_after_ns": end, "sampled_ns": sampled,
                    "resolution_ns": before["resolution_ns"], "yama_scope": before["yama_scope"], "seccomp_filters": self.anchor["seccomp_filters"]}]
            frames = [canonical({"version": 1, "phase": phase, "request_sha256": sha(raw), "payload": payload}) for phase, payload in zip(PHASES, fields)]
            sealed = canonical({"version": 1, "phase": "SEALED", "request_sha256": sha(raw), "mac": mac(self.key, raw + b"".join(frames))})
            require(all(len(frame) <= LIMITS["receipt_bytes"] for frame in [*frames, sealed]), "CONTEXT_FRAME_BOUND")
            deadline.check()
            return b"".join([*frames, sealed])
        except Exception:
            raise Invalid("CONTEXT_SOURCE_REFUSED") from None


class ContextLease:
    def __init__(self, token, context, binding, clock, end, receipt):
        require(token is _TOKEN, "CONTEXT_LEASE")
        self._context, self.binding, self.clock, self.end, self.receipt = copy.deepcopy(context), dict(binding), clock, end, receipt

    def check(self, deadline):
        require(deadline.clock is self.clock and self.clock() < self.end, "CONTEXT_LEASE_EXPIRED")
        deadline.check()

    def view(self, case_id, invocation_sha256, preparation, deadline):
        self.check(deadline)
        require(self.binding == {"case_id": case_id, "invocation_sha256": invocation_sha256,
            "preparation_sha256": sha(canonical(validate_preparation(preparation)))}, "CONTEXT_LEASE_BINDING")
        return copy.deepcopy(self._context)


class ContextSource:
    """The exchange transports signed bytes only; the secret is never sent to actors."""
    def __init__(self, anchor, key, exchange, *, authorize=None):
        self.anchor, self._key = validate_anchor(anchor), key
        mac(key, b"")
        require(callable(exchange), "CONTEXT_TRANSPORT")
        self.exchange, self.authorize, self.used = exchange, authorize, set()

    def acquire(self, case_id, invocation_sha256, preparation, daemon_sha256, deadline, budget):
        validate_preparation(preparation)
        digest(invocation_sha256)
        require(daemon_sha256 == self.anchor["daemon_sha256"] and
            preparation["artifacts"]["runtime_context_source"] == self.anchor["source_sha256"], "CONTEXT_SOURCE_BINDING")
        require(callable(self.authorize) and self.authorize("runtime_context", invocation_sha256, sha(canonical(self.anchor))) is True, "CONTEXT_NOT_APPROVED")
        started = deadline.clock()
        window = min(LIMITS["admission_ms"] * 1000000, int(deadline.remaining() * 1000000000))
        require(window > 0 and len(self.used) < LIMITS["receipts"], "CONTEXT_BOUNDS")
        challenge = secrets.token_hex(32)
        digest(challenge)
        require(challenge not in self.used, "CONTEXT_REPLAY")
        self.used.add(challenge)
        binding = {"case_id": case_id, "invocation_sha256": invocation_sha256, "preparation_sha256": sha(canonical(preparation))}
        body = binding | {"version": 1, "anchor_sha256": sha(canonical(self.anchor)), "challenge": challenge, "window_ns": window}
        raw = canonical({"body": body, "mac": mac(self._key, canonical(body))})
        request(raw, self.anchor, self._key)
        deadline.check()
        try:
            received = self.exchange(raw, deadline)
            require(type(received) is bytes and 0 < len(received) <= 4 * LIMITS["receipt_bytes"], "CONTEXT_OUTPUT_BOUND")
            budget.consume(len(received))
            parts = received.splitlines(keepends=True)
            require(len(parts) == 4, "CONTEXT_FRAMES")
            values = [decode(part, LIMITS["receipt_bytes"]) for part in parts]
            for value, phase in zip(values, (*PHASES, "SEALED")):
                budget.frame()
                exact(value, ("version", "phase", "request_sha256", "mac" if phase == "SEALED" else "payload"))
                integer(value["version"], 1, 1)
                require(value["phase"] == phase and value["request_sha256"] == sha(raw), "CONTEXT_RESPONSE_BINDING")
            require(type(values[3]["mac"]) is str and hmac.compare_digest(values[3]["mac"], mac(self._key, raw + b"".join(parts[:3]))), "CONTEXT_AUTH")
            exact(values[0]["payload"], CONTEXT_HASHES)
            exact(values[1]["payload"], (*HOST_NAMESPACES, "user_ns", "time_ns", "ns_device"))
            exact(values[2]["payload"], ("not_before_ns", "not_after_ns", "sampled_ns", "resolution_ns", "yama_scope", "seccomp_filters"))
            window_data = dict(values[2]["payload"])
            sampled = clock_ns(window_data.pop("sampled_ns"), 1)
            context = validate_context(values[0]["payload"] | values[1]["payload"] | window_data)
            require(context["not_after_ns"] - context["not_before_ns"] == window and context["not_before_ns"] <= sampled < context["not_after_ns"], "CONTEXT_WINDOW")
            require(context["kernel_boot_sha256"] == self.anchor["kernel_boot_sha256"] and context["ns_device"] == self.anchor["ns_device"] and
                context["seccomp_filters"] == self.anchor["seccomp_filters"], "CONTEXT_ANCHOR")
            for name in (*HOST_NAMESPACES, "user_ns", "time_ns"):
                require(context[name] == self.anchor["namespaces"][name.removeprefix("host_")], "CONTEXT_ANCHOR")
            end = min(deadline.end, started + window / 1000000000)
            require(deadline.clock() < end, "CONTEXT_LEASE_EXPIRED")
            receipt = {"context_sha256": sha(canonical(context)), "response_sha256": sha(received), "request_sha256": sha(raw),
                "source_sha256": self.anchor["source_sha256"], "anchor_sha256": sha(canonical(self.anchor)),
                "execution_authorized": False, "runtime_evidence": False}
            return ContextLease(_TOKEN, context, binding, deadline.clock, end, receipt)
        except Exception:
            raise Invalid("CONTEXT_ACQUISITION_REJECTED") from None
