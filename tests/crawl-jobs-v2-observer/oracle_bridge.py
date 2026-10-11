"""Join the owned, admitted oracle's chained replies to controller decisions."""
import copy
from contracts import LIMITS, Invalid, canonical, decode, require, sha
from owned_resources import ACTORS, VOLUMES, Registry, admit_container, timestamp, validate_snapshot
import oracle_protocol as wire


def _fail_closed(operation):
    def checked(self, *args, **kwargs):
        try:
            return operation(self, *args, **kwargs)
        except BaseException:
            self.failed = True
            try:
                self.stream.close()
            except Exception:
                pass  # Remote disposal remains the independent watchdog's task.
            raise
    return checked


class OracleSession:
    def __init__(self, backend, stream, ref, snapshot, preparation):
        admitted = backend._native_admissions.get("oracle")
        require(admitted is not None and admitted["stream"] is stream and backend._actor_streams.get(ref["id"]) is stream,
            "ORACLE_NOT_ADMITTED")
        require(admitted["validated"]["preparation_sha256"] == sha(canonical(preparation)), "ORACLE_PREPARATION")
        self.backend, self.stream, self.ref = backend, stream, dict(ref)
        self.snapshot = validate_snapshot(snapshot)
        require(canonical(self.snapshot["scope"]) == canonical(backend.scope) and
            self.snapshot["entries"].get("oracle", {}).get("reference") == ref, "ORACLE_SNAPSHOT_BINDING")
        self.binding = {key: stream.binding[key] for key in ("case_id", "invocation_sha256")}
        self.sequence, self.previous, self.phase, self.failed = 0, None, "READY", False
        self.identity_sha256, self.matched_observer, self.closed_receipt = None, None, None
        self.client_history = wire.ClientHistory(self.binding["case_id"])

    def _exchange(self, action, payload=None):
        require(not self.failed and self.sequence < LIMITS["receipts"], "ORACLE_SESSION")
        try:
            self.backend._authorize("oracle_evidence")
            request = canonical({**self.binding, "version": 1, "sequence": self.sequence, "previous": self.previous,
                "action": action, "payload": payload})
            wire.parse_request(request, self.binding, self.sequence, self.previous)
            self.stream._send(request)
            while not self.stream.frames:
                self.stream.deadline.check()
                require(not self.stream.stdout_eof, "ORACLE_EOF")
                self.stream._pump(min(self.stream.deadline.remaining(), 0.01))
            self.stream._pump(0)
            raw = self.stream.frames.pop(0)
            value = wire.response(raw, self.binding, self.sequence, self.previous, wire.ACTIONS[action])
            require(not self.stream.frames and not self.stream.decoder.partial, "ORACLE_EXTRA_OUTPUT")
            self.stream.deadline.check()
            if self.identity_sha256 is not None:
                require(value["payload"].get("witness_sha256") == self.identity_sha256, "ORACLE_WITNESS_CHANGED")
            self.client_history.accept(value["phase"], value["payload"])
            self.previous, self.sequence = sha(raw), self.sequence + 1
            self.stream.phase = value["phase"]
            self.phase = "INITIALIZED" if action == "INIT" else action
            return value
        except BaseException:
            self.failed = True
            self.stream.close()
            raise

    @_fail_closed
    def initialize(self):
        require(self.phase == "READY", "ORACLE_ORDER")
        result = self._exchange("INIT")
        self.identity_sha256 = sha(canonical(result["payload"]["witness"]))
        self.require_ready()
        return result

    def require_ready(self):
        require(not self.failed and self.phase == "INITIALIZED", "ORACLE_NOT_READY")
        self.stream.deadline.check()
        self.stream.require_quiet_live("ORACLE_INITIALIZED")
        self.stream.deadline.check()
        admitted = self.backend._native_admissions.get("oracle")
        require(admitted is not None and admitted["stream"] is self.stream and self.stream.deadline.clock() < admitted["expires_at"], "ORACLE_EXPIRED")

    @_fail_closed
    def observe_held(self, observer):
        require(not self.failed and self.phase == "INITIALIZED", "ORACLE_ORDER")
        admitted = self.backend._native_admissions.get("observer")
        require(admitted is not None and admitted["stream"] is observer and
            all(observer.binding[key] == self.binding[key] for key in self.binding), "ORACLE_OBSERVER_BINDING")
        observer.require_quiet_live("HELD")
        target = self.backend._native_admissions.get("target", {}).get("stream")
        require(target is not None, "ORACLE_TARGET_REQUIRED")
        target.require_quiet_live("START_DISPATCHED")
        observed = observer.last_message_sha256
        require(observed is not None and observer.last_message is not None, "ORACLE_HELD_REQUIRED")
        result = self._exchange("HELD", {"observer_sha256": observed})
        require(result["payload"]["observer_sha256"] == observed and
            result["payload"]["ordinal"] == observer.last_message["ordinal"], "ORACLE_HELD_JOIN")
        observer.require_quiet_live("HELD")
        target.require_quiet_live("START_DISPATCHED")
        self.matched_observer = observed
        self.held_upper_ns = result["payload"]["upper_ns"]
        return result

    @_fail_closed
    def advance_held(self, coordinator, observer):
        require(coordinator.phase == "HELD" and coordinator.case_id == self.binding["case_id"] and
            coordinator.invocation_sha256 == self.binding["invocation_sha256"], "ORACLE_COORDINATOR")
        self.observe_held(observer)
        return coordinator.advance({**self.binding, "sequence": coordinator.count, "previous": coordinator.previous,
            "kind": "ORACLE_MATCHED", "payload": {"evidence_sha256": self.previous}})

    def authorize_resume(self, observer):
        require(not self.failed and self.phase == "HELD" and self.matched_observer is not None and
            observer.last_message_sha256 == self.matched_observer, "ORACLE_MATCH_REQUIRED")
        require(self.backend._native_admissions.get("observer", {}).get("stream") is observer, "ORACLE_OBSERVER_BINDING")
        self.stream.deadline.check()
        self.stream.require_quiet_live("ORACLE_HELD")
        self.stream.deadline.check()

    @_fail_closed
    def observe_final(self, target):
        require(not self.failed and self.phase == "HELD", "ORACLE_ORDER")
        require(self.backend._native_admissions.get("target", {}).get("stream") is target and target.phase == "FINISHED", "ORACLE_TARGET_NOT_FINISHED")
        result = self._exchange("FINAL")
        require(result["payload"]["post_ns"] >= self.held_upper_ns, "ORACLE_FINAL_CLOCK")
        return result

    @_fail_closed
    def close(self):
        require(self.phase in ("INITIALIZED", "HELD", "FINAL"), "ORACLE_ORDER")
        result = self._exchange("CLOSE")
        self.closed_receipt = copy.deepcopy(result)
        self.stream.phase = "FINISHED"
        self.stream.finish_transport()
        return result

    def closure(self, deadline):
        """A closed socket/CLI alone never proves the two remote actors have ended."""
        require(not self.failed and self.phase == "CLOSE" and self.closed_receipt is not None and self.stream.stdout_eof,
            "ORACLE_CLOSURE_REQUIRED")
        self.backend._authorize("oracle_closure")
        registry = object.__new__(Registry)
        registry.scope, registry.entries = self.backend.scope, self.snapshot["entries"]
        volumes = {role: registry.entries[role]["reference"] for role in VOLUMES}
        def stopped():
            observations = {}
            for role in ("target", "oracle"):
                ref = registry.entries[role]["reference"]
                original = self.backend._runtime_starts.get(ref["id"])
                require(original is not None, "ORACLE_PROCESS_GENERATION")
                actual = self.backend.inspect("container", ref["id"], deadline)
                admit_container(actual, registry.spec(role), volumes, phase="cleanup", bound=ref)
                state = actual["State"]
                require(actual.get("RestartCount") == 0 and timestamp(state.get("StartedAt")) == original["started_at"] and
                    state["Running"] is False and state["Pid"] == 0 and state["Status"] == "exited", "ORACLE_REMOTE_NOT_STOPPED")
                if role == "oracle":
                    require(state["ExitCode"] == 0 and state["OOMKilled"] is False, "ORACLE_WORKER_FAILED")
                observations[role] = sha(canonical(actual))
            return observations
        observations = stopped()
        deadline.check()
        self.backend._authorize("oracle_closure")
        require(stopped() == observations, "ORACLE_CLOSURE_CHANGED")
        deadline.check()
        client = self.closed_receipt["payload"]["client"]
        return {**self.binding, "oracle_receipt_sha256": self.previous, "remote_observations": observations,
            "target_stopped": True, "client_retired": True, "client": dict(client),
            "closed_no_reply_window": client["classification_at_cut"] == "known_pre_acknowledgment" and
                self.matched_observer is not None and self.client_history.cut is not None and
                client["cut_proved"] and client["closed"] and client["eof"] and not client["transport_error"] and client["bytes"] < 1024,
            "execution_authorized": False, "runtime_evidence": False, "cleanup_required": True}
