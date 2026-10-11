"""Closed oracle RPC and independent fixed-frame receipt accounting."""
from contracts import LIMITS, TRIALS, canonical, clock_ns, decode, digest, exact, integer, require, sha
from linux_observation import source_hash

SOURCE_FILES = ("contracts.py", "runtime_admission.py", "linux_observation.py", "oracle.py", "oracle_protocol.py", "oracle_worker.py", "worker.py")
ACTIONS = {"INIT": "ORACLE_INITIALIZED", "HELD": "ORACLE_HELD", "FINAL": "ORACLE_FINAL", "CLOSE": "ORACLE_CLOSED"}
CLIENT_FIELDS = ("bytes", "complete_ns", "eof", "closed", "transport_error", "cut_proved", "classification_at_cut", "complete_after_cut")


def source_digest():
    return source_hash(SOURCE_FILES)


def bound(raw, binding, sequence, previous):
    value = decode(raw, LIMITS["receipt_bytes"])
    for key in ("case_id", "invocation_sha256"):
        require(value.get(key) == binding[key], "ORACLE_BINDING")
    integer(value.get("version"), 1, 1)
    integer(value.get("sequence"), sequence, sequence)
    require(value.get("previous") == previous, "ORACLE_CHAIN")
    if previous is not None:
        digest(previous)
    return value


def parse_request(raw, binding, sequence, previous):
    value = bound(raw, binding, sequence, previous)
    exact(value, ("case_id", "invocation_sha256", "version", "sequence", "previous", "action", "payload"))
    require(value["action"] in ACTIONS, "ORACLE_ACTION")
    if value["action"] == "HELD":
        exact(value["payload"], ("observer_sha256",))
        digest(value["payload"]["observer_sha256"])
    else:
        require(value["payload"] is None, "ORACLE_PAYLOAD")
    return value


def client_summary(value):
    exact(value, CLIENT_FIELDS)
    integer(value["bytes"], 0, 1024)
    clock_ns(value["complete_ns"])
    require((value["complete_ns"] > 0) is (value["bytes"] == 1024), "CLIENT_COMPLETE")
    for key in ("eof", "closed", "transport_error", "cut_proved", "complete_after_cut"):
        require(type(value[key]) is bool, "CLIENT_BOOLEAN")
    require(value["classification_at_cut"] in ("unknown", "acknowledged", "known_pre_acknowledgment", "not_applicable"), "CLIENT_CLASSIFICATION")
    if value["classification_at_cut"] in ("acknowledged", "known_pre_acknowledgment"):
        require(value["cut_proved"], "CLIENT_CUT")
    if value["classification_at_cut"] == "acknowledged":
        require(value["bytes"] == 1024 and not value["complete_after_cut"], "CLIENT_ACK")
    if value["complete_after_cut"]:
        require(value["bytes"] == 1024 and value["cut_proved"], "CLIENT_LATE")
    require(not (value["eof"] and value["transport_error"]), "CLIENT_TERMINAL_STATE")
    return value


def response(raw, binding, sequence, previous, phase):
    value = bound(raw, binding, sequence, previous)
    exact(value, ("case_id", "invocation_sha256", "version", "sequence", "previous", "phase", "payload", "execution_authorized", "runtime_evidence"))
    require(value["phase"] == phase and value["execution_authorized"] is False and value["runtime_evidence"] is False, "ORACLE_RESPONSE")
    body = value["payload"]
    if phase == "ORACLE_INITIALIZED":
        exact(body, ("witness", "state_sha256", "client"))
        exact(body["witness"], ("device", "inode", "uid", "gid", "mode", "bytes"))
        integer(body["witness"]["device"], 1)
        integer(body["witness"]["inode"], 1)
        require((body["witness"]["uid"], body["witness"]["gid"], body["witness"]["mode"], body["witness"]["bytes"]) == (999, 999, 0o600, 8480), "WITNESS_IDENTITY")
        digest(body["state_sha256"])
    elif phase == "ORACLE_HELD":
        exact(body, ("witness_sha256", "state_sha256", "observer_sha256", "lower_ns", "upper_ns", "ordinal", "client"))
        for key in ("witness_sha256", "state_sha256", "observer_sha256"):
            digest(body[key])
        clock_ns(body["upper_ns"], clock_ns(body["lower_ns"], 1))
        integer(body["ordinal"], 1, 1050)
    elif phase == "ORACLE_FINAL":
        exact(body, ("witness_sha256", "state_sha256", "post_ns", "client"))
        digest(body["witness_sha256"])
        digest(body["state_sha256"])
        clock_ns(body["post_ns"], 1)
    else:
        require(phase == "ORACLE_CLOSED", "ORACLE_PHASE")
        exact(body, ("witness_sha256", "client"))
        digest(body["witness_sha256"])
        require(body["client"]["closed"] is True, "CLIENT_OPEN")
    client_summary(body["client"])
    enabled = binding["case_id"].startswith(("P3.pre-send.", "P3.received.", "P3.receipt-race."))
    client = body["client"]
    if not enabled:
        require(client["classification_at_cut"] == "not_applicable" and client["bytes"] == client["complete_ns"] == 0 and
            not client["eof"] and not client["transport_error"] and not client["complete_after_cut"], "CLIENT_NOT_APPLICABLE")
    else:
        require(client["classification_at_cut"] != "not_applicable", "CLIENT_REQUIRED")
        if client["classification_at_cut"] == "known_pre_acknowledgment":
            require(binding["case_id"].startswith(("P3.pre-send.", "P3.receipt-race.")), "CLIENT_PREACK_CASE")
    return value


class ClientHistory:
    """Controller-side monotonic history; a valid hash chain alone proves no cut."""
    def __init__(self, case_id):
        require(case_id in TRIALS and not case_id.startswith("N."), "CLIENT_CASE")
        self.case_id = case_id
        self.enabled = case_id.startswith(("P3.pre-send.", "P3.received.", "P3.receipt-race."))
        self.previous, self.cut, self.phase = None, None, None

    def accept(self, phase, payload):
        following = {None: ("ORACLE_INITIALIZED",), "ORACLE_INITIALIZED": ("ORACLE_HELD", "ORACLE_CLOSED"),
            "ORACLE_HELD": ("ORACLE_FINAL", "ORACLE_CLOSED"), "ORACLE_FINAL": ("ORACLE_CLOSED",), "ORACLE_CLOSED": ()}
        require(phase in following[self.phase], "CLIENT_ORDER")
        current = client_summary(payload["client"])
        previous, cut = self.previous, self.cut
        require(current["closed"] is (phase == "ORACLE_CLOSED"), "CLIENT_PHASE")
        if phase == "ORACLE_INITIALIZED":
            require(current["bytes"] == 0 and not current["cut_proved"] and not current["eof"] and
                not current["transport_error"] and not current["complete_after_cut"], "CLIENT_INITIAL")
        else:
            require(current["bytes"] >= previous["bytes"], "CLIENT_ROLLBACK")
            if previous["complete_ns"]:
                require(current["complete_ns"] == previous["complete_ns"], "CLIENT_COMPLETE_CHANGED")
            for flag in ("eof", "transport_error", "cut_proved", "closed"):
                require(not previous[flag] or current[flag], "CLIENT_ROLLBACK")
            if previous["eof"] or previous["transport_error"]:
                require(current["bytes"] == previous["bytes"] and current["complete_ns"] == previous["complete_ns"], "CLIENT_AFTER_TERMINAL")
            if phase == "ORACLE_HELD":
                require(cut is None and current["cut_proved"], "CLIENT_CUT_PHASE")
                require(not current["eof"] and not current["transport_error"], "CLIENT_EARLY_TERMINAL")
                cut = (clock_ns(payload["lower_ns"], 1), clock_ns(payload["upper_ns"], payload["lower_ns"]))
                if self.case_id.startswith("P3.pre-send."):
                    require(current["bytes"] == 0, "CLIENT_PREACK_CONTRADICTION")
                elif self.case_id.startswith("P3.receipt-race."):
                    require(current["bytes"] < 1024, "CLIENT_PREACK_CONTRADICTION")
                expected = "not_applicable"
                if self.enabled:
                    expected = ("known_pre_acknowledgment" if self.case_id.startswith(("P3.pre-send.", "P3.receipt-race."))
                        else "acknowledged" if 0 < current["complete_ns"] < cut[0] else "unknown")
                require(current["classification_at_cut"] == expected, "CLIENT_CUT_CLASSIFICATION")
            elif cut is not None:
                require(current["classification_at_cut"] == previous["classification_at_cut"], "CLIENT_CUT_CHANGED")
                if not previous["complete_ns"] and current["complete_ns"]:
                    require(current["complete_ns"] >= cut[1], "CLIENT_CLOCK_ROLLBACK")
            else:
                require(not current["cut_proved"], "CLIENT_CUT_WITHOUT_HELD")
        if cut is None:
            require(current["classification_at_cut"] == ("unknown" if self.enabled else "not_applicable"), "CLIENT_NO_CUT")
        require(current["complete_after_cut"] is bool(cut is not None and current["complete_ns"] > cut[1]), "CLIENT_LATE_CHANGED")
        self.previous, self.cut, self.phase = dict(current), cut, phase


class ClientTranscript:
    def __init__(self, case_id):
        require(case_id in TRIALS and not case_id.startswith("N."), "CLIENT_CASE")
        self.case_id = case_id
        self.enabled = case_id.startswith(("P3.pre-send.", "P3.received.", "P3.receipt-race."))
        self.data, self.complete_ns, self.last_ns = bytearray(), 0, 0
        self.eof, self.closed, self.cut_proved = False, False, False
        self.transport_error = False
        self.cut_upper = 0
        self.classification = "unknown" if self.enabled else "not_applicable"

    def feed(self, raw, when):
        require(self.enabled and not self.closed and not self.eof and not self.transport_error and type(raw) is bytes and raw, "CLIENT_STATE")
        clock_ns(when, max(1, self.last_ns))
        require(len(self.data) + len(raw) <= 1024 and all(byte == 0x43 for byte in raw), "CLIENT_FRAME")
        self.data.extend(raw)
        self.last_ns = when
        if len(self.data) == 1024:
            self.complete_ns = when

    def finish(self, when):
        require(self.enabled and not self.closed and not self.eof and not self.transport_error, "CLIENT_EOF")
        self.last_ns = clock_ns(when, max(1, self.last_ns))
        self.eof = True

    def cut(self, lower, upper):
        require(not self.cut_proved and not self.closed, "CLIENT_CUT")
        clock_ns(upper, clock_ns(lower, 1))
        self.cut_proved, self.cut_upper = True, upper
        self.last_ns = max(self.last_ns, upper)
        if not self.enabled:
            return
        require(not self.eof and not self.transport_error, "CLIENT_EARLY_EOF")
        if self.case_id.startswith(("P3.pre-send.", "P3.receipt-race.")):
            maximum = 0 if self.case_id.startswith("P3.pre-send.") else 1023
            require(len(self.data) <= maximum, "CLIENT_PREACK_CONTRADICTION")
            self.classification = "known_pre_acknowledgment"
        elif self.complete_ns and self.complete_ns < lower:
            self.classification = "acknowledged"

    def summary(self):
        return client_summary({"bytes": len(self.data), "complete_ns": self.complete_ns, "eof": self.eof,
            "closed": self.closed, "transport_error": self.transport_error, "cut_proved": self.cut_proved, "classification_at_cut": self.classification,
            "complete_after_cut": bool(self.cut_proved and self.complete_ns > self.cut_upper)})
