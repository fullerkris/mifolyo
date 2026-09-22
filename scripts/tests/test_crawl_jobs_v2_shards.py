"""Coverage guards for exhaustive CI race sharding; never launches Go tests."""
import copy
import contextlib
import io
import json
from pathlib import Path
import runpy
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

driver = runpy.run_path(str(Path(__file__).resolve().parents[1] / "test-crawl-jobs-v2-shard.py"))


class RaceShardTests(unittest.TestCase):
    def setUp(self):
        self.names = sorted([f"TestCase{i}" for i in range(100)] + ["ExampleWire", "FuzzFrame"])
        self.reports = [driver["report_for"](self.names, i) for i in range(driver["COUNT"])]
        for row in self.reports:
            row["status"] = "PASS"
            row["passed"] = list(row["selected"])

    def test_partition_is_complete_disjoint_and_order_independent(self):
        groups = driver["partition"](self.names)
        self.assertEqual(groups, driver["partition"](list(reversed(self.names))))
        self.assertEqual(sorted(name for group in groups for name in group), self.names)
        self.assertEqual(driver["verify_reports"](self.reports), len(self.names))

    def test_omitted_duplicate_failed_wrong_revision_or_changed_inventory_rejects(self):
        cases = [self.reports[:-1], [*self.reports[:-1], self.reports[0]]]
        for key, value in (("status", "FAIL"), ("status", "NOT_RUN"), ("race", False),
                           ("commit", "other"), ("inventory", self.names[:-1]), ("selected", []), ("passed", [])):
            changed = copy.deepcopy(self.reports)
            changed[3][key] = value
            cases.append(changed)
        for reports in cases:
            with self.assertRaises(ValueError):
                driver["verify_reports"](reports)

    def test_malformed_or_duplicate_test_inventory_rejects(self):
        for names in ([], ["TestOne", "TestOne"], ["TestName/child"], ["TestName|TestOther"]):
            with self.assertRaises(ValueError):
                driver["partition"](names)

    def test_actual_json_collector_rejects_nested_and_unknown_skips(self):
        for nested in (False, True):
            with self.subTest(nested=nested), tempfile.TemporaryDirectory() as directory:
                selected = self.reports[0]["selected"]
                skipped = selected[0] + ("/dependency_unavailable" if nested else "")
                events = [{"Test": skipped, "Action": "skip"}] + [
                    {"Test": name, "Action": "pass"} for name in selected if name != skipped]
                def fake_go(*_, **kwargs):
                    for event in events:
                        kwargs["stdout"].write(json.dumps(event) + "\n")
                    return SimpleNamespace(returncode=0)
                scope = driver["main"].__globals__
                with patch.dict(scope, inventory=lambda: self.names), \
                     patch.object(scope["subprocess"], "run", side_effect=fake_go), \
                     patch.object(sys, "argv", ["sharder", "run", "--shard", "0", "--report-dir", directory]), \
                     contextlib.redirect_stdout(io.StringIO()):
                    self.assertEqual(driver["main"](), 1)
                report = json.loads((Path(directory) / "shard-0.json").read_text())
                self.assertEqual(report["skipped"], [skipped])
                with self.assertRaises(ValueError):
                    driver["verify_reports"]([report, *self.reports[1:]])

    def test_only_existing_optional_root_skip_is_allowed(self):
        names = sorted([*self.names, "TestJobLuaNativeFactoryParity"])
        reports = [driver["report_for"](names, i) for i in range(driver["COUNT"])]
        for report in reports:
            report["status"] = "PASS"
            report["passed"] = [name for name in report["selected"] if name != "TestJobLuaNativeFactoryParity"]
            report["skipped"] = [name for name in report["selected"] if name == "TestJobLuaNativeFactoryParity"]
        self.assertEqual(driver["verify_reports"](reports), len(names))

    def test_remaining_package_command_uses_exact_argument_list_without_shell(self):
        prefix = "github.com/IonelPopJara/search-engine/services/spider/"
        packages = [prefix + "cmd/spider", driver["FULL_PACKAGE"], prefix + "internal/database",
                    prefix + "internal/database/crawljobsv2/tools/generate-unicode"]
        scope = driver["other_packages"].__globals__
        with patch.object(scope["subprocess"], "run", side_effect=[
            SimpleNamespace(stdout="\n".join(packages)), SimpleNamespace(returncode=0)
        ]) as called:
            self.assertEqual(driver["other_packages"](), 0)
        command = called.call_args_list[1].args[0]
        self.assertNotIn(driver["FULL_PACKAGE"], command)
        self.assertEqual(command[7:], [p for p in packages if p != driver["FULL_PACKAGE"]])
        self.assertIn("-race", command)

    def test_changed_package_inventory_cannot_silently_omit_v2(self):
        scope = driver["other_packages"].__globals__
        for value in ("", "other/module", driver["FULL_PACKAGE"] + "\n" + driver["FULL_PACKAGE"]):
            with patch.object(scope["subprocess"], "run", return_value=SimpleNamespace(stdout=value)) as called:
                with self.assertRaises(ValueError):
                    driver["other_packages"]()
                self.assertEqual(called.call_count, 1)


if __name__ == "__main__":
    unittest.main()
