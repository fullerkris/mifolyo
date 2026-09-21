#!/usr/bin/env python3
"""Independent review probes: source-only/local fakes, NEVER Redis or Docker.

The memory probe calls the executor's real request validator in a local Python
child. RSS is a local observation (possibly including pre-exec high-water usage),
not a Linux cgroup measurement; traced heap is reported separately.
The timeout probes call the real controller with explicitly modeled OS/Docker
boundaries. They neither run Docker nor prove the target daemon's behavior.
The inventory is a fixed snapshot independently obtained with shasum -a 256.
"""
from __future__ import annotations

import argparse
import hashlib
from pathlib import Path
import resource
import subprocess
import sys
import tracemalloc

HERE = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(HERE))

import executor as worker
import harness as h
import runtime_case as case

REVIEWED_HEAD = "b931f36ada3ed9a4495b8661d1444f10ff73265f"
REVIEWED_SHA256 = """\
dd044b259fd1da45365c3c14b7d322420fb5b44eb29fc08c86e7ef1d826f9fff  tests/crawl-jobs-v2-redis/controller.py
81c246dd5bd29b1ec2e5ae62bc64ad9128fb3a9b176be97d13900c2bc873e603  tests/crawl-jobs-v2-redis/executor.py
d8c00fbf04367a5d765ad0f91c385c6c93ef251d7d69f1bed51b171b6c5babbf  tests/crawl-jobs-v2-redis/runtime_case.py
b702c4a1190bcbecc299eb7e9637b7cbe898ef8358cf16a34a277f84d5402fc2  tests/crawl-jobs-v2-redis/resp.py
a7ec642371b2c7f32511e9a842c4244fffc38c7c95359ffc86a6e1695f261637  tests/crawl-jobs-v2-redis/harness.py
40dd75d86bd6edacfa64fb832c46652151030c06066856007bfdb7d7854522fd  tests/crawl-jobs-v2-redis/redis.conf
7e675d478965ec5cd8b2d4a65469f5a837464b19eae88a7512e6a3e4cd0ffe05  tests/crawl-jobs-v2-redis/Dockerfile.execution
c2fc4875d1f23a62ad9f479597f23defa47953ab386f651e6b619b76e27a2aa0  tests/crawl-jobs-v2-redis/Dockerfile.execution.dockerignore
c30dba17ac8b0e1a284fa7034629ac090def24d7917aa71540de4b7532509631  tests/crawl-jobs-v2-redis/test_execution.py
3f44f380bf023c4d9294026beab813ff49267e727597a32a05e24e5ccb681aaa  tests/crawl-jobs-v2-redis/test_harness.py
6983f4d4655d7da1eff512725de4122c378a89fe874211ba377fdfe531c37f33  tests/crawl-jobs-v2-redis/README.md
a0352ec09a63e9998144b7f4681fc498458c46de539d1d91d5d2e757fbd0ac09  services/spider/internal/database/crawljobsv2/m4_offline_artifacts_test.go
a7922db2eb4655d2a85ac4ba92a25d0a28c8932f8e65f94d436b299ec318f641  services/spider/Dockerfile
72036a2d10ac735ec0de80756f02e6a2401a742c9e11054a7267f2df88241acd  services/spider/Dockerfile.dockerignore
835e98db86e4d0cba224bc9fb9c3a3408514e4730d832773b7448f9b238a69c9  docs/crawl-jobs-v2.md
a76ae0f6bdff1b2c24a29c328c51ba9cadb101914dce20f1a79360c152a7b4ea  docs/crawl-jobs-v2-plan.md
c24816904857c80837e417b16cf0587df09d370d308d33c08386ab1d31d71bdc  .github/workflows/required-checks.yml
b864de95a14aed804fd387a56036c221ee906b771e699ca870468dcef2dde13f  scripts/generate-crawl-jobs-v2-bundle.py
5582ad554f4bdf5b44ef5f0cae3abe51b5e4c7bb9d350c8647d0ceb356415fe5  services/spider/internal/database/crawljobsv2/lua/cj2_approve_boot.lua
97aec3d6bf98d422fe3f890064ad387eefeb34e0575ce85323e7e057d711c8bf  services/spider/internal/database/crawljobsv2/lua/cj2_maintain_rate_scopes.lua
5ace179857870c8659f36f8dd0f5e0d3c556be97364c759098e99e08428f3792  contracts/crawl-jobs-v2/digest-vectors.json
fa42e95305e01e560796567c86c6ae2cb126df12b48df3fadf8f8c925e8caf85  scripts/verify-crawl-jobs-v2-digests.py
fc65c818e19f910c4ddd07a339e9ffd918470adb0a2097b4c295b01793521f49  services/spider/internal/database/crawljobsv2/script_bundle_generated.go
76a9ec19ff49140db86f1f0a285be8c3da83456587e8b4c78ffdadbe742bdbf4  services/spider/internal/database/crawljobsv2/lua_src/gate.lua
0d6f9a63822f4efbc6997a834e4519d0b133ff944850dd7eae373b9c59920e44  services/spider/internal/database/crawljobsv2/lua_src/context.lua
5e9a48480ecbaf857da5dee8d2ca705cb8a6544acae8c069ea540cfe4dcf4bef  services/spider/internal/database/crawljobsv2/lua_src/read.lua
ad10f4ca7e323331d515f1fbaec295b1890fc187cdbf2a88b3fbc8f7b181fda6  services/spider/internal/database/crawljobsv2/lua_src/plan.lua
3ebc3ef39cdfc39126b1c517576a288f3072f0a5ee3b043f978c639f1f3183d0  services/spider/internal/database/crawljobsv2/lua_src/ledger_maintenance.lua
"""


def inventory():
    rows = {}
    for line in REVIEWED_SHA256.splitlines():
        expected, name = line.split("  ", 1)
        actual = hashlib.sha256((h.ROOT / name).read_bytes()).hexdigest()
        assert actual == expected, f"Reviewed bytes changed: {name}"
        rows[name] = actual
    print(REVIEWED_SHA256, end="")
    print(f"PASS: {len(rows)} reviewed-file hashes unchanged")
    print("canonical inventory sha256: " + h.digest(h.canonical(rows)))


def timeout_process_group():
    """Model an exited group leader with a child retaining its output pipes.

    poll()==0 says nothing about whether other group members remain. The selector
    map stays nonempty because such a child still owns stdout/stderr. All OS
    interfaces below are mocked: no process or signal is actually created/sent.
    """
    from types import SimpleNamespace
    from unittest.mock import Mock, patch
    import controller as ctl

    process = SimpleNamespace(pid=12345, stdin=Mock(), stdout=Mock(), stderr=Mock(),
                              poll=Mock(return_value=0), wait=Mock(return_value=0))
    mux = Mock()
    mux.get_map.return_value = {1: "child-held output pipe"}
    manager = Mock()
    manager.__enter__ = Mock(return_value=mux)
    manager.__exit__ = Mock(return_value=False)
    with patch.object(ctl.subprocess, "Popen", return_value=process), \
         patch.object(ctl.selectors, "DefaultSelector", return_value=manager), \
         patch.object(ctl.os, "set_blocking"), \
         patch.object(ctl.time, "monotonic", side_effect=[10.0, 11.0]), \
         patch.object(ctl.os, "killpg") as killpg:
        try:
            ctl.command(["local-fake-command"], timeout=0.1)
        except ctl.CommandError as exc:
            assert str(exc) == "COMMAND_TIMEOUT"
        else:
            raise AssertionError("expected COMMAND_TIMEOUT")
        assert killpg.call_count == 0, "bug no longer reproduces: process group was killed"
        assert process.wait.call_count == 1
    print("REPRODUCED: COMMAND_TIMEOUT with exited leader => killpg calls: 0")
    print("Limit: OS/process-group boundary modeled; no real process was launched.")


def timeout_exec_order():
    """Model Docker exec outliving its timed-out attaching CLI.

    Tests the real execute() error path, not Docker itself. This facade cannot
    establish actual Redis effects or a target daemon's exec cancellation.
    Source basis: Moby v27.5.1 api/server/router/container/exec.go passes
    context.Background() to ContainerExecStart; daemon/exec.go treats the exec
    process separately from the attaching client. Consult those sources rather
    than treating this fake as evidence of a real daemon run.
    """
    import controller as ctl
    import test_execution as existing

    class LateExec(existing.FakeDocker):
        exec_running = False
        revoke_overlap = False

        def stage(self, name, stage, request, timeout):
            if stage == "resume":
                self.events.append(stage)
                self.exec_running = True
                raise ctl.CommandError("COMMAND_TIMEOUT")
            if stage == "revoke":
                self.revoke_overlap = self.exec_running
            return super().stage(name, stage, request, timeout)

        def remove(self, kind, name):
            if kind == "container" and name.endswith("-executor"):
                self.exec_running = False
            super().remove(kind, name)

    plan = existing.test_plan()
    backend = LateExec()
    report = ctl.execute(plan, existing.approval(plan), backend, revision_check=lambda _: None)
    assert backend.revoke_overlap, "bug no longer reproduces: cleanup waited for the exec"
    assert not backend.exec_running
    assert report["verdict"] == "FAIL" and report["failure_phase"] == "resume"
    print("REPRODUCED: revoke stage entered while timed-out resume exec still running")
    print("Executor container removal happens afterwards; report correctly remains FAIL.")
    print("Limit: fake models independent Docker exec lifetime; no Docker or Redis run.")


def memory():
    import test_execution as existing

    request = existing.ExecutorTests().request()
    result = subprocess.run(
        [sys.executable, "-B", str(Path(__file__).resolve()), "memory-child"],
        input=h.canonical(request), capture_output=True, timeout=60, check=True,
    )
    print(result.stdout.decode(), end="")


def memory_child():
    request = h.decode(sys.stdin.buffer.read(h.MAX_ARTIFACT_BYTES + 1))
    tracemalloc.start()
    worker.validate_request(request)
    current, maximum = tracemalloc.get_traced_memory()
    print("real executor.validate_request: PASS (offline; no environment/init stage)")
    print(f"peak simultaneously traced Python allocations: {maximum} bytes ({maximum / 1048576:.2f} MiB)")
    peak = resource.getrusage(resource.RUSAGE_SELF).ru_maxrss
    peak_bytes = peak if sys.platform == "darwin" else peak * 1024
    print(f"local peak RSS: {peak_bytes} bytes ({peak_bytes / 1048576:.2f} MiB)")
    print("controller init limit: 134217728 bytes (128 MiB)")
    print("controller executor limit: 268435456 bytes (256 MiB)")


def source_memory():
    tracemalloc.start()
    identities, operations = h.source_identity()
    current, maximum = tracemalloc.get_traced_memory()
    print(f"canonical source identity validated ({len(operations)} operations)")
    print(f"peak simultaneously traced Python allocations: {maximum} bytes ({maximum / 1048576:.2f} MiB)")
    peak = resource.getrusage(resource.RUSAGE_SELF).ru_maxrss
    peak_bytes = peak if sys.platform == "darwin" else peak * 1024
    print(f"local peak RSS: {peak_bytes} bytes ({peak_bytes / 1048576:.2f} MiB)")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("inventory", "timeout-process-group", "timeout-exec-order",
                                         "memory", "memory-child", "source-memory"))
    args = parser.parse_args()
    globals()[args.mode.replace("-", "_")]()


if __name__ == "__main__":
    main()
