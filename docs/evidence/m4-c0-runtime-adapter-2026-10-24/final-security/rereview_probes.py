"""SEC-01 replay plus independent negative controls, against the actual final guard.

No historical module is imported/executed wholesale: only the original regression
method, its config context manager, and the original compatibility function are
extracted unchanged via AST. This prevents writes into the old review folder.
Only the old compatibility name `corrected_guard` is rebound to db.cli_arguments.
No Docker, network connection, native actor, or even inert subprocess is needed.
"""
import ast
from contextlib import contextmanager
import copy
import hashlib
import io
import itertools
import json
import os
from pathlib import Path
import sys
import tempfile
import traceback
from types import SimpleNamespace
import unittest
from unittest.mock import patch

sys.dont_write_bytecode = True
HERE = Path(__file__).resolve().parent
ROOT = HERE.parent
INITIAL = ROOT.parent
SOURCE = ROOT / "source/tests/crawl-jobs-v2-observer"
OLD_REVIEW = INITIAL / "security-review"
MODE = sys.argv[1]
assert MODE in ("022", "077")
mask = os.umask(0)
os.umask(mask)
assert mask == int(MODE, 8)
SCRATCH = HERE / ("scratch-" + MODE)
SCRATCH.mkdir(exist_ok=True)
sys.path.insert(0, str(SOURCE))
import contracts as c
import docker_backend as db
import owned_resources as owned
from streams import Deadline, WireBudget
from test_owned_resources import DAEMON, FakeDocker, fixture_scope
import test_live_adapter

REAL_GUARD = db.cli_arguments
REAL_TEMP = tempfile.TemporaryDirectory
assert Path(db.__file__).resolve() == SOURCE / "docker_backend.py"
DETAILS = {}
AUDIT = {"forbidden_process_attempts": 0, "forbidden_connection_attempts": 0, "outside_write_attempts": 0}
CANARY = "SECURITY-REVIEW-SYNTHETIC-PRIVATE-CANARY"


def within_review(path, dir_fd=None):
    if isinstance(path, int):
        return  # Existing file-descriptor operations, including tool stdout.
    path = Path(os.fsdecode(path))
    if not path.is_absolute() and isinstance(dir_fd, int) and dir_fd >= 0:
        path = Path(os.readlink("/proc/self/fd/" + str(dir_fd))) / path
    if not path.resolve().is_relative_to(HERE):
        AUDIT["outside_write_attempts"] += 1
        raise AssertionError("probe write escaped permitted review directory")


def audit(event, args):
    if event in ("subprocess.Popen", "os.system", "os.exec", "os.fork", "os.forkpty", "os.posix_spawn"):
        AUDIT["forbidden_process_attempts"] += 1
        raise AssertionError("no child processes permitted in this re-review")
    if event in ("socket.connect", "socket.bind", "socket.getaddrinfo"):
        AUDIT["forbidden_connection_attempts"] += 1
        raise AssertionError("no socket operation permitted in this re-review")
    if event == "open" and isinstance(args[2], int) and args[2] & (os.O_WRONLY | os.O_RDWR | os.O_CREAT | os.O_TRUNC | os.O_APPEND):
        within_review(args[0])
    elif event in ("os.mkdir", "os.remove", "os.rmdir"):
        within_review(args[0], args[2] if event == "os.mkdir" else args[1])
    elif event == "os.rename":
        within_review(args[0], args[2])
        within_review(args[1], args[3])


sys.addaudithook(audit)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def extract(path, name, namespace, *, class_name=None, expected=None):
    raw = path.read_bytes()
    if expected is not None:
        assert sha(raw) == expected
    nodes = ast.parse(raw, filename=str(path)).body
    if class_name:
        nodes = next(n for n in nodes if isinstance(n, ast.ClassDef) and n.name == class_name).body
    node = next(n for n in nodes if isinstance(n, ast.FunctionDef) and n.name == name)
    compiled = compile(ast.Module(body=[node], type_ignores=[]), str(path), "exec")
    exec(compiled, namespace)
    return namespace[name], {"path": str(path), "sha256": sha(raw), "function": name,
                             "function_ast_sha256": sha(ast.dump(node).encode())}


REGRESSION_NAME = "test_02_regression_clustered_help_must_be_rejected"
OLD_PROBE_SHA = "7fd4d84ccb3a16efb1619452e2bae98d175f31dfcf477ab59b7cb21296c253a9"
OLD_COMPAT_SHA = "859ddd680469130369b61d696f50200e043e5b67465dccfe5e0a0b40fcb0b858"


def original_regression_namespace(label):
    ns = {"__name__": label, "db": db, "c": c, "Deadline": Deadline, "patch": patch,
          "DETAILS": {}, "Path": Path, "contextmanager": contextmanager, "SCRATCH": SCRATCH, "REAL_TEMP": REAL_TEMP}
    extract(OLD_REVIEW / "security_probes.py", "configs", ns, expected=OLD_PROBE_SHA)
    method, identity = extract(OLD_REVIEW / "security_probes.py", REGRESSION_NAME, ns,
                               class_name="SecurityChecks", expected=OLD_PROBE_SHA)
    test_class = type(label, (unittest.TestCase,), {"__module__": __name__, REGRESSION_NAME: method})
    return ns, test_class, identity


REPLAY_NS, OriginalSEC01Replay, REPLAY_IDENTITY = original_regression_namespace("OriginalSEC01Replay")
OPS = (("container", "inspect"), ("container", "create"), ("container", "ls"),
       ("container", "kill"), ("container", "wait"), ("container", "rm"),
       ("volume", "inspect"), ("volume", "create"), ("volume", "rm"), ("image", "inspect"))
ORACLE = ["container", "create", "--entrypoint", "python3", "sha256:" + "a" * 64,
          "-I", "-B", "/oracle/worker.py"]


@contextmanager
def no_dispatch():
    with patch.object(db, "verify_cli", side_effect=AssertionError("binary admission touched")) as binary, \
            patch.object(db.tempfile, "TemporaryDirectory", side_effect=AssertionError("configuration allocated")) as config, \
            patch.object(db.subprocess, "Popen", side_effect=AssertionError("process spawned")) as spawn, \
            patch.object(db, "bounded_command", side_effect=AssertionError("transport reached")) as transport:
        yield
        for item in (binary, config, spawn, transport):
            item.assert_not_called()


class FocusedControls(unittest.TestCase):
    def reject(self, tail):
        with self.assertRaises(c.Invalid):
            db.command([*db.PREFIX, *tail], Deadline(1))

    def test_01_regression_detects_original_guard_negative_control(self):
        ns, negative_class, identity = original_regression_namespace("OriginalGuardNegativeControl")
        old_ns = {"__name__": "old_guard_control", "require": c.require, "PREFIX": db.PREFIX, "hex_id": owned.hex_id}
        old_guard, guard_identity = extract(INITIAL / "source/tests/crawl-jobs-v2-observer/docker_backend.py",
            "cli_arguments", old_ns, expected="8ceef501710ab69609d349d072d377a83fea0cf5f2377f05076114ef7fc0c130")
        with (HERE / ("negative-control-" + MODE + ".log")).open("x") as log, patch.object(db, "cli_arguments", old_guard):
            result = unittest.TextTestRunner(stream=log, verbosity=2).run(negative_class(REGRESSION_NAME))
        self.assertEqual((result.testsRun, len(result.failures), len(result.errors)), (1, 1, 0))
        self.assertTrue(all(row["reached_transport"] and not row["rejected"] for row in ns["DETAILS"]["clustered_help"]))
        self.assertIs(db.cli_arguments, REAL_GUARD)
        self.assertEqual(identity["function_ast_sha256"], REPLAY_IDENTITY["function_ast_sha256"])
        DETAILS["negative_control"] = {"status": "EXPECTED_FAILURE_DETECTED", "tests": 1, "failures": 1, "errors": 0,
            "guard_identity": guard_identity, "observations": ns["DETAILS"]["clustered_help"], "source_edited": False}

    def test_02_all_short_clusters_reject_for_every_allowed_operation(self):
        alphabet = "hHDcslIBv"
        tokens = ["-" + "".join(chars) for n in (1, 2, 3) for chars in itertools.product(alphabet, repeat=n)]
        with no_dispatch():
            for operation in OPS:
                for token in tokens:
                    self.reject([*operation, token])
        DETAILS["short_cluster_matrix"] = {"alphabet": alphabet, "lengths": [1, 2, 3], "tokens": len(tokens),
            "operations": len(OPS), "rejected_calls": len(tokens) * len(OPS), "token_list_sha256": sha(c.canonical(tokens))}

    def test_03_attached_control_and_value_positions_reject(self):
        tokens = ["-", "-hh=true", "-hh=false", "-hfalse", "-Dh=true", "-Hunix:///unused.sock",
                  "-Htcp://127.0.0.1:1", "-H=unix:///unused.sock", "-cunused", "-c=unused",
                  "-Ih", "-Bh", "-IB", "-I=1", "-\th", "-\nh", "- h"]
        with no_dispatch():
            for token in tokens:
                self.reject(["container", "inspect", token, "a" * 64])
                self.reject(["container", "inspect", "a" * 64, token])
                self.reject(["container", "inspect", "--format", token, "a" * 64])
        DETAILS["attached_and_value_positions"] = {"tokens": tokens, "positions": 3, "rejected_calls": len(tokens) * 3}

    def test_04_oracle_exception_requires_each_exact_sentinel(self):
        bad = []
        for position, values in (
            (2, ["--entrypoint=python3", "--entry-point", "entrypoint", "--", ""]),
            (3, ["python", "python3.14", "/usr/bin/python3", "python3 ", "python3\n", ""]),
            (4, ["image:latest", "sha256:" + "0" * 64, "sha256:" + "a" * 63, "sha256:" + "a" * 65,
                 "sha256:" + "A" * 64, "SHA256:" + "a" * 64, "sha256:" + "a" * 64 + "\n", "", None]),
            (5, ["-IB", "-B", "-I=1", "--isolated", "-Ih", ""]),
            (6, ["-BI", "-I", "-B=1", "--dont-write-bytecode", "-Bh", ""]),
            (7, ["/other", "/oracle/worker.py/", "/oracle/./worker.py", "worker.py", "--help", ""])):
            for value in values:
                row = list(ORACLE)
                row[position] = value
                bad.append(row)
        # Missing fixed operands/flags and extra arguments cannot widen the tail.
        bad.extend(ORACLE[:i] + ORACLE[i + 1:] for i in (2, 3, 4, 7))
        bad.extend([ORACLE + ["extra"], ORACLE[:-1], ORACLE[:-3] + ["-B", "-I", ORACLE[-1]],
                    ORACLE[:-3] + ["-IB", ORACLE[-1]], ORACLE + ORACLE[-6:]])
        with no_dispatch():
            for tail in bad:
                self.reject(tail)
        DETAILS["malformed_oracle"] = {"rejected_calls": len(bad), "case_list_sha256": sha(c.canonical(bad))}

    def test_05_only_final_tail_is_exempt_and_actor_never_is(self):
        count = 0
        with no_dispatch():
            for token in ("-hh", "-I", "-B", "-IB"):
                for index in range(len(ORACLE) + 1):
                    self.reject(ORACLE[:index] + [token] + ORACLE[index:])
                    count += 1
            for operation in OPS:
                if operation != ("container", "create"):
                    self.reject([*operation, *ORACLE[2:]])
                    count += 1
            self.reject(["container", "create", "--", "-hh", *ORACLE[2:]])
            count += 1
            for tail in (ORACLE, ["container", "start", "--attach", "--interactive", "a" * 64, *ORACLE[2:]],
                         ["container", "start", "--attach", "--interactive", "-hh"]):
                with self.assertRaises(c.Invalid):
                    db.DockerProcessStream([*db.PREFIX, *tail], {}, WireBudget(), Deadline(1))
                count += 1
        DETAILS["tail_scope"] = {"rejected_calls": count, "earlier_short_tokens_scanned": True, "actor_exception": False}

    def test_06_original_long_prefix_operation_and_bounds_gates_still_reject(self):
        tokens = ["--help", "--help=true", "--help=false", "help", "help=topic", "__complete", "__completeNoDesc",
                  "__complete=x", "--config=/unused", "--context=unused", "--host=unix:///unused.sock", "-c", "-H"]
        raw_cases = [["docker", *db.PREFIX[1:], "container", "inspect", "a" * 64],
                     [db.DOCKER, "--host", "unix:///other.sock", "container", "inspect", "a" * 64],
                     tuple([*db.PREFIX, "container", "inspect", "a" * 64]),
                     [*db.PREFIX, "container", "inspect", "\0"],
                     [*db.PREFIX, "container", "inspect", "a" * 4097],
                     [*db.PREFIX, "container", "inspect", *(["a" * 4096] * 9)],
                     [*db.PREFIX, "container", "inspect", *(["a"] * 256)]]
        wrong_ops = [["info"], ["version"], ["buildx", "version"], ["plugin", "ls"], ["container", "exec", "a" * 64]]
        with no_dispatch():
            for token in tokens:
                self.reject(["container", "inspect", "a" * 64, token])
            for argv in raw_cases:
                with self.assertRaises(c.Invalid):
                    db.command(argv, Deadline(1))
            for tail in wrong_ops:
                self.reject(tail)
        DETAILS["unchanged_gates"] = {"rejected_calls": len(tokens) + len(raw_cases) + len(wrong_ops)}

    def test_07_valid_oracle_tail_reaches_only_stubbed_transport(self):
        dispatched, names = [], []
        for image in ("sha256:" + "a" * 64, "sha256:" + "0" * 63 + "1", "sha256:" + "0123456789abcdef" * 4):
            tail = [*ORACLE[:4], image, *ORACLE[5:]]
            with patch.object(db, "verify_cli"), REPLAY_NS["configs"]() as (configs, _), \
                    patch.object(db, "bounded_command", side_effect=lambda a, d, e: (dispatched.append(a) or (0, b"OK", b""))):
                self.assertEqual(db.command([*db.PREFIX, *tail], Deadline(1)), (0, b"OK", b""))
                names.extend(configs)
        self.assertEqual(len(dispatched), 3)
        self.assertTrue(all(not Path(name).exists() for name in names))
        REAL_GUARD([*db.PREFIX, "container", "start", "--attach", "--interactive", "b" * 64], actor=True)
        DETAILS["positive_transport_controls"] = {"valid_oracle_images": 3, "actor_shape_valid": True,
            "actual_binary_or_child_launched": False, "configs_removed": True}

    def test_08_original_398_call_compatibility_uses_actual_final_guard(self):
        namespace = {"__name__": "actual_guard_compatibility", "c": c, "db": db, "owned": owned,
            "Deadline": Deadline, "WireBudget": WireBudget, "patch": patch, "traceback": traceback,
            "FakeDocker": FakeDocker, "fixture_scope": fixture_scope, "DAEMON": DAEMON,
            "probes": SimpleNamespace(CANARY=CANARY), "corrected_guard": REAL_GUARD}
        replay, identity = extract(OLD_REVIEW / "supplementary_probes.py", "call_shapes", namespace, expected=OLD_COMPAT_SHA)
        self.assertIs(namespace["corrected_guard"], db.cli_arguments)
        value = replay()
        prior = json.loads((OLD_REVIEW / "supplementary-probes.json").read_bytes())["call_shapes"]
        self.assertEqual(value["calls"], prior["calls"])
        self.assertEqual(len(value["calls"]), 398)
        self.assertEqual(value["factory_error_redaction"], prior["factory_error_redaction"])
        self.assertIs(db.cli_arguments, REAL_GUARD)
        actual_guard_calls = sum(row["argv"][3] not in ("info", "version") for row in value["calls"])
        DETAILS["compatibility"] = {"status": value["status"], "function_identity": identity, "calls": 398,
            "actual_cli_arguments_calls": actual_guard_calls, "metadata_fixture_calls": 398 - actual_guard_calls,
            "call_list_sha256": sha(c.canonical(value["calls"])), "exact_original_call_sequence": True,
            "guard_binding": "original compatibility name corrected_guard is identical to actual db.cli_arguments",
            "guard_source": str(Path(REAL_GUARD.__code__.co_filename).resolve()),
            "proposed_replacement_guard_used": False, "factory_errors": value["factory_error_redaction"]}
        with (HERE / ("compatibility-calls-" + MODE + ".json")).open("x") as handle:
            json.dump(value, handle, indent=2, sort_keys=True)
            handle.write("\n")

    def test_09_no_lifecycle_or_other_backend_body_changed(self):
        old = INITIAL / "source/tests/crawl-jobs-v2-observer"
        for name in ("local_process.py", "streams.py"):
            self.assertEqual((old / name).read_bytes(), (SOURCE / name).read_bytes())
        def without_guard(path):
            module = ast.parse(path.read_bytes())
            module.body = [node for node in module.body if not (isinstance(node, ast.FunctionDef) and node.name == "cli_arguments")]
            return ast.dump(module)
        self.assertEqual(without_guard(old / "docker_backend.py"), without_guard(SOURCE / "docker_backend.py"))
        DETAILS["unchanged_implementation"] = {"lifecycle_files_byte_identical": True,
            "backend_outside_cli_arguments_ast_identical": True}


def routed_temp(*args, **kwargs):
    # Product tests' temporary profile fixtures also stay in the new review only.
    kwargs["dir"] = SCRATCH
    return REAL_TEMP(*args, **kwargs)


def main():
    suite = unittest.TestSuite([OriginalSEC01Replay(REGRESSION_NAME)])
    suite.addTests(unittest.defaultTestLoader.loadTestsFromTestCase(FocusedControls))
    product_methods = ["test_clustered_help_and_attached_short_overrides_refuse_before_side_effects",
                       "test_short_option_exception_requires_exact_oracle_entrypoint_image_and_tail",
                       "test_generated_oracle_create_remains_accepted_for_both_architectures"]
    suite.addTests(test_live_adapter.ConfigurationTests(name) for name in product_methods)
    with (HERE / ("probes-" + MODE + ".log")).open("x") as log, \
            patch.object(db.tempfile, "TemporaryDirectory", side_effect=routed_temp):
        result = unittest.TextTestRunner(stream=log, verbosity=2).run(suite)
    assert db.cli_arguments is REAL_GUARD
    report = {"kind": "SEC-01-independent-security-rereview-probes", "mode": MODE, "effective_umask": oct(mask),
              "tests_run": result.testsRun, "passed": result.testsRun - len(result.failures) - len(result.errors),
              "failures": [{"test": t.id(), "traceback": text} for t, text in result.failures],
              "errors": [{"test": t.id(), "traceback": text} for t, text in result.errors],
              "original_regression": {"identity": REPLAY_IDENTITY, "observations": REPLAY_NS["DETAILS"].get("clustered_help")},
              "details": DETAILS, "audit": AUDIT, "python": sys.version, "snapshot_source": str(SOURCE),
              "guard_sha256": sha((SOURCE / "docker_backend.py").read_bytes()),
              "scratch_empty": not list(SCRATCH.iterdir()), "full_338_tests_rerun": False,
              "docker_or_ssh_or_native_or_any_child_execution": False,
              "historical_module_top_level_executed": False,
              "resource_warning_filter": "error::ResourceWarning"}
    with (HERE / ("probes-" + MODE + ".json")).open("x") as handle:
        json.dump(report, handle, indent=2, sort_keys=True)
        handle.write("\n")
    print((HERE / ("probes-" + MODE + ".log")).read_text())
    print(json.dumps({k: v for k, v in report.items() if k not in ("details", "original_regression")}, indent=2))
    sys.exit(0 if result.wasSuccessful() and report["scratch_empty"] and not any(AUDIT.values()) else 1)


if __name__ == "__main__":
    main()
