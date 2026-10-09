"""Exercise the C0 CI gate with tiny benign suites, never native/Docker actors."""
import hashlib
import io
import json
import os
from pathlib import Path
import runpy
import subprocess
import sys
import tempfile
import textwrap
import unittest

RUNNER = Path(__file__).resolve().parents[1] / "test-crawl-jobs-v2-observer.py"
driver = runpy.run_path(str(RUNNER))


class ObserverRunnerTests(unittest.TestCase):
    def run_fixture(self, source):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            script = root / "scripts" / RUNNER.name
            script.parent.mkdir()
            script.write_bytes(RUNNER.read_bytes())
            package = root / "tests/crawl-jobs-v2-observer"
            package.mkdir(parents=True)
            if source is not None:
                (package / "test_probe.py").write_text(textwrap.dedent(source))
            report = root / "report.json"
            process = subprocess.run([sys.executable, "-B", "-W", "error::ResourceWarning", str(script),
                                      "--report", str(report)], cwd=root.parent,
                                     env=dict(os.environ, PYTHONDONTWRITEBYTECODE="1", GITHUB_SHA="test-revision"),
                                     capture_output=True, text=True, timeout=30)
            result = json.loads(report.read_text())
            self.assertEqual(result["commit"], "test-revision")
            self.assertEqual(result["runner_sha256"], hashlib.sha256(RUNNER.read_bytes()).hexdigest())
            self.assertNotIn("ResourceWarning", process.stderr)
            return process, result

    def test_success_uses_script_relative_discovery_and_binds_sources(self):
        source = "import unittest\nclass Probe(unittest.TestCase):\n    def test_ok(self): pass\n"
        process, report = self.run_fixture(source)
        self.assertEqual(process.returncode, 0, process.stderr)
        self.assertEqual(report["status"], "PASS")
        self.assertEqual(report["inventory"], ["test_probe.Probe.test_ok"])
        self.assertEqual(report["passed"], report["inventory"])
        self.assertEqual(report["source_files"], {
            "tests/crawl-jobs-v2-observer/test_probe.py": hashlib.sha256(source.encode()).hexdigest()})

    def test_empty_discovery_refuses(self):
        process, report = self.run_fixture(None)
        self.assertEqual(process.returncode, 1)
        self.assertEqual(report["status"], "FAIL")

    def test_import_failure_refuses(self):
        process, report = self.run_fixture("raise ImportError('fixture')\n")
        self.assertEqual(process.returncode, 1)
        self.assertTrue(report["errors"])

    def test_failures_and_errors_refuse(self):
        for body, field in (("self.fail('fixture')", "failures"), ("raise RuntimeError('fixture')", "errors")):
            with self.subTest(field=field):
                process, report = self.run_fixture(f"import unittest\nclass Probe(unittest.TestCase):\n    def test_bad(self): {body}\n")
                self.assertEqual(process.returncode, 1)
                self.assertTrue(report[field])

    def test_method_class_and_setup_skips_refuse(self):
        cases = ["@unittest.skip('fixture')\nclass Probe(unittest.TestCase):\n    def test_bad(self): pass\n",
                 "class Probe(unittest.TestCase):\n    @unittest.skip('fixture')\n    def test_bad(self): pass\n",
                 "class Probe(unittest.TestCase):\n    @classmethod\n    def setUpClass(cls): raise unittest.SkipTest('fixture')\n    def test_bad(self): pass\n"]
        for source in cases:
            with self.subTest(source=source):
                process, report = self.run_fixture("import unittest\n" + source)
                self.assertEqual(process.returncode, 1)
                self.assertTrue(report["skipped"])

    def test_expected_failure_and_unexpected_success_refuse(self):
        for body, field in (("self.fail('fixture')", "expected_failures"), ("pass", "unexpected_successes")):
            with self.subTest(field=field):
                process, report = self.run_fixture("import unittest\nclass Probe(unittest.TestCase):\n"
                    f"    @unittest.expectedFailure\n    def test_bad(self): {body}\n")
                self.assertEqual(process.returncode, 1)
                self.assertTrue(report[field])

    def test_skipped_and_failed_subtests_refuse(self):
        for body, field in (("self.skipTest('fixture')", "skipped"), ("self.fail('fixture')", "failures")):
            with self.subTest(field=field):
                process, report = self.run_fixture("import unittest\nclass Probe(unittest.TestCase):\n"
                    f"    def test_bad(self):\n        with self.subTest(case=1): {body}\n")
                self.assertEqual(process.returncode, 1)
                self.assertTrue(report[field])

    def test_incomplete_suite_refuses_even_if_executed_tests_pass(self):
        class PartialSuite(unittest.TestSuite):
            def run(self, result, debug=False):
                self._tests[0](result)
                return result
        suite = PartialSuite([unittest.FunctionTestCase(lambda: None, description="one"),
                              unittest.FunctionTestCase(lambda: None, description="two")])
        # Distinct IDs ensure this tests completion rather than duplicate refusal.
        suite._tests[0].id = lambda: "first"
        suite._tests[1].id = lambda: "second"
        report = driver["run_suite"](suite, stream=io.StringIO())
        self.assertEqual(report["tests_run"], 1)
        self.assertEqual(report["status"], "FAIL")

    def test_duplicate_inventory_refuses_before_execution(self):
        called = []
        test = unittest.FunctionTestCase(lambda: called.append(True))
        with self.assertRaises(ValueError):
            driver["run_suite"](unittest.TestSuite([test, test]), stream=io.StringIO())
        self.assertEqual(called, [])

    def test_package_mutation_during_tests_refuses(self):
        process, report = self.run_fixture("""
            import unittest
            from pathlib import Path
            class Probe(unittest.TestCase):
                def test_mutates(self):
                    Path(__file__).write_text('# changed\\n')
            """)
        self.assertEqual(process.returncode, 1)
        self.assertEqual(report["status"], "FAIL")
        self.assertIn("changed during", report["refusal"])


if __name__ == "__main__":
    unittest.main()
