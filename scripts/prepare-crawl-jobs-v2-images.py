#!/usr/bin/env python3
"""Validate already-built immutable images without starting Redis or a fixture.

Checks stopped-container admission, then runs only a Redis version command and
two networkless Python image checks. The metadata checks never start containers.
Writes bounded evidence outside project volumes; never creates an approval.
"""
from __future__ import annotations

import argparse
import json
from pathlib import Path
import secrets
import sys
import time

ROOT = Path(__file__).resolve().parents[1]
HERE = ROOT / "tests/crawl-jobs-v2-redis"
sys.path.insert(0, str(HERE))

import controller as ctl
import harness as h


def validate_prestart(backend, harness_image, redis_image):
    fixture = secrets.token_hex(16)
    prefix = "cj2-prestart-check-" + fixture
    volumes = {role: prefix + "-" + role for role in ("control", "data")}
    resources = []
    report = {"kind": "metadata_only_prestart", "status": "FAIL", "containers_started": 0, "roles": []}
    try:
        for volume in volumes.values():
            h.require(backend.inspect("volume", volume) is None, "CHECK_VOLUME_EXISTS")
            resources.append(("volume", volume))
            backend.volume(volume, fixture)
        for role in ("init", "executor", "redis", "revocation"):
            name = prefix + "-" + role
            h.require(backend.inspect("container", name) is None, "CHECK_CONTAINER_EXISTS")
            spec = ctl.container_spec(name, role, fixture, redis_image if role == "redis" else harness_image, volumes)
            resources.append(("container", name))
            backend.create(spec)
            observed = backend.inspect("container", name)
            ctl.verify_container(observed, spec, False)
            h.require(observed["State"].get("Status") == "created" and type(observed["State"].get("Pid")) is int
                      and observed["State"]["Pid"] == 0,
                      "CHECK_CONTAINER_STARTED")
            report["roles"].append({"role": role, "status": "PASS", "cap_add": observed["HostConfig"].get("CapAdd"),
                                    "inspection_sha256": h.digest(h.canonical(observed))})
        report["status"] = "PASS"
    except (Exception, KeyboardInterrupt) as error:
        report["failure_details"] = ctl.failure_details(error)
    finally:
        previous_deadline = backend.deadline
        with ctl.cleanup_signals():
            backend.deadline = time.monotonic() + 60
            report["cleanup"] = ctl.cleanup(backend, resources, fixture)
            backend.deadline = previous_deadline
        if not all(row["removed"] for row in report["cleanup"]):
            report["status"] = "FAIL"
    return report


def validate(backend, image, redis_image, role):
    token = secrets.token_hex(16)
    name = "cj2-image-check-" + token
    memory = 134217728 if role == "init" else 268435456
    uid = "0:0" if role == "init" else "65534:65534"
    args = ["container", "create", "--name", name, "--pull", "never", "--network", "none",
            "--read-only", "--user", uid, "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true",
            "--memory", str(memory), "--memory-swap", str(memory), "--cpus", "1", "--pids-limit", "32",
            "--restart", "no", "--label", "io.mifolyo.cj2.image-check=" + token]
    if role == "init":
        args += ["--cap-add", "CHOWN"]
    if role == "redis-version":
        args += ["--tmpfs", "/data:rw,noexec,nosuid,size=1048576", "--entrypoint", "redis-server", image, "--version"]
        payload = b""
    else:
        args += ["--interactive", "--entrypoint", "python3", image, "-I", "-B", "-", redis_image, image]
        payload = (HERE / "image_check.py").read_bytes()
    result = {"image": image, "role": role, "memory_limit_bytes": memory, "status": "FAIL"}
    try:
        h.require(backend.inspect("container", name) is None, "CHECK_CONTAINER_EXISTS")
        backend.call(*args)
        code, output, _ = ctl.command([*backend.prefix, "container", "start", "--attach", "--interactive", name],
                                      data=payload, timeout=120)
        observed = backend.inspect("container", name)
        state = observed["State"]
        result["exit_code"], result["oom_killed"] = state["ExitCode"], state["OOMKilled"]
        h.require(code == 0 and state["ExitCode"] == 0 and state["OOMKilled"] is False, "IMAGE_CHECK_FAILED")
        if role == "redis-version":
            text = output.decode().strip()
            h.require(text.startswith("Redis server v=7.4.11 "), "REDIS_VERSION")
            result["version_output"] = text
        else:
            checked = h.decode(output)
            h.require(checked["status"] == "PASS" and checked["execution_authorized"] is False, "CHECK_RESULT")
            h.require(set(checked["files"]) == set(ctl.scope_paths()), "IMAGE_INVENTORY")
            h.require(checked["identities"] == h.source_identity()[0], "IMAGE_IDENTITIES")
            for relative, digest in checked["files"].items():
                h.require((ROOT / relative).is_file() and h.digest((ROOT / relative).read_bytes()) == digest, "IMAGE_SOURCE_DRIFT")
            h.require(checked["cgroup_memory_peak_bytes"] is not None and
                      checked["cgroup_memory_peak_bytes"] <= memory, "MEMORY_BOUND")
            result["check"] = checked
        result["status"] = "PASS"
    except Exception:
        result["error"] = "IMAGE_VALIDATION_FAILED"
    finally:
        try:
            existing = backend.inspect("container", name)
            if existing is not None:
                h.require(existing["Config"]["Labels"].get("io.mifolyo.cj2.image-check") == token, "CHECK_OWNER")
                backend.remove("container", name)
            h.require(backend.inspect("container", name) is None, "CHECK_REMAINS")
            result["cleanup"] = "verified"
        except Exception:
            result["cleanup"], result["status"] = "not_proven", "FAIL"
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--harness-image", required=True)
    parser.add_argument("--redis-image", required=True)
    parser.add_argument("--architecture", choices=("arm64", "amd64"), default="arm64")
    parser.add_argument("--output-dir", type=Path, required=True)
    args = parser.parse_args()
    h.require(args.output_dir.is_dir() and not args.output_dir.is_symlink(), "OUTPUT_DIRECTORY")
    for value in (args.harness_image, args.redis_image):
        h.require(value.startswith("sha256:") and h.nonzero(value[7:]), "IMAGE_ID")
    backend = ctl.Docker()
    backend.deadline = time.monotonic() + 600
    images = {}
    for role, value in (("harness", args.harness_image), ("redis", args.redis_image)):
        raw = backend.inspect("image", value)
        images[role] = ctl.image_admission(raw, value, args.architecture, role == "harness")
    prestart = validate_prestart(backend, args.harness_image, args.redis_image)
    checks = [validate(backend, args.redis_image, args.redis_image, "redis-version"),
              validate(backend, args.harness_image, args.redis_image, "init"),
              validate(backend, args.harness_image, args.redis_image, "executor")] if prestart["status"] == "PASS" else []
    report = {"version": 1, "architecture": args.architecture, "images": images, "checks": checks, "prestart": prestart,
              "status": "PASS" if prestart["status"] == "PASS" and all(c["status"] == "PASS" for c in checks) else "FAIL",
              "redis_started": False, "execution_authorized": False}
    if report["status"] == "PASS":
        inputs = {"format_version": 1, "scenario": "ledger-smoke", "redis_version": "7.4.11",
                  "redis_image": args.redis_image, "harness_image": args.harness_image,
                  "standin_image": args.harness_image}
        plan, recipe = h.compile_plan(inputs), ctl.case.recipe()
        for name, artifact in (("inputs.json", inputs), ("plan.json", plan), ("recipe.json", recipe)):
            with (args.output_dir / name).open("xb") as stream:
                stream.write(h.canonical(artifact))
        report["plan_sha256"] = h.digest(h.canonical(plan))
        report["recipe_sha256"] = h.digest(h.canonical(recipe))
    with (args.output_dir / "image-validation.json").open("xb") as stream:
        stream.write(h.canonical(report))
    print(json.dumps({"status": report["status"], "prestart": prestart, "checks": [
        {k: v for k, v in c.items() if k != "check"} | ({"peak_bytes": c["check"]["cgroup_memory_peak_bytes"]} if "check" in c else {})
        for c in checks]}, sort_keys=True))
    return 0 if report["status"] == "PASS" else 1


if __name__ == "__main__":
    raise SystemExit(main())
