"""Local fakes only: no Docker daemon, real Redis or external socket is used."""
from __future__ import annotations

import copy
import hashlib
import json
from pathlib import Path
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

import controller as ctl
import executor as worker
import harness as h
import resp
import runtime_case as case


def test_plan(scenario="ledger-smoke"):
    return h.compile_plan({"format_version": 1, "scenario": scenario, "redis_version": "7.2.0",
                           "redis_image": "sha256:" + "1" * 64, "harness_image": "sha256:" + "2" * 64,
                           "standin_image": "sha256:" + "2" * 64})


def approval(plan):
    return {"version": 1, "case": case.case_for_plan(plan), "approved": True, "operator": "unit-test",
            "commit": "a" * 40, "plan_sha256": h.digest(h.canonical(plan)),
            "recipe_sha256": case.recipe_sha256(case.case_for_plan(plan)), "expires_at_ms": int(time.time() * 1000) + 1000000,
            "max_seconds": 120, "architecture": "arm64"}


def inspected_container(spec):
    return {"Image": spec["image"], "Config": {"User": spec["uid"], "Labels":
                {ctl.LABEL: spec["fixture_id"], "io.mifolyo.cj2.case": spec.get("case", case.CASE)}},
            "State": {"Running": False, "Pid": 0}, "HostConfig": {
                "NetworkMode": "none", "ReadonlyRootfs": True, "Privileged": False,
                "Memory": spec["memory"], "MemorySwap": spec["memory"], "PidsLimit": 64,
                "NanoCpus": 1000000000, "RestartPolicy": {"Name": "no"}, "PortBindings": {},
                "PublishAllPorts": False, "CapDrop": ["ALL"], "CapAdd": spec["cap_add"],
                "SecurityOpt": ["no-new-privileges:true"], "Binds": [], "Devices": [], "PidMode": "", "IpcMode": "private",
                "Tmpfs": {"/tmp": "rw,noexec,nosuid,size=16777216"}},
            "Mounts": [{"Type": "volume", "Name": name, "Destination": target, "RW": not readonly}
                       for name, target, readonly in spec["mounts"]]}


class FakeDocker:
    evidence_kind = "simulated"

    def __init__(self, fail=None):
        self.resources = {("volume", "retained-evidence"): {"Labels": {"unrelated": "true"}}}
        self.events, self.passwords = [], set()
        self.fail = fail

    def inspect(self, kind, name):
        if kind == "image":
            return {"Id": name, "Os": "linux", "Architecture": "arm64", "RootFS": {"Layers": ["sha256:" + "3" * 64]},
                    "Config": {"Env": [], "Volumes": {}, "OnBuild": []}}
        return self.resources.get((kind, name))

    def volume(self, name, fixture_id):
        self.resources[("volume", name)] = {"Labels": {ctl.LABEL: fixture_id, "io.mifolyo.cj2.case": getattr(self, "case_id", case.CASE)}}
        if self.fail == "ambiguous-volume":
            raise ctl.CommandError("lost create reply")

    def create(self, spec):
        self.resources[("container", spec["name"])] = inspected_container(spec)
        self.events.append("create:" + spec["role"])
        if self.fail == "ambiguous-redis" and spec["role"] == "redis":
            self.resources[("container", spec["name"])]["State"]["Running"] = True
            raise ctl.CommandError("lost create reply")

    def start(self, name):
        self.resources[("container", name)]["State"]["Running"] = True
        self.resources[("container", name)]["State"]["Pid"] = 1234
        self.events.append("start:" + name.rsplit("-", 1)[-1])

    def kill(self, name):
        self.resources[("container", name)]["State"]["Running"] = False
        self.resources[("container", name)]["State"]["Pid"] = 0
        self.events.append("kill" if name.endswith("-redis") else "kill:" + name.rsplit("-", 1)[-1])

    def call(self, *args, **_):
        if args[:4] == ("container", "kill", "--signal", "KILL"):
            self.kill(args[4])
            return b""
        if args[:2] == ("container", "wait"):
            assert not self.resources[("container", args[2])]["State"]["Running"]
            self.events.append("wait:" + args[2].rsplit("-", 1)[-1])
            return b"137\n"
        raise AssertionError("Unexpected fake Docker command")

    def quiesce(self, name, fixture_id):
        self.events.append("quiesce:" + name.rsplit("-", 1)[-1])
        return ctl.Docker.quiesce(self, name, fixture_id)

    def stage(self, name, stage, request, timeout):
        self.events.append(stage)
        self.passwords.update(request["credentials"].values())
        if stage == "measure":
            assert set(request["credentials"]) == {"ledger", "observer"}
        if self.fail == stage:
            raise ctl.CommandError("stage failure")
        if self.fail == "interrupt" and stage == "measure":
            raise KeyboardInterrupt()
        result = {"simulated_stage": stage}
        if stage == "init":
            result = {"empty_volumes_verified": True, "config_sha256": request["plan"]["redis_config"]["sha256"],
                      "acl_file_sha256": h.digest(case.acl_file(request["credentials"]))}
        if stage in ("resume", "revoke"):
            result = {"early_revocation" if stage == "resume" else "revocation":
                      {role: {"reconnect": "denied", "server_reachable": True, "held_session": "terminated"}
                       for role in (case.EARLY_ROLES if stage == "resume" else case.ROLES)}}
        if self.fail == "missing-proof" and stage == "revoke":
            result = {"revocation": {}}
        return {"stage": stage, "status": "PASS", "recipe_sha256": case.recipe_sha256(),
                "isolation": {"interfaces": ["lo"]}, "result": result}

    def remove(self, kind, name):
        self.events.append("remove:" + kind)
        if self.fail == "cleanup-volume" and kind == "volume":
            raise ctl.CommandError("removal failed")
        del self.resources[(kind, name)]


class ControllerTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.plan = test_plan()

    def run_case(self, fake):
        return ctl.execute(self.plan, approval(self.plan), fake, revision_check=lambda _: None)

    def test_order_and_no_real_acceptance_for_simulation(self):
        fake = FakeDocker()
        report = self.run_case(fake)
        self.assertEqual(report["verdict"], "PASS")
        self.assertFalse(report["case_evidence_valid"])
        self.assertFalse(report["m4_accepted"])
        self.assertEqual(report["evidence_kind"], "simulated")
        stages = [item for item in fake.events if ":" not in item]
        self.assertEqual(stages, ["init", "ready", "probe", "kill", "ready", "resume", "measure", "revoke"])
        self.assertEqual(fake.events.count("start:redis"), 2)
        self.assertEqual(set(fake.resources), {("volume", "retained-evidence")})
        for password in fake.passwords:
            self.assertNotIn(password.encode(), h.canonical(report))

    def test_failures_interrupts_and_lost_replies_always_cleanup(self):
        for failure in ("init", "probe", "resume", "measure", "interrupt", "ambiguous-volume", "ambiguous-redis", "revoke", "missing-proof"):
            with self.subTest(failure=failure):
                fake = FakeDocker(failure)
                report = self.run_case(fake)
                self.assertEqual(report["verdict"], "FAIL")
                self.assertFalse(report["case_evidence_valid"])
                self.assertEqual(set(fake.resources), {("volume", "retained-evidence")})
                if failure in ("resume", "measure", "interrupt", "ambiguous-redis"):
                    self.assertIn("revoke", fake.events)

    def test_cleanup_failure_invalidates_success(self):
        report = self.run_case(FakeDocker("cleanup-volume"))
        self.assertTrue(report["case_passed"])
        self.assertEqual(report["verdict"], "FAIL")
        self.assertTrue(any(not row["removed"] for row in report["cleanup"]))

    def test_cleanup_never_deletes_unowned_resource_or_treats_daemon_error_as_absence(self):
        fake = FakeDocker()
        result = ctl.cleanup(fake, [("volume", "retained-evidence")], "a" * 32)
        self.assertFalse(result[0]["removed"])
        self.assertIn(("volume", "retained-evidence"), fake.resources)
        with patch.object(fake, "inspect", side_effect=ctl.CommandError("daemon unavailable")):
            result = ctl.cleanup(fake, [("volume", "anything")], "a" * 32)
        self.assertFalse(result[0]["removed"])

    def test_approval_refused_before_backend_activity(self):
        for field, value in (("approved", 1), ("approved", False), ("version", True),
                             ("expires_at_ms", 1), ("max_seconds", 301), ("case", "other"),
                             ("plan_sha256", "0" * 64), ("recipe_sha256", "0" * 64),
                             ("architecture", "mips"), ("extra", "x")):
            fake = FakeDocker()
            changed = approval(self.plan)
            changed[field] = value
            with self.subTest(field=field), self.assertRaises(h.InvalidArtifact):
                ctl.execute(self.plan, changed, fake, revision_check=lambda _: None)
            self.assertEqual(fake.events, [])
        with self.assertRaises(h.InvalidArtifact):
            ctl.execute(self.plan, approval(self.plan), FakeDocker(),
                        revision_check=lambda _: h.require(False, "DIRTY_INPUT"))

    def test_container_and_image_admission_rejects_mutations(self):
        spec = ctl.container_spec("fixture", "executor", "a" * 32, "sha256:" + "2" * 64,
                                  {"control": "own-control", "data": "own-data"})
        pristine = inspected_container(spec)
        for field, value in (("NetworkMode", "host"), ("Privileged", True), ("ReadonlyRootfs", False),
                             ("Memory", 0), ("MemorySwap", -1), ("PidsLimit", -1),
                             ("PortBindings", {"6379/tcp": [{}]}), ("CapAdd", ["SYS_ADMIN"]),
                             ("SecurityOpt", []), ("Binds", ["/var/run/docker.sock:/docker.sock"]),
                             ("Tmpfs", {"/": "rw"}),
                             ("RestartPolicy", {"Name": "always"})):
            value_under_test = copy.deepcopy(pristine)
            value_under_test["HostConfig"][field] = value
            with self.subTest(field=field), self.assertRaises(h.InvalidArtifact):
                ctl.verify_container(value_under_test, spec, False)
        value_under_test = copy.deepcopy(pristine)
        value_under_test["Mounts"][0]["RW"] = True
        with self.assertRaises(h.InvalidArtifact):
            ctl.verify_container(value_under_test, spec, False)
        image = FakeDocker().inspect("image", spec["image"])
        for field, value in (("Id", "sha256:" + "5" * 64), ("Os", "windows"), ("Architecture", "amd64")):
            changed = {**image, field: value}
            with self.assertRaises(h.InvalidArtifact):
                ctl.image_admission(changed, spec["image"], "arm64", True)
        image["Config"]["Env"] = ["HTTPS_PROXY=secret-canary"]
        with self.assertRaises(h.InvalidArtifact):
            ctl.image_admission(image, spec["image"], "arm64", True)

    def test_docker_command_is_local_portless_named_volume_only(self):
        with patch.object(ctl.shutil, "which", return_value="/fake/docker"):
            docker = ctl.Docker()
        spec = ctl.container_spec("fixture", "redis", "a" * 32, "sha256:" + "1" * 64,
                                  {"control": "own-control", "data": "own-data"})
        with patch.object(docker, "call") as called:
            docker.create(spec)
        args = called.call_args.args
        self.assertEqual(docker.prefix, ["/fake/docker", "--host", "unix:///var/run/docker.sock"])
        self.assertIn("never", args)
        self.assertNotIn("--publish", args)
        self.assertNotIn("--privileged", args)
        self.assertTrue(all("type=volume" in args[i + 1] and "volume-nocopy" in args[i + 1]
                            for i, part in enumerate(args) if part == "--mount"))

    def test_bounded_commands_and_exclusive_evidence_files(self):
        with self.assertRaises(ctl.CommandError):
            ctl.command([sys.executable, "-c", "import time; time.sleep(5)"], timeout=0.05)
        with self.assertRaises(ctl.CommandError):
            ctl.command([sys.executable, "-c", "import os; os.write(1, b'x' * 3000000)"], timeout=5)
        report = {"fixture_id": "a" * 32, "verdict": "FAIL"}
        with tempfile.TemporaryDirectory() as directory:
            path = ctl.write_report(Path(directory), report)
            self.assertEqual(json.loads(path.read_bytes()), report)
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            with self.assertRaises(FileExistsError):
                ctl.write_report(Path(directory), report)

    def test_intent_precedes_resources_and_failed_journal_prevents_execution(self):
        fake = FakeDocker()
        intents = []
        def journal(value):
            self.assertEqual(fake.events, [])
            self.assertEqual(value["status"], "INCOMPLETE")
            self.assertFalse(value["m4_accepted"])
            intents.append(value)
        report = ctl.execute(self.plan, approval(self.plan), fake, revision_check=lambda _: None, journal=journal)
        self.assertEqual(report["intent_sha256"], h.digest(h.canonical(intents[0])))
        fake = FakeDocker()
        with self.assertRaises(OSError):
            ctl.execute(self.plan, approval(self.plan), fake, revision_check=lambda _: None,
                        journal=lambda _: (_ for _ in ()).throw(OSError("unwritable")))
        self.assertEqual(fake.events, [])


class SocketFake:
    def __init__(self, raw, fragment=3):
        self.raw, self.fragment, self.sent, self.closed = raw, fragment, [], False

    def settimeout(self, _):
        pass

    def sendall(self, raw):
        self.sent.append(raw)

    def recv(self, count):
        chunk = self.raw[:min(count, self.fragment)]
        self.raw = self.raw[len(chunk):]
        return chunk

    def close(self):
        self.closed = True


class TransportTests(unittest.TestCase):
    def test_fragmented_binary_and_null_replies(self):
        sock = SocketFake(b"*4\r\n+OK\r\n:0\r\n$3\r\nx\x00y\r\n$-1\r\n")
        with resp.Client(connection=sock) as client:
            self.assertEqual(client.call("TEST", b"a\x00b"), [b"OK", 0, b"x\x00y", None])
        self.assertTrue(sock.closed)
        self.assertIn(b"$3\r\na\x00b\r\n", sock.sent[0])

    def test_reply_bounds_truncation_and_no_retry(self):
        for raw in (b"$999999999\r\n", b"*999999999\r\n", b"*1\r\n" * 8, b"$3\r\nx", b"$-2\r\n", b"!bad\r\n"):
            sock = SocketFake(raw)
            with self.subTest(raw=raw), self.assertRaises(resp.TransportError):
                resp.Client(connection=sock).call("SET", "key", "value")
            self.assertEqual(len(sock.sent), 1)
            self.assertTrue(sock.closed)

    def test_error_redaction_and_outgoing_bounds(self):
        for raw, code in ((b"-NOPERM secret-canary\r\n", "NOPERM"),
                          (b"-ERR CRAWL_V2_BOOT_UNAPPROVED secret-canary\r\n", "CRAWL_V2_BOOT_UNAPPROVED")):
            with self.assertRaises(resp.RedisError) as raised:
                resp.Client(connection=SocketFake(raw)).call("PING")
            self.assertEqual(raised.exception.code, code)
            self.assertNotIn("secret-canary", str(raised.exception))
        with self.assertRaises(resp.TransportError):
            resp.encode(("SET", "key", b"x" * resp.MAX_REQUEST))


class RedisFake:
    """Reply/ledger facade, not Lua execution or evidence of Redis semantics."""
    def __init__(self):
        self.data, self.revoked, self.calls, self.scripts = {}, set(), [], {}
        self.run_id, self.ms = "a" * 40, 1000

    def connect(self, role, credentials):
        if role in self.revoked:
            raise resp.RedisError(b"WRONGPASS synthetic")
        outer = self
        class Connection:
            def __enter__(self):
                return self
            def __exit__(self, *_):
                pass
            def close(self):
                pass
            def observe_peer_disconnect(self):
                if role in outer.revoked:
                    return "eof"
                raise resp.TransportError("PEER_STILL_CONNECTED")
            def call(self, *args):
                if role in outer.revoked:
                    raise resp.PeerDisconnected("PEER_EOF")
                return outer.call(role, *args)
        return Connection()

    def call(self, role, *args):
        self.calls.append((role, *args))
        cmd = args[0]
        if role == "ledger" and len(args) > 1 and args[1] in h.ABSENCE_ONLY and cmd != "TYPE":
            raise resp.RedisError(b"NOPERM synthetic")
        if cmd == "PING":
            return b"PONG"
        if cmd == "TIME":
            return [b"1", b"0"]
        if cmd == "CONFIG":
            return [v.encode() for item in case.CONFIG.items() for v in item]
        if cmd == "INFO":
            return {"SERVER": f"run_id:{self.run_id}\r\nredis_version:7.2.0\r\n".encode(),
                    "PERSISTENCE": b"aof_enabled:1\r\naof_last_write_status:ok\r\nloading:0\r\n",
                    "MEMORY": b"used_memory:2000000\r\n", "CLUSTER": b"cluster_enabled:0\r\n",
                    "REPLICATION": b"role:master\r\nconnected_slaves:0\r\n"}[args[1]]
        if cmd == "DBSIZE":
            return len(self.data)
        if cmd == "SCAN":
            return [b"0", [key.encode() for key in sorted(self.data)]]
        if cmd == "ACL":
            name = args[2].removeprefix("cj2_")
            changed = name not in self.revoked
            self.revoked.add(name)
            return int(changed)
        if cmd == "SCRIPT":
            sha = hashlib.sha1(args[2]).hexdigest()
            self.scripts[sha] = "boot" if b"local durability_key" in args[2] else "maintain"
            return sha.encode()
        if cmd == "EVALSHA":
            if self.scripts[args[1]] == "maintain":
                return [b"BATCH_DONE", b"1000", b"0", b"0"]
            existing = h.AUTH[0] in self.data
            values = ("1", "approved", args[4], args[5], "1000", "", "", "initial", "", args[6], args[7], "0")
            self.data[h.AUTH[0]] = {key: value.encode() for key, value in zip(worker.BOOT_FIELDS, values)}
            return [b"EXISTS_IDENTICAL" if existing else b"OK", b"1000", args[5].encode()]
        key = args[1]
        value = self.data.get(key)
        if cmd == "TYPE":
            return b"none" if value is None else (b"hash" if type(value) is dict else b"string")
        if cmd == "SET":
            self.data[key] = args[2].encode()
            return b"OK"
        if cmd == "GET":
            return value
        if cmd == "DEL":
            del self.data[key]
            return 1
        if cmd == "PTTL":
            return -1 if value is not None else -2
        if cmd == "HSET":
            self.data[key] = {k: v.encode() for k, v in zip(args[2::2], args[3::2])}
            return len(self.data[key])
        if cmd == "HLEN":
            return len(value)
        if cmd == "HSTRLEN":
            return len(value[args[2]])
        if cmd == "HMGET":
            return [value.get(k) for k in args[2:]]
        if cmd == "DUMP":
            return repr(sorted(value.items()) if type(value) is dict else value).encode()
        raise AssertionError("Unexpected fake Redis command")


class ExecutorTests(unittest.TestCase):
    def request(self):
        return {"plan": test_plan(), "recipe_sha256": case.recipe_sha256(), "fixture_id": "1" * 32,
                "credentials": {role: hashlib.sha256(role.encode()).hexdigest() for role in case.ROLES}, "previous": {}}

    def test_probe_boot_setup_measure_and_revocation_facade(self):
        redis, request = RedisFake(), self.request()
        with patch.object(worker, "connect", redis.connect):
            request["previous"] = worker.probe(request)
            redis.run_id = "b" * 40
            resumed = worker.resume(request)
            self.assertEqual(redis.revoked, set(case.EARLY_ROLES))
            request["previous"] = resumed
            result = worker.measure(request)
            self.assertEqual(len(result["acl_negatives"]), 21)
            self.assertEqual(set(redis.data), set(case.STORED_KEYS))
            self.assertEqual(result["active_replies"], [["BATCH_DONE", "1000", "0", "0"]] * 2)
            revoked = worker.revoke(request["credentials"], case.ROLES)
            self.assertEqual(set(revoked), set(case.ROLES))
        self.assertNotIn("SCRIPT", [entry[1] for entry in redis.calls if entry[0] == "ledger"])
        self.assertTrue(all(entry[3] == "1" and entry[4] == h.AUTH[0]
                            for entry in redis.calls if entry[:2] == ("boot", "EVALSHA")))

    def test_probe_loss_or_same_process_blocks_boot(self):
        for fault in ("lost", "changed", "same_run"):
            redis, request = RedisFake(), self.request()
            with self.subTest(fault=fault), patch.object(worker, "connect", redis.connect):
                request["previous"] = worker.probe(request)
                if fault != "same_run":
                    redis.run_id = "b" * 40
                if fault == "lost":
                    redis.data.clear()
                if fault == "changed":
                    redis.data[case.PROBE] = b"different"
                with self.assertRaises(h.InvalidArtifact):
                    worker.resume(request)
                self.assertFalse(any(entry[1] == "EVALSHA" for entry in redis.calls))

    def test_unexpected_key_or_missing_setup_record_fails_before_measurement(self):
        redis, request = RedisFake(), self.request()
        with patch.object(worker, "connect", redis.connect):
            request["previous"] = worker.probe(request)
            redis.run_id = "b" * 40
            request["previous"] = worker.resume(request)
            redis.data["unrelated"] = b"must-not-read"
            with self.assertRaises(h.InvalidArtifact):
                worker.measure(request)
            self.assertFalse(any(entry[:2] == ("ledger", "EVALSHA") for entry in redis.calls))

    def test_acl_recipe_has_no_setup_regrant_or_ledger_marker_mutation(self):
        rules = case.acl_rules()
        self.assertEqual(set(rules), set(case.ROLES))
        self.assertEqual(rules["revoker"], ("+ping +acl|deluser",))
        self.assertFalse(any("+hset" in rule or "+set" in rule or "+script" in rule for rule in rules["ledger"]))
        self.assertFalse(any("+acl|setuser" in rule for role in rules.values() for rule in role))
        raw = case.acl_file(self.request()["credentials"])
        self.assertTrue(raw.startswith(b"user default off"))
        for password in self.request()["credentials"].values():
            self.assertNotIn(password.encode(), raw)


if __name__ == "__main__":
    unittest.main()
