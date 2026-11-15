"""Pure-fake call-shape and preserved-evidence controls; never launches a process."""
import hashlib
import json
from pathlib import Path
import sys
import traceback
from unittest.mock import patch

sys.dont_write_bytecode = True
HERE = Path(__file__).resolve().parent
ROOT = HERE.parent
REPO = HERE.parents[2]
sys.path.insert(0, str(HERE))
import security_probes as probes
from security_probes import c, db, owned, Deadline, WireBudget, corrected_guard
from test_owned_resources import FakeDocker, fixture_scope, DAEMON


def file_record(path):
    raw = path.read_bytes()
    return {"bytes": len(raw), "sha256": hashlib.sha256(raw).hexdigest()}


def call_shapes():
    all_calls, factory_errors = [], []
    for arch in ("x86_64", "aarch64"):
        value = fixture_scope(architecture=arch)
        fake = FakeDocker(value)
        registry = owned.Registry(value, lambda *_: None)
        def runner(argv, deadline):
            assert argv[:3] == db.PREFIX
            args = argv[3:]
            all_calls.append({"architecture": arch, "argv": argv})
            if args == ["info", "--format", "{{.ID}}"]:
                return 0, DAEMON, b""
            if args == ["version", "--format", "{{json .Server}}"]:
                return 0, c.canonical({"Os": "linux", "Arch": c.platform(arch)["image_architecture"]}), b""
            corrected_guard(argv)
            kind, action = args[:2]
            if action == "inspect":
                observed = fake.inspect(kind, args[2], deadline)
                return (0, c.canonical([observed]), b"") if observed is not None else (1, b"[]", f"Error: No such {kind}: {args[2]}".encode())
            if (kind, action) == ("container", "ls"):
                selected = args[args.index("--filter") + 1].removeprefix("volume=")
                return 0, "\n".join(fake.attachments(selected, deadline)).encode(), b""
            if action == "create":
                name = args[-1] if kind == "volume" else args[args.index("--name") + 1]
                role = next(r for r in (*owned.ACTORS, *owned.VOLUMES) if owned.name(value, r) == name)
                if kind == "volume":
                    fake.create_volume(registry.spec(role), deadline)
                    return 0, name.encode(), b""
                return 0, fake.create_container(registry.spec(role), deadline).encode(), b""
            ref = next(row["reference"] for row in registry.entries.values()
                       if row["reference"] is not None and row["reference"].get("id", row["reference"]["name"]) == args[-1])
            if action == "kill":
                fake.kill(ref, deadline)
            elif action == "wait":
                fake.wait(ref, deadline)
                return 0, b"137", b""
            elif action == "rm":
                fake.remove(ref, deadline)
            else:
                raise AssertionError((kind, action))
            return 0, b"", b""

        def failed_factory(argv, *args, **kwargs):
            corrected_guard(argv, actor=True)
            all_calls.append({"architecture": arch, "argv": argv})
            raise OSError(5, probes.CANARY)

        backend = db.DockerBackend(value, {r: "/never-read/" + r for r in owned.ACTORS},
            authorize=lambda *_: True, runner=runner, stream_factory=failed_factory)
        with patch.object(db.subprocess, "Popen", side_effect=AssertionError("process not permitted")), \
                patch.object(db, "read_engine_metadata", side_effect=AssertionError("socket not permitted")), \
                patch.object(backend, "_profile_file", return_value="/synthetic/approved-profile.json"):
            prepared = db.prepare_resources(registry, backend, Deadline(3))
            assert prepared["execution_authorized"] is False
            ref = registry.entries["target"]["reference"]
            try:
                backend.open_actor(ref, registry.spec("target"), registry.snapshot(), WireBudget(), Deadline(3))
            except c.Invalid as exc:
                rendered = "".join(traceback.format_exception(exc))
                assert str(exc) == "DOCKER_TRANSPORT" and probes.CANARY not in rendered
                factory_errors.append({"architecture": arch, "error": str(exc), "canary_leaked": False})
            else:
                raise AssertionError("factory error accepted")
            assert not backend._actor_streams
            for role in (*owned.ACTORS, *owned.VOLUMES):
                ref = registry.entries[role]["reference"]
                if role in owned.ACTORS:
                    backend.kill(ref, Deadline(1))
                    backend.wait(ref, Deadline(1))
                backend.remove(ref, Deadline(1))
            assert not fake.volumes and not fake.containers
    return {"status": "PASS", "calls": all_calls, "factory_error_redaction": factory_errors,
            "native_execution": False, "docker_execution": False,
            "method": "pure FakeDocker metadata; simulated authorization, no resource mutation"}


def preservation():
    before = json.loads((ROOT / "before.json").read_bytes())
    current = json.loads((ROOT / "source-inventory-corrected.json").read_bytes())
    prior_path = REPO / "out/c0-main-sync-2026-10-24/tests-022.json"
    prior = json.loads(prior_path.read_bytes())
    assert len(prior["inventory"]) == 318
    retained = {}
    for mode in ("022", "077"):
        path = ROOT / ("tests-" + mode + ".json")
        result = json.loads(path.read_bytes())
        old_ids, new_ids = set(prior["inventory"]), set(result["inventory"])
        assert old_ids <= new_ids and len(new_ids - old_ids) == 17
        assert result["status"] == "PASS" and result["tests_run"] == 335
        assert result["inventory"] == result["started"] == result["passed"]
        assert all(not result[key] for key in ("errors", "failures", "skipped", "unexpected_successes", "expected_failures"))
        assert all(current["files"][p]["sha256"] == digest for p, digest in result["source_files"].items())
        retained[mode] = {"report": file_record(path), "retained": len(old_ids), "new": sorted(new_ids - old_ids),
                          "reported_passed": result["tests_run"], "source_hashes_matched": len(result["source_files"])}
    copies = {p: file_record(ROOT / "before" / p) for p in before["files"]}
    assert all(row["sha256"] == before["files"][p]["sha256"] for p, row in copies.items())
    historical = {p: file_record(REPO / p) for p in before["files"] if p.startswith("docs/evidence/")}
    assert all(row["sha256"] == before["files"][p]["sha256"] for p, row in historical.items())
    native = {p: digest for p, digest in current["unchanged_native_baseline_proof"].items()}
    assert len(native) == 7
    assert all(digest == prior["source_files"][p] == current["files"][p]["sha256"] for p, digest in native.items())
    return {"status": "PASS", "prior_report_path": str(prior_path.relative_to(REPO)), "prior_report": file_record(prior_path),
            "supplied_test_reports": retained, "before_snapshot_copies_matched": len(copies),
            "current_historical_evidence_files_matched": historical,
            "seven_native_files_match_prior_test_and_corrected_inventory": native,
            "git_commands_executed": False, "full_test_suite_rerun": False}


def main():
    result = {"call_shapes": call_shapes(), "preservation": preservation()}
    with (HERE / "supplementary-probes.json").open("x") as handle:
        json.dump(result, handle, indent=2, sort_keys=True)
        handle.write("\n")
    print(json.dumps({"call_shapes": result["call_shapes"]["status"], "simulated_calls": len(result["call_shapes"]["calls"]),
        "factory_failures": result["call_shapes"]["factory_error_redaction"], "preservation": result["preservation"]["status"],
        "preserved_before_copies": result["preservation"]["before_snapshot_copies_matched"],
        "preserved_current_evidence_files": len(result["preservation"]["current_historical_evidence_files_matched"])}, indent=2))


if __name__ == "__main__":
    main()
