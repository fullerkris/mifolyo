"""The planned H/I admission matrix. Mutations are synthetic; no unsafe container starts."""
import copy
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import admission
import controller as ctl
import executor as worker
import harness as h
import negative_cases as nc
import negative_specs as ns
import runtime_case as case
import test_execution as base
from test_negative_cases import context, AT, OLD, NEW


class NegativeAdmissionTests(unittest.TestCase):
    def setUp(self):
        self.plan, self.fixture, _ = context(next(iter(ns.STORED)))
        self.spec = ctl.container_spec("fixture-executor", "executor", "a" * 32, "sha256:" + "2" * 64,
                                       {"control": "own-control", "data": "own-data"})
        self.metadata = base.inspected_container(self.spec)
        self.image = base.FakeDocker().inspect("image", self.spec["image"])

    def reject(self, call, expected=None):
        with self.assertRaises(h.InvalidArtifact) as raised:
            call()
        if expected:
            self.assertEqual(str(raised.exception), expected)
        self.assertNotIn("secret-canary", str(raised.exception))

    def field(self, section, name, value, code):
        changed = copy.deepcopy(self.metadata)
        changed[section][name] = value
        self.reject(lambda: ctl.verify_container(changed, self.spec, False), code)

    def artifact(self, label):
        changed = copy.deepcopy(self.plan)
        if label == "missing_field":
            del changed["inputs"]["scenario"]
        elif label == "extra_field":
            changed["endpoint"] = "secret-canary"
        elif label == "duplicate_field":
            self.reject(lambda: h.decode(b'{"x":1,"x":2}'), "DUPLICATE_FIELD")
            return
        elif label == "zero_executable_digest":
            changed["guard_core"]["fields"][4][1] = "0" * 64
        elif label == "changed_source_or_config":
            changed["redis_config"]["sha256"] = "f" * 64
        elif label == "invented_image":
            changed["inputs"]["harness_image"] = "redis:latest"
        elif label == "render_enabled":
            changed["compatibility"]["fields"][-1][1] = "sha256:" + "3" * 64
        else:
            changed["inputs"]["scenario"] = "unapproved-case"
        self.reject(lambda: h.validate_plan(changed))

    def provenance(self, label):
        plan = base.test_plan(ns.CASES[ns.BOOT])
        fid = "1" * 32
        value = "m4-probe:" + fid + ":" + h.digest(h.canonical(plan))
        previous = {"acknowledged": True, "at_ms": AT, "old_run_id": OLD, "key": h.AUTH[2], "type": "string",
                    "value": value, "value_sha256": h.digest(value.encode()), "expiry": "persistent"}
        evidence = {"case": ns.BOOT, "fixture_id": fid, "plan_sha256": h.digest(h.canonical(plan)), "old_run_id": OLD,
                    "new_run_id": NEW, "acknowledged_probe_sha256": previous["value_sha256"], "verified_at_ms": AT + 1, "acknowledged_loss_bound": 0}
        digest = h.digest(h.canonical(evidence))
        nc.validate_probe_evidence(plan, fid, previous, evidence, digest)
        if label == "missing_receipt":
            previous = {}
        elif label == "acknowledged_false":
            previous["acknowledged"] = False
        elif label == "same_run_id":
            evidence["new_run_id"] = OLD
            digest = h.digest(h.canonical(evidence))
        elif label == "wrong_fixture_or_plan":
            evidence["fixture_id"] = "f" * 32
            digest = h.digest(h.canonical(evidence))
        elif label == "changed_probe_bytes_or_hash":
            previous["value"] += "secret-canary"
            previous["value_sha256"] = h.digest(previous["value"].encode())
            evidence["acknowledged_probe_sha256"] = previous["value_sha256"]
            digest = h.digest(h.canonical(evidence))
        elif label == "test_descriptor_substitution":
            evidence = plan["test_descriptors"]["aof_crash_evidence_sha256"]["artifact"]
            digest = h.digest(h.canonical(evidence))
        else:
            digest = "f" * 64
        self.reject(lambda: nc.validate_probe_evidence(plan, fid, previous, evidence, digest))

    def manifest(self, label):
        if label == "setup_regrant":
            self.credentials("restored_setup_role")
            return
        f = copy.deepcopy(self.fixture)
        if label == "duplicate_key":
            f["key_inventory"].append(f["key_inventory"][0])
        elif label == "extra_key":
            f["initial_state"]["secret-canary"] = None
        elif label == "omitted_nonnegative_entry":
            del f["initial_state"][f["worker"]["job_key"]]
        elif label == "wrong_type":
            f["initial_state"][h.AUTH[3]]["type"] = "string"
        elif label == "changed_bytes":
            f["initial_state"][h.AUTH[3]]["fields"][0][1] = "9"
        elif label == "changed_expiry":
            f["initial_state"][h.AUTH[3]]["expires_at_ms"] = AT
        elif label == "out_of_bound":
            f["initial_state"][h.AUTH[3]]["fields"][0][1] = "x" * 16385
        else:
            f["initial_state"][h.AUTH[3]] = None
            f["initial_state"][h.AUTH[4]] = nc.string(self.plan["identities"]["contract_sha256"])
        self.reject(lambda: nc.validate_fixture(self.plan, f), "NEGATIVE_FIXTURE_MISMATCH")

    def preexisting(self, label):
        if label == "retained_name_substitution":
            self.reject(lambda: h.validate_plan(dict(self.plan, volumes={"data": "retained-evidence"})))
            return
        suffix = "-control" if label == "existing_control" else "-data"
        class Existing(base.FakeDocker):
            def inspect(inner, kind, name):
                if kind == "volume" and name.endswith(suffix):
                    inner.resources.setdefault((kind, name), {"Labels": {"foreign": "true"}})
                return super().inspect(kind, name)
        backend = Existing()
        plan = base.test_plan()
        report = ctl.execute(plan, base.approval(plan), backend, revision_check=lambda _: None)
        self.assertEqual(report["verdict"], "FAIL")
        self.assertEqual(report["failure_phase"], "volumes")
        self.assertFalse(any(event.startswith("start:") for event in backend.events))
        self.assertTrue(any(name.endswith(suffix) for kind, name in backend.resources if kind == "volume"))

    def ownership(self, label):
        if label in ("wrong_fixture_label", "wrong_case_label"):
            value = {"Labels": {ctl.LABEL: "a" * 32, "io.mifolyo.cj2.case": case.CASE}}
            value["Labels"][ctl.LABEL if label == "wrong_fixture_label" else "io.mifolyo.cj2.case"] = "wrong"
            self.assertFalse(ctl.owned(value, "volume", "a" * 32))
            return
        volumes = {"control": "own-control", "data": "own-data"}
        allowed = {"executor": {"own-control"}, "redis": {"own-control", "own-data"}}
        attached = {"own-control": ["executor", "redis"], "own-data": ["redis"]}
        admission.volume_attachments(volumes, attached, allowed)
        if label == "same_control_and_data":
            volumes["data"] = volumes["control"]
        else:
            attached["own-control"].append("foreign")
        self.reject(lambda: admission.volume_attachments(volumes, attached, allowed), "VOLUME_SHARED")

    def empty_storage(self, label):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            control, data = root / "control", root / "data"
            control.mkdir(); data.mkdir()
            admission.empty_directories((control, data))
            if label == "symlink_directory":
                link = root / "linked-control"
                link.symlink_to(control, target_is_directory=True)
                control = link
            else:
                ((control if label == "nonempty_control" else data) / "synthetic-canary").write_bytes(b"not fixture data")
            self.reject(lambda: admission.empty_directories((control, data)), "VOLUME_NOT_EMPTY")

    def network_metadata(self, label):
        field, value = {"bridge": ("NetworkMode", "bridge"), "host_network": ("NetworkMode", "host"),
                        "published_port": ("PortBindings", {"6379/tcp": [{}]}), "publish_all": ("PublishAllPorts", True),
                        "external_dns": ("Dns", ["192.0.2.1"]), "extra_host": ("ExtraHosts", ["fixture.invalid:192.0.2.1"])}[label]
        self.field("HostConfig", field, value, "ISOLATION")

    def network_kernel(self, label):
        interfaces = {"lo": {"flags": "0x9", "operstate": "unknown"}}
        v4, v6 = "Iface Destination Gateway Flags\n", ""
        worker.validate_network(interfaces, v4, v6)
        if label == "eth0":
            interfaces["eth0"] = {"flags": "0x0", "operstate": "down"}
            code = "NETWORK_INTERFACE"
        elif label == "active_fallback":
            interfaces["tunl0"] = {"flags": "0x81", "operstate": "up"}
            code = "ACTIVE_INTERFACE"
        elif label == "ipv4_route":
            v4 += "eth0 00000000 0100000A\n"
            code = "IPV4_ROUTE"
        else:
            v6 = "0 " * 9 + "eth0\n"
            code = "IPV6_ROUTE"
        self.reject(lambda: worker.validate_network(interfaces, v4, v6), code)

    def proxy(self, label):
        if label == "image_proxy":
            changed = copy.deepcopy(self.image)
            changed["Config"]["Env"] = ["HTTPS_PROXY=secret-canary"]
            self.reject(lambda: ctl.image_admission(changed, self.spec["image"], "arm64", True), "IMAGE_PROXY")
        else:
            with patch.object(worker.os, "geteuid", return_value=65534), patch.dict(worker.os.environ, {"http_proxy": "secret-canary"}):
                self.reject(worker.environment, "PROXY")

    def mounts(self, label):
        if label in ("host_bind", "docker_socket_bind"):
            self.field("HostConfig", "Binds", ["synthetic-host:/var/run/docker.sock"], "ISOLATION")
            return
        changed = copy.deepcopy(self.metadata)
        if label == "foreign_volume":
            changed["Mounts"][0]["Name"] = "foreign"
        elif label == "executor_data_mount":
            changed["Mounts"].append({"Type": "volume", "Name": "own-data", "Destination": "/data", "RW": False})
        else:
            changed["Mounts"][0]["RW"] = True
        self.reject(lambda: ctl.verify_container(changed, self.spec, False), "MOUNTS")

    def privilege(self, label):
        if label == "runtime_uid_zero":
            self.field("Config", "User", "0:0", "CONTAINER_IDENTITY")
            return
        field, value = {"privileged": ("Privileged", True), "extra_capability": ("CapAdd", ["CHOWN"]),
            "missing_cap_drop": ("CapDrop", []), "host_pid": ("PidMode", "host"), "host_ipc": ("IpcMode", "host"),
            "wrong_memory_or_swap": ("MemorySwap", -1), "wrong_cpu_or_pid_limit": ("PidsLimit", -1),
            "writable_rootfs": ("ReadonlyRootfs", False), "missing_no_new_privileges": ("SecurityOpt", []),
            "wrong_tmpfs": ("Tmpfs", {"/": "rw"}), "restart_policy": ("RestartPolicy", {"Name": "always"})}[label]
        self.field("HostConfig", field, value, "ISOLATION")

    def artifact_binding(self, label):
        if label == "wrong_image_or_architecture":
            self.reject(lambda: ctl.image_admission(self.image, self.spec["image"], "amd64", True), "IMAGE_IDENTITY")
        elif label == "untracked_or_dirty_source":
            for answers, code in (([(0, b"a" * 40, b""), (1, b"", b"")], "UNTRACKED_INPUT"),
                                  ([(0, b"a" * 40, b""), (0, b"", b""), (1, b"", b"")], "DIRTY_INPUT")):
                with patch.object(ctl, "command", side_effect=answers):
                    self.reject(lambda: ctl.verify_revision("a" * 40), code)
        else:
            plan = base.test_plan(self.plan["inputs"]["scenario"])
            approved = base.approval(plan)
            case.validate_approval(plan, approved, int(ctl.time.time() * 1000))
            if label == "wrong_recipe":
                approved["recipe_sha256"] = "f" * 64
                code = "APPROVAL_ARTIFACT_MISMATCH"
            elif label == "swapped_approval_case":
                approved["case"] = ns.BOOT
                code = "EXECUTION_NOT_APPROVED"
            else:
                approved["expires_at_ms"] = 1
                code = "APPROVAL_EXPIRY"
            self.reject(lambda: case.validate_approval(plan, approved, int(ctl.time.time() * 1000)), code)

    def process_environment(self, label):
        if label == "unexpected_entrypoint":
            self.field("Config", "Entrypoint", ["sh"], "CONTAINER_COMMAND")
        elif label == "unexpected_command":
            self.field("Config", "Cmd", ["-c", "secret-canary"], "CONTAINER_COMMAND")
        elif label == "extra_environment_secret_canary":
            self.field("Config", "Env", ["PRODUCTION_SECRET=secret-canary"], "CONTAINER_ENV")
        else:
            expected = ["python3", "-B", admission.ENTRY, "measure", "30000"]
            rows = [{"pid": 1, "argv": ["python3", "-B", admission.ENTRY, "hold"]}, {"pid": 17, "argv": expected}]
            admission.validate_processes(rows, 17, expected)
            rows.append({"pid": 18, "argv": ["synthetic-spider-canary"]})
            self.reject(lambda: admission.validate_processes(rows, 17, expected), "PROCESS_INVENTORY")

    def credentials(self, label):
        selected = "ledger-promote-denied-v1"
        plan = base.test_plan(ns.CASES[selected])
        values = {role: h.digest(role.encode()) for role in case.roles(selected)}
        case.credentials_valid(values, selected)
        self.assertEqual(tuple(values)[-1], "revoker")
        if label == "reused_role_password":
            values["release_admin"] = values["migration_admin"]
            self.reject(lambda: case.credentials_valid(values, selected), "CREDENTIAL_REUSE")
        elif label == "extra_role":
            values["unlisted"] = "a" * 64
            self.reject(lambda: case.credentials_valid(values, selected), "INVALID_FIELDS")
        else:
            request = {"plan": plan, "fixture_id": "1" * 32, "recipe_sha256": case.recipe_sha256(selected), "previous": {},
                       "claim_material": {"owner_a": "2" * 32, "owner_b": "3" * 32, "token_a": "4" * 64, "token_b": "5" * 64, "wrong_token": "6" * 64},
                       "credentials": {role: values[role] for role in (*case.measure_roles(selected), "setup")}}
            self.reject(lambda: worker.validate_request(request, "measure"), "INVALID_FIELDS")

    def configuration(self, label):
        if label == "wrong_config_digest":
            self.artifact("changed_source_or_config")
            return
        config = dict(case.CONFIG)
        changes = {"aof_disabled": ("appendonly", "no"), "wrong_fsync": ("appendfsync", "everysec"),
                   "truncation_enabled": ("aof-load-truncated", "yes"), "eviction_enabled": ("maxmemory-policy", "allkeys-lru"),
                   "memory_below_floor": ("maxmemory", "1024")}
        if label in changes:
            key, value = changes[label]
            config[key] = value
        underlying = base.RedisFake()
        class Injected:
            def call(inner, *args):
                if args[0] == "CONFIG":
                    return [part.encode() for pair in config.items() for part in pair]
                if label == "cluster_or_replica" and args == ("INFO", "CLUSTER"):
                    return b"cluster_enabled:1\n"
                return underlying.call("setup", *args)
        self.reject(lambda: worker.configuration(Injected(), base.test_plan()), "CLUSTER" if label == "cluster_or_replica" else "CONFIG")

    def test_all_82_planned_offline_admission_variants(self):
        raw = (h.HERE / "planning/bootstrap-acl-negatives-v1.json").read_bytes()
        self.assertEqual(h.digest(raw), "cf418fad45d1c0522bcf1b51cd039a0dc36bfa3b1edc881424b32bba4f569823")
        packet = json.loads(raw)
        handlers = {"H01": self.artifact, "H02": self.provenance, "H03": self.manifest,
                    "I01": self.preexisting, "I02": self.ownership, "I03": self.empty_storage,
                    "I04": self.network_metadata, "I05": self.network_kernel, "I06": self.proxy, "I07": self.mounts,
                    "I08": self.privilege, "I09": self.artifact_binding, "I10": self.process_environment,
                    "I11": self.credentials, "I12": self.configuration}
        self.assertEqual({row["assertion_id"] for row in packet["offline_assertions"]}, set(handlers))
        self.assertEqual(sum(len(row["variants"]) for row in packet["offline_assertions"]), 82)
        ctl.verify_container(self.metadata, self.spec, False)
        ctl.image_admission(self.image, self.spec["image"], "arm64", True)
        for row in packet["offline_assertions"]:
            for variant in row["variants"]:
                with self.subTest(assertion=row["assertion_id"], variant=variant):
                    handlers[row["assertion_id"]](variant)

    def test_foreign_attachment_prevents_start_and_cleanup_never_detaches_it(self):
        class Shared(base.FakeDocker):
            def attachments(inner, volume):
                names = super().attachments(volume)
                return names + (["foreign-container"] if volume.endswith("-control") else [])
        backend = Shared()
        plan = base.test_plan()
        report = ctl.execute(plan, base.approval(plan), backend, revision_check=lambda _: None)
        self.assertEqual(report["verdict"], "FAIL")
        self.assertFalse(any(event.startswith("start:") for event in backend.events))
        self.assertTrue(any(not row["removed"] for row in report["cleanup"] if row["name"].endswith("-control")))
        self.assertNotIn("foreign-container", str(report))

    def test_extra_dns_options_wrong_environment_binding_and_process_substitution(self):
        for field in ("DnsSearch", "DnsOptions"):
            self.field("HostConfig", field, ["secret-canary"], "ISOLATION")
        self.field("Config", "Env", ["PATH=/different-but-otherwise-allowed"], "CONTAINER_ENV")
        for env in (["PATH=/bin", "PATH=/bin"], ["PATH="], ["PATH=/bin\nsecret-canary"], "secret-canary", None):
            self.reject(lambda: admission.environment_digest(env), "CONTAINER_ENV")
        expected = ["python3", "-B", admission.ENTRY, "measure", "30000"]
        for bad in (["sh"], ["python3", "-c", "secret-canary"]):
            rows = [{"pid": 1, "argv": ["python3", "-B", admission.ENTRY, "hold"]}, {"pid": 17, "argv": bad}]
            self.reject(lambda: admission.validate_processes(rows, 17, expected), "PROCESS_INVENTORY")


if __name__ == "__main__":
    unittest.main()
