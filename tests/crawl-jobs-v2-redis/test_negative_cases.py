"""Offline M4-P3 fixture tests and PUBLIC synthetic Go vectors. No I/O authority."""
import copy
import sys
import unittest

import harness as h
import negative_specs as ns
import negative_cases as nc
import claim_release as cr
import runtime_case as case
import resp
from test_harness import inputs
from test_claim_release import captured

AT = 3000000001
EPOCH = "7" * 32
OLD, NEW = "a" * 40, "b" * 40
EVIDENCE = h.digest(b"public offline rehearsal control; NOT actual measured evidence")


def context(case_id, at=AT):
    plan = h.compile_plan(inputs(ns.CASES[case_id]))
    values = captured(at)
    if case_id == ns.BOOT:
        values = {key: values[key] for key in ("fixture_id", "redis_time_ms")}
    fixture = nc.compile_fixture(plan, values)
    boot_wire = nc.boot_wire(NEW, EPOCH, EVIDENCE, at)
    boot = nc.boot_record(boot_wire, at)
    return plan, fixture, boot


class NegativeFixtureTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.cases = {name: context(name) for name in ns.CASES}

    def test_closed_inventory_profiles_and_nonexecuting_provenance(self):
        self.assertEqual(len(ns.CASES), 13)
        for name, (plan, fixture, _) in self.cases.items():
            with self.subTest(case=name):
                self.assertEqual(case.case_for_plan(plan), name)
                self.assertEqual(plan["profile"], "administrative" if name in ns.ADMIN else "ledger")
                self.assertEqual(nc.validate_fixture(plan, fixture), fixture)
                self.assertFalse(fixture["execution_authorized"])
                self.assertFalse(fixture["time_observation_verified"])
                self.assertEqual(len(fixture["key_inventory"]), 2 if name == ns.BOOT else 71 if name in ns.ADMIN else 58)
                self.assertNotIn(h.AUTH[0], fixture["initial_state"])
                self.assertEqual(set(fixture["initial_state"]), set(fixture["key_inventory"]) - {h.AUTH[0]})
                if name in ns.ADMIN or name == ns.BOOT:
                    self.assertTrue(all(row is None for row in fixture["initial_state"].values()))

    def test_each_state_delta_is_exactly_one_closed_difference(self):
        for name in ns.STORED:
            _, fixture, _ = self.cases[name]
            baseline = fixture["worker"]["initial_state"]
            differences = [key for key in baseline if h.canonical(baseline[key]) != h.canonical(fixture["initial_state"][key])]
            self.assertEqual(len(differences), 1)
            for key in (fixture["worker"]["base_key"], fixture["worker"]["job_key"]):
                self.assertEqual(baseline[key], fixture["initial_state"][key])

    def test_forged_inputs_state_source_and_cross_case_binding_reject(self):
        name = next(iter(ns.STORED))
        plan, fixture, _ = self.cases[name]
        for mutate in (lambda f: f["initial_state"].pop(f["worker"]["job_key"]),
                       lambda f: f["initial_state"].update({"unlisted": None}),
                       lambda f: f["key_inventory"].reverse(),
                       lambda f: f["initial_state"][h.AUTH[3]].update(expires_at_ms=1),
                       lambda f: f["initial_state"][h.AUTH[3]]["fields"].append(["extra", "secret-canary"]),
                       lambda f: f.update(case=ns.BOOT), lambda f: f.update(execution_authorized=True),
                       lambda f: f.update(admin_nonce="a" * 32), lambda f: f.update(compiler_sha256="b" * 64)):
            changed = copy.deepcopy(fixture)
            mutate(changed)
            with self.assertRaises(h.InvalidArtifact):
                nc.validate_fixture(plan, changed)
        for value in (True, 1.0, "1000", 0, h.MAX_EXACT):
            with self.assertRaises(h.InvalidArtifact):
                nc.compile_fixture(plan, dict(captured(), redis_time_ms=value))
        with self.assertRaises(h.InvalidArtifact):
            nc.compile_fixture(plan, dict(captured(), mutation="caller-script"))
        other = self.cases[list(ns.STORED)[1]][0]
        with self.assertRaises(h.InvalidArtifact):
            nc.validate_fixture(other, fixture)

    def test_public_summary_has_no_private_material(self):
        for name, (plan, fixture, _) in self.cases.items():
            raw = h.canonical(nc.public_summary(plan, fixture))
            private = [fixture["admin_nonce"], cr.URL, cr.ROBOTS]
            if fixture["worker"]:
                private += [captured()[key] for key in ("owner_a", "owner_b", "token_a", "token_b", "wrong_token")]
                private += [fixture["worker"]["identities"][key]["reservation_id"] for key in ("a", "b")]
            for value in private:
                self.assertNotIn(value.encode(), raw)

    def test_wire_mutations_are_individually_bounded_and_outer_admitted(self):
        from test_claim_execution import permits
        plan, fixture, _ = self.cases[ns.WIRE]
        original = cr.wire_requests(plan, fixture["worker"], EPOCH)[0]
        rows = nc.ledger_wires(plan, fixture, EPOCH)
        self.assertEqual(len(rows), 12)
        rules = case.acl_rules(ns.WIRE, fixture, plan)["ledger"]
        for index, (label, code, wire) in enumerate(rows):
            self.assertEqual(label, f"W{index + 1:02}")
            self.assertTrue(code.startswith("CRAWL_V2_"))
            self.assertLess(len(resp.encode(wire)), 2 * 1024 * 1024)
            self.assertTrue(permits(rules, "EVALSHA", *wire[3:60]))
            changed = sum(left != right for left, right in zip(original, wire)) + abs(len(original) - len(wire))
            self.assertEqual(changed, 2 if label == "W10" else 1)

    def test_boot_mutations_and_age_keep_original_protocol_constants(self):
        wire = nc.boot_wire(NEW, EPOCH, EVIDENCE, AT)
        rows = nc.boot_wires(wire, OLD, AT)
        self.assertEqual(len(rows), 9)
        self.assertEqual(rows[3][2][7], str(AT - 2592000001))
        self.assertEqual(rows[4][2][7], str(AT + 600000))
        self.assertEqual(rows[5][2][4], OLD)
        for value in (True, 2592000001, h.MAX_EXACT, "1000"):
            with self.assertRaises(h.InvalidArtifact):
                nc.boot_wires(wire, OLD, value)

    def test_admin_state_is_only_a_canonical_output_projection(self):
        for name in ns.ADMIN:
            plan, fixture, boot = self.cases[name]
            observed = nc.admin_observations(plan, fixture, boot, h.digest(b"public process control"), AT)
            rows = nc.admin_projection(plan, fixture, observed, EPOCH, [AT + 1, AT + 2, AT + 3])
            self.assertEqual([row["operation"] for row in rows], [ns.INSTALL, ns.RETIRE, ns.PROMOTE])
            self.assertEqual([int(row["wire"][2]) for row in rows], [8, 16, 29])
            self.assertEqual([len(row["wire"]) - 3 - int(row["wire"][2]) for row in rows], [31, 23, 20])
            self.assertEqual([sum(value is not None for value in row["state"].values()) for row in rows], [3, 4, 4])
            self.assertTrue(all(rows[-1]["state"][key] is None for key in h.ABSENCE_ONLY))
            changed = dict(observed, fixture_id="a" * 32)
            with self.assertRaises(h.InvalidArtifact):
                nc.admin_projection(plan, fixture, changed, EPOCH, [AT, AT, AT])


def wire_json(wire):
    return {"parts_hex": [(value.encode() if type(value) is str else value).hex() for value in wire], "resp_size": len(resp.encode(wire))}


def go_vectors(group):
    wanted = {"stored": tuple(ns.STORED), "wire": (ns.WIRE,), "boot": (ns.BOOT,), "admin": tuple(ns.ADMIN)}
    h.require(group in wanted, "VECTOR_GROUP")
    vectors = []
    for name in wanted[group]:
        plan, fixture, boot = context(name)
        initial = {**fixture["initial_state"], h.AUTH[0]: cr._hash(list(boot.items()))}
        vector = {"case": name, "fixture": fixture, "boot_record": boot,
                  "acl_rules": case.acl_rules(name, fixture, plan), "initial": initial, "steps": []}
        def add(op, actor, wire, now, *, code=None, reply=None, expected=None):
            vector["steps"].append({"operation": op, "actor": actor, "wire": wire_json(wire), "at_ms": now,
                                    "error": code or "", "reply": reply or [], "state": copy.deepcopy(expected or initial)})
        if name in ns.STORED or name == ns.WIRE:
            baseline_wires = cr.wire_requests(plan, fixture["worker"], EPOCH)
            vector["positive_claim_wire"] = wire_json(baseline_wires[0])
            for index, (_, code, wire) in enumerate(nc.ledger_wires(plan, fixture, EPOCH)):
                for repetition in range(2):
                    add(cr.CLAIM, "ledger", wire, AT + 1 + index * 2 + repetition, code=code)
            if name == ns.WIRE:
                projected = cr.expected_sequence(plan, fixture["worker"], [AT + 100] * 3 + [AT + 101] * 6)
                for index, op in ((0, cr.CLAIM), (3, cr.RELEASE)):
                    row = projected[index]
                    add(op, "ledger", baseline_wires[index], int(row["reply"][1]), reply=row["reply"],
                        expected={**row["state"], h.AUTH[0]: initial[h.AUTH[0]]})
        elif name == ns.BOOT:
            initial[h.AUTH[0]] = None
            wire = nc.boot_wire(NEW, EPOCH, EVIDENCE, AT)
            vector["positive_boot_wire"] = wire_json(wire)
            for index, (_, code, changed) in enumerate(nc.boot_wires(wire, OLD, AT)):
                for repetition in range(2):
                    add("CJ2_APPROVE_BOOT", "boot", changed, AT + 1 + index * 2 + repetition, code=code)
            approved = nc.boot_record(wire, AT + 100)
            post = {**initial, h.AUTH[0]: cr._hash(list(approved.items()))}
            add("CJ2_APPROVE_BOOT", "boot", wire, AT + 100, reply=["OK", str(AT + 100), EPOCH], expected=post)
            add("CJ2_APPROVE_BOOT", "boot", wire, AT + 101, reply=["EXISTS_IDENTICAL", str(AT + 101), EPOCH], expected=post)
        else:
            observed = nc.admin_observations(plan, fixture, boot, h.digest(b"public process control"), AT)
            target = (ns.INSTALL, ns.RETIRE, ns.PROMOTE).index(ns.ADMIN[name][1])
            rows = nc.admin_projection(plan, fixture, observed, EPOCH, [AT + 10, AT + 20, AT + 30])
            vector["admin_artifacts"] = {"core_hex": plan["guard_core"]["record_hex"],
                "marker_hex": plan["compatibility_marker"]["record_hex"], "observations": observed}
            for index in range(target):
                row = rows[index]
                add(row["operation"], "release_admin" if index == 0 else "migration_admin", row["wire"], int(row["reply"][1]),
                    reply=row["reply"], expected={**row["state"], h.AUTH[0]: initial[h.AUTH[0]]})
            row = rows[target]
            pre = vector["steps"][-1]["state"] if target else initial
            for repetition in range(2):
                add(row["operation"], "ledger", row["wire"], int(row["reply"][1]) - 2 + repetition,
                    code=ns.ADMIN[name][2], expected=pre)
            add(row["operation"], ns.ADMIN[name][3], row["wire"], int(row["reply"][1]), reply=row["reply"],
                expected={**row["state"], h.AUTH[0]: initial[h.AUTH[0]]})
        vectors.append(vector)
    raw = h.canonical({"purpose": "offline_public_test_vectors", "execution_authorized": False, "vectors": vectors})
    h.require(len(raw) <= h.MAX_ARTIFACT_BYTES, "VECTOR_BOUND")
    sys.stdout.buffer.write(raw)


if __name__ == "__main__":
    if len(sys.argv) == 3 and sys.argv[1] == "--go-vectors":
        go_vectors(sys.argv[2])
    else:
        unittest.main()
