"""Provisioning/SSH guards using private files, benign pipes and simulated Linux."""
import base64
import copy
import errno
import os
from pathlib import Path
import select
import signal
import shlex
import stat
import struct
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

import contracts as c
import context_files as files
import context_provision as provision
import context_ssh
import context_transport as transport
import context_worker
import runtime_context as context
from streams import Deadline, WireBudget
import test_runtime_admission as admission_tests
from test_runtime_context import fixture, KEY


def public_key(number):
    raw = struct.pack(">I", 11) + b"ssh-ed25519" + struct.pack(">I", 32) + bytes([number]) * 32
    return "ssh-ed25519 " + base64.b64encode(raw).decode()


def identity_bytes():
    # Deliberately invalid, public fixture material. Real authentication is not
    # attempted by the Python stand-ins or the offline `ssh -G` check.
    return b"-----BEGIN OPENSSH " + b"PRIVATE KEY-----\nfixture-only-not-a-key\n-----END OPENSSH " + b"PRIVATE KEY-----\n"


def deployment():
    anchor = fixture()[2]
    return {"version": 1, "anchor": anchor, "host": "127.0.0.1", "port": 2222, "service_uid": 3001, "service_gid": 3002,
        "ssh_host_key": public_key(1), "ssh_client_key": public_key(2),
        "ssh_client_sha256": c.sha(Path(transport.SSH).read_bytes()), "host_python_sha256": c.sha(b"operator-pinned-host-python")}


def prepare(value):
    result = admission_tests.preparation()
    result["artifacts"].update(runtime_context_source=value["anchor"]["source_sha256"],
        runtime_context_deployment=provision.deployment_digest(value), runtime_context_transport=provision.transport_digest())
    return result


ACTOR = r'''
import json, os, subprocess, sys, time
from pathlib import Path
mode, package, bundle = sys.argv[1:]
if mode == "hang": time.sleep(20)
raw = sys.stdin.buffer.read()
if mode == "stderr":
    os.write(2, b"private-transport-canary\n")
elif mode == "overflow":
    os.write(1, b"x" * 4097)
elif mode == "empty":
    pass
elif mode == "descendant":
    subprocess.Popen([sys.executable, "-I", "-B", "-c", "import time; time.sleep(20)"], close_fds=True)
elif mode == "echo":
    os.write(1, raw)
else:
    sys.path.insert(0, package)
    import runtime_context as context
    from streams import Deadline
    from test_runtime_context import Reader
    value = json.loads((Path(bundle) / "deployment.json").read_bytes())
    key = (Path(bundle) / "context-source.key").read_bytes()
    reply = context.LinuxContextEndpoint(value["anchor"], key, Reader()).exchange(raw, Deadline(2))
    if mode == "partial": reply = reply[:-1]
    if mode == "extra": reply += b"x"
    os.write(1, reply)
    if mode == "exit": raise SystemExit(1)
'''


class ProvisioningTests(unittest.TestCase):
    def test_bundle_has_closed_host_layout_and_private_credentials(self):
        value = deployment()
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder) / "new"
            record = provision.create_bundle(root, value, identity_bytes(), key=KEY)
            self.assertEqual(stat.S_IMODE(root.stat().st_mode), 0o700)
            self.assertEqual((root / "host/etc/context-source.key").read_bytes(), KEY)
            self.assertEqual((root / "controller/context-source.key").read_bytes(), KEY)
            for path in root.rglob("*"):
                self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o700 if path.is_dir() else 0o600)
            self.assertEqual({path.name for path in (root / "host/code").iterdir()}, set(context.SOURCE_FILES))
            for name, digest in record["files"].items(): self.assertEqual(c.sha((root / name).read_bytes()), digest)
            raw = c.canonical(record)
            self.assertNotIn(KEY, raw); self.assertNotIn(KEY.hex().encode(), raw)
            self.assertNotIn(identity_bytes(), raw)
            self.assertFalse(record["installation_performed"] or record["runtime_evidence"] or record["execution_authorized"])
            self.assertEqual((root / "host/etc/authorized_keys").read_text(),
                'restrict,command="' + files.FORCED_COMMAND + '" ' + value["ssh_client_key"] + "\n")

    def test_secret_generation_does_not_change_public_manifest(self):
        with tempfile.TemporaryDirectory() as folder:
            root, value = Path(folder), deployment()
            one = provision.create_bundle(root / "one", value, identity_bytes())
            two = provision.create_bundle(root / "two", value, identity_bytes())
            self.assertEqual(one, two)
            self.assertNotEqual((root / "one/controller/context-source.key").read_bytes(), (root / "two/controller/context-source.key").read_bytes())

    def test_existing_destination_and_symlink_are_never_overwritten(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            (root / "marker").write_bytes(b"preserve")
            link = root / "link"; link.symlink_to(root, target_is_directory=True)
            for destination in (root, link):
                with self.assertRaises(FileExistsError): provision.create_bundle(destination, deployment(), identity_bytes(), key=KEY)
            self.assertEqual((root / "marker").read_bytes(), b"preserve")

    def test_source_mismatch_refuses_before_materialization(self):
        value = deployment(); value["anchor"]["source_sha256"] = c.sha(b"foreign-source")
        with tempfile.TemporaryDirectory() as folder:
            target = Path(folder) / "new"
            with self.assertRaises(c.Invalid): provision.create_bundle(target, value, identity_bytes(), key=KEY)
            self.assertFalse(target.exists())

    def test_bundle_verification_refuses_code_policy_secret_and_extra_file_changes(self):
        for change in ("code", "manifest", "key", "extra"):
            with self.subTest(change=change), tempfile.TemporaryDirectory() as folder:
                root = Path(folder) / "bundle"
                provision.create_bundle(root, deployment(), identity_bytes(), key=KEY)
                if change == "code": (root / "host/code/context_ssh.py").write_bytes(b"unreviewed")
                elif change == "manifest":
                    value = c.decode((root / "manifest.json").read_bytes())
                    value["host_installation"]["key_mode"] = 0o644
                    (root / "manifest.json").write_bytes(c.canonical(value))
                elif change == "key": (root / "host/etc/context-source.key").write_bytes(b"z" * 32)
                else: (root / "host/code/extra.py").write_bytes(b"unexpected")
                with self.assertRaises(c.Invalid): provision.verify_bundle(root)

    def test_cli_bundle_validation_reports_only_public_identities(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder) / "bundle"
            provision.create_bundle(root, deployment(), identity_bytes(), key=KEY)
            result = subprocess.run([sys.executable, "-I", "-B", str(Path(provision.__file__)), "--check-bundle", str(root)],
                capture_output=True, timeout=3)
            self.assertEqual(result.returncode, 0, result.stderr.decode())
            self.assertEqual(c.decode(result.stdout)["status"], "PASS_PRIVATE_BUNDLE")
            self.assertNotIn(KEY, result.stdout + result.stderr)

    def test_host_and_key_injection_and_invalid_service_identity_refuse(self):
        for name, bad in (("host", "-oProxyCommand=bad"), ("host", "example.org"), ("host", "::%en0"),
                ("host", "0.0.0.0"), ("host", "224.0.0.1"), ("port", True), ("port", 0),
                ("service_uid", 0), ("service_gid", 999), ("ssh_host_key", public_key(1) + "\ncommand=bad"),
                ("ssh_client_key", "ssh-ed25519 xxxx"), ("ssh_client_sha256", "0" * 64)):
            with self.subTest(name=name, bad=bad), self.assertRaises(c.Invalid):
                provision.validate_deployment(deployment() | {name: bad})

    def test_only_canonical_ed25519_public_key_blobs_are_accepted(self):
        self.assertEqual(provision.public_key(public_key(1)), public_key(1))
        for value in (public_key(0), public_key(1) + " comment", public_key(1).replace("ssh-ed25519", "ssh-rsa"),
                "ssh-ed25519 " + base64.b64encode(b"not-an-ssh-key").decode()):
            with self.assertRaises(c.Invalid): provision.public_key(value)

    def test_effective_sshd_policy_requires_every_closed_restriction(self):
        wanted = provision.ssh_policy() | {"permituserenvironment": "no"}
        raw = "".join(key + " " + value + "\n" for key, value in wanted.items()).encode()
        result = provision.validate_sshd_output(raw)
        self.assertEqual(result["status"], "PASS_POLICY_INPUT")
        self.assertFalse(result["live_daemon_verified"])
        for key in wanted:
            changed = dict(wanted); changed[key] = "unsafe"
            with self.subTest(key=key), self.assertRaises(c.Invalid):
                provision.validate_sshd_output("".join(k + " " + v + "\n" for k, v in changed.items()).encode())
        with self.assertRaises(c.Invalid): provision.validate_sshd_output(raw + b"permittty no\n")

    def test_credential_modes_links_and_nonregular_files_refuse(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder) / "bundle"
            provision.create_bundle(root, deployment(), identity_bytes(), key=KEY)
            client = root / "controller"
            self.assertEqual(transport.credentials(client)["context-source.key"], KEY)
            key = client / "context-source.key"
            key.chmod(0o644)
            with self.assertRaises(c.Invalid): transport.credentials(client)
            key.chmod(0o600)
            hard = client / "hardlink"; os.link(key, hard)
            with self.assertRaises(c.Invalid): transport.credentials(client)
            hard.unlink()
            key.rename(client / "saved")
            key.symlink_to(client / "saved")
            with self.assertRaises(OSError): transport.credentials(client)
            key.unlink(); os.mkfifo(key, 0o600)
            with self.assertRaises(c.Invalid): transport.credentials(client)

    def test_directory_owner_permissions_and_symlink_refuse(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            with self.assertRaises(c.Invalid): files.directory(root, os.geteuid() + 1, 0o700)
            root.chmod(0o755)
            with self.assertRaises(c.Invalid): files.directory(root, os.geteuid(), 0o700)
            root.chmod(0o700)
            link = root / "link"; link.symlink_to(root, target_is_directory=True)
            with self.assertRaises(OSError): files.directory(link, os.geteuid(), 0o700)

    def test_fixed_worker_config_path_cannot_be_selected_by_caller(self):
        with self.assertRaisesRegex(c.Invalid, "CONTEXT_CONFIG_PATH"):
            context_worker.provisioned("/tmp/actor-chosen-anchor")

    def test_ssh_entrypoint_refuses_wrong_command_before_config_access(self):
        for command in (None, "", "sh", files.REMOTE_COMMAND + " extra"):
            env = {} if command is None else {"SSH_ORIGINAL_COMMAND": command}
            with patch.dict(os.environ, env, clear=True), patch.object(sys, "argv", ["context_ssh.py"]), \
                    patch.object(files, "read_config") as read, self.assertRaises(c.Invalid):
                context_ssh.admit_session()
            read.assert_not_called()

    def test_ssh_entrypoint_requires_service_identity(self):
        service = c.canonical({"uid": 3001, "gid": 3002, "python_sha256": c.sha(b"python")})
        with patch.dict(os.environ, {"SSH_ORIGINAL_COMMAND": files.REMOTE_COMMAND}), \
                patch.object(sys, "argv", ["context_ssh.py"]), patch.object(files, "read_config", return_value=service), \
                patch("context_ssh.os.getuid", return_value=0), self.assertRaisesRegex(c.Invalid, "CONTEXT_SERVICE_IDENTITY"):
            context_ssh.admit_session()

    def test_ssh_entrypoint_checks_exact_code_inventory_and_interpreter(self):
        # Ownership/credentials are explicitly simulated; file bytes/modes and
        # inventory are real private fixtures. No host account or /etc is changed.
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder) / "code"; root.mkdir(mode=0o755)
            root.chmod(0o755)  # Simulated install mode, independent of the caller's umask.
            for name in context.SOURCE_FILES:
                path = root / name; path.write_bytes(b"source fixture\n"); path.chmod(0o444)
            service = {"uid": 3001, "gid": 3002, "python_sha256": c.sha(b"host-python")}
            actual_uid, real_directory, real_read = os.geteuid(), files.directory, files.read_at
            def directory(path, uid, mode):
                self.assertEqual((uid, mode), (0, 0o755))
                return real_directory(path, actual_uid, mode)
            def read(fd, name, uid, modes, limit):
                self.assertEqual((uid, modes), (0, (0o444,)))
                return real_read(fd, name, actual_uid, modes, limit)
            with patch.dict(os.environ, {"SSH_ORIGINAL_COMMAND": files.REMOTE_COMMAND}), \
                    patch.object(sys, "argv", ["context_ssh.py"]), patch.object(context_ssh, "__file__", str(root / "context_ssh.py")), \
                    patch.object(files, "CODE", root), patch.object(files, "read_config", return_value=c.canonical(service)), \
                    patch.object(files, "directory", side_effect=directory), patch.object(files, "read_at", side_effect=read), \
                    patch("context_ssh.os.getuid", return_value=3001), patch("context_ssh.os.geteuid", return_value=3001), \
                    patch("context_ssh.os.getgid", return_value=3002), patch("context_ssh.os.getegid", return_value=3002), \
                    patch("context_ssh.os.getgroups", return_value=[3002]), \
                    patch.object(context_ssh.LinuxReader, "executable", return_value=service["python_sha256"]) as executable:
                context_ssh.admit_session()
                executable.return_value = c.sha(b"different-python")
                with self.assertRaisesRegex(c.Invalid, "CONTEXT_PYTHON_IDENTITY"): context_ssh.admit_session()
                executable.return_value = service["python_sha256"]
                (root / "extra.py").write_bytes(b"extra")
                with self.assertRaisesRegex(c.Invalid, "CONTEXT_CODE_INVENTORY"): context_ssh.admit_session()

    def test_stable_file_reader_detects_path_replacement(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            (root / "one").write_bytes(b"first"); (root / "one").chmod(0o600)
            (root / "two").write_bytes(b"other"); (root / "two").chmod(0o600)
            substitute = (root / "two").stat()
            fd = files.directory(root, os.geteuid(), 0o700)
            try:
                with patch("context_files.os.stat", return_value=substitute), self.assertRaisesRegex(c.Invalid, "CONTEXT_FILE_CHANGED"):
                    files.read_at(fd, "one", os.geteuid(), (0o600,), 5)
            finally: os.close(fd)

    def test_packet_requires_deployment_and_transport_identities(self):
        prep = prepare(deployment())
        for field in ("runtime_context_deployment", "runtime_context_transport"):
            changed = copy.deepcopy(prep); changed["artifacts"].pop(field)
            with self.assertRaises(c.Invalid): c.validate_preparation(changed)


class SSHTransportTests(unittest.TestCase):
    def run_source(self, mode="valid", *, before=None, after=None, seconds=2):
        with tempfile.TemporaryDirectory() as folder:
            root, value = Path(folder) / "bundle", deployment()
            provision.create_bundle(root, value, identity_bytes(), key=KEY)
            client = root / "controller"
            prep = prepare(value)
            source = transport.provisioned_source(client, prep, authorize=lambda *_: True)
            calls, children = [], []
            actual_exchange = transport.exchange_process
            def spawn(argv, **kwargs):
                calls.append(argv)
                self.assertEqual(argv[0], transport.SSH)
                self.assertEqual(argv[-2:], [value["host"], files.REMOTE_COMMAND])
                self.assertNotIn(KEY.decode(), str(argv) + str(kwargs))
                identity = Path(argv[argv.index("-i") + 1])
                options = dict(argv[i + 1].split("=", 1) for i, part in enumerate(argv[:-1]) if part == "-o")
                known = Path(shlex.split(options["UserKnownHostsFile"])[0])
                self.assertEqual(identity.read_bytes(), identity_bytes())
                self.assertEqual(stat.S_IMODE(identity.stat().st_mode), 0o600)
                self.assertEqual(stat.S_IMODE(identity.parent.stat().st_mode), 0o700)
                self.assertEqual(known.read_text(), files.HOST_ALIAS + " " + value["ssh_host_key"] + "\n")
                self.assertEqual(set(kwargs["env"]), {"PATH", "LC_ALL", "SSH_ASKPASS_REQUIRE"})
                process = subprocess.Popen([sys.executable, "-I", "-B", "-c", ACTOR, mode,
                    str(Path(__file__).resolve().parent), str(client)], **kwargs)
                children.append(process)
                return process
            def exchange(argv, raw, deadline):
                result = actual_exchange(argv, raw, deadline, spawn=spawn)
                if after: after(client, source)
                return result
            if before: before(client, source, prep)
            try:
                with patch.object(transport, "exchange_process", side_effect=exchange):
                    lease = source.acquire("P1.pre.1.1", c.sha(b"scope"), prep, value["anchor"]["daemon_sha256"], Deadline(seconds), WireBudget())
                self.assertEqual(lease.binding["case_id"], "P1.pre.1.1")
                self.assertFalse(lease.receipt["execution_authorized"] or lease.receipt["runtime_evidence"])
                return lease, calls
            finally:
                for child in children:
                    self.assertIsNotNone(child.returncode)
                    self.assertTrue(all(pipe.closed for pipe in (child.stdin, child.stdout, child.stderr)))

    def test_signed_context_roundtrip_over_benign_process_pipes(self):
        lease, calls = self.run_source()
        self.assertEqual(len(calls), 1)
        self.assertEqual(lease._context["host_pid_ns"], fixture()[2]["namespaces"]["pid_ns"])

    def test_nonzero_exit_cannot_accept_even_a_valid_signed_response(self):
        with self.assertRaises(c.Invalid): self.run_source("exit")

    def test_empty_partial_extra_and_oversized_output_refuse(self):
        for mode in ("empty", "partial", "extra", "overflow"):
            with self.subTest(mode=mode), self.assertRaises(c.Invalid): self.run_source(mode)

    def test_stderr_is_bounded_and_value_free(self):
        with self.assertRaises(c.Invalid) as caught: self.run_source("stderr")
        self.assertNotIn("private-transport-canary", str(caught.exception))
        self.assertNotIn(KEY.decode(), str(caught.exception))

    def test_hung_peer_is_killed_within_deadline_and_cleanup_budget(self):
        started = time.monotonic()
        with self.assertRaises(c.Invalid): self.run_source("hang", seconds=0.15)
        self.assertLess(time.monotonic() - started, 3)

    def test_descendant_held_pipe_cannot_turn_leader_exit_into_success(self):
        started = time.monotonic()
        with self.assertRaises(c.Invalid): self.run_source("descendant", seconds=0.3)
        self.assertLess(time.monotonic() - started, 3)

    def test_credential_change_before_transport_refuses_without_launch(self):
        def changed(client, source, prep):
            (client / "context-source.key").write_bytes(b"z" * 32)
        with self.assertRaises(c.Invalid): self.run_source(before=changed)

    def test_credential_change_after_valid_response_invalidates_exchange(self):
        def changed(client, source): (client / "context-source.key").write_bytes(b"z" * 32)
        with self.assertRaises(c.Invalid): self.run_source(after=changed)

    def test_mutating_public_deployment_copy_cannot_redirect_transport(self):
        def mutate(client, source, prep): source.exchange.deployment["host"] = "192.0.2.1"
        _, calls = self.run_source(before=mutate)
        self.assertEqual(calls[0][-2], "127.0.0.1")

    def test_missing_authorization_refuses_before_transport(self):
        def denied(client, source, prep): source.authorize = None
        with self.assertRaisesRegex(c.Invalid, "CONTEXT_NOT_APPROVED"): self.run_source(before=denied)

    def test_wrong_ssh_binary_digest_refuses_before_launch(self):
        with tempfile.TemporaryDirectory() as folder:
            value = deployment(); value["ssh_client_sha256"] = c.sha(b"wrong-binary")
            root = Path(folder) / "bundle"
            provision.create_bundle(root, value, identity_bytes(), key=KEY)
            source = transport.provisioned_source(root / "controller", prepare(value), authorize=lambda *_: True)
            with patch.object(transport, "exchange_process") as execute, self.assertRaises(c.Invalid):
                source.acquire("P1.pre.1.1", c.sha(b"scope"), prepare(value), value["anchor"]["daemon_sha256"], Deadline(1), WireBudget())
            execute.assert_not_called()

    def test_deployment_or_transport_packet_mismatch_refuses(self):
        with tempfile.TemporaryDirectory() as folder:
            value, root = deployment(), Path(folder) / "bundle"
            provision.create_bundle(root, value, identity_bytes(), key=KEY)
            for field in ("runtime_context_deployment", "runtime_context_transport", "runtime_context_source"):
                prep = prepare(value); prep["artifacts"][field] = c.sha(b"foreign")
                with self.subTest(field=field), self.assertRaises(c.Invalid):
                    transport.provisioned_source(root / "controller", prep)

    def test_reused_source_rechecks_provisioning_before_authorization_or_io(self):
        with tempfile.TemporaryDirectory() as folder:
            value, root = deployment(), Path(folder) / "bundle"
            provision.create_bundle(root, value, identity_bytes(), key=KEY)
            endpoint = context.LinuxContextEndpoint(value["anchor"], KEY, fixture()[0])
            for in_place in (False, True):
                for field in ("runtime_context_deployment", "runtime_context_transport", "runtime_context_source"):
                    with self.subTest(field=field, in_place=in_place):
                        prep = prepare(value)
                        source = transport.provisioned_source(root / "controller", prep)
                        with patch.object(source, "authorize", return_value=True) as authorize, \
                                patch.object(transport.SSHContextTransport, "__call__", side_effect=endpoint.exchange) as exchange:
                            source.acquire("P1.pre.1.1", c.sha(b"scope"), prep, value["anchor"]["daemon_sha256"], Deadline(2), WireBudget())
                            authorize.reset_mock(); exchange.reset_mock()
                            changed = prep if in_place else copy.deepcopy(prep)
                            changed["artifacts"][field] = c.sha(b"different-prepared-artifact")
                            with self.assertRaises(c.Invalid):
                                source.acquire("P1.pre.1.1", c.sha(b"scope"), changed, value["anchor"]["daemon_sha256"], Deadline(2), WireBudget())
                            authorize.assert_not_called(); exchange.assert_not_called()

    def test_reused_source_accepts_matching_bindings_with_fresh_challenges(self):
        with tempfile.TemporaryDirectory() as folder:
            value, root = deployment(), Path(folder) / "bundle"
            provision.create_bundle(root, value, identity_bytes(), key=KEY)
            prep = prepare(value)
            source = transport.provisioned_source(root / "controller", prep, authorize=lambda *_: True)
            endpoint = context.LinuxContextEndpoint(value["anchor"], KEY, fixture()[0])
            changed = copy.deepcopy(prep); changed["artifacts"]["target_elf"] = c.sha(b"another-target")
            requests = []
            with patch.object(transport.SSHContextTransport, "__call__", side_effect=endpoint.exchange) as exchange:
                for packet in (prep, prep, changed):
                    deadline = Deadline(2)
                    lease = source.acquire("P1.pre.1.1", c.sha(b"scope"), packet, value["anchor"]["daemon_sha256"], deadline, WireBudget())
                    lease.view("P1.pre.1.1", c.sha(b"scope"), packet, deadline)
                    self.assertEqual(lease.binding["preparation_sha256"], c.sha(c.canonical(packet)))
                    requests.append(lease.receipt["request_sha256"])
                self.assertEqual(exchange.call_count, 3)
            self.assertEqual(len(set(requests)), 3)

    def test_public_provisioning_receipt_cannot_rebind_a_reused_source(self):
        with tempfile.TemporaryDirectory() as folder:
            value, root = deployment(), Path(folder) / "bundle"
            provision.create_bundle(root, value, identity_bytes(), key=KEY)
            prep = prepare(value)
            source = transport.provisioned_source(root / "controller", prep, authorize=lambda *_: True)
            changed = copy.deepcopy(prep)
            for artifact, receipt in (("runtime_context_deployment", "deployment_sha256"), ("runtime_context_transport", "transport_source_sha256")):
                changed["artifacts"][artifact] = source.provisioning_receipt[receipt] = c.sha(b"unadmitted-binding")
            endpoint = context.LinuxContextEndpoint(value["anchor"], KEY, fixture()[0])
            with patch.object(transport.SSHContextTransport, "__call__", side_effect=endpoint.exchange) as exchange:
                with self.assertRaises(c.Invalid):
                    source.acquire("P1.pre.1.1", c.sha(b"scope"), changed, value["anchor"]["daemon_sha256"], Deadline(2), WireBudget())
                exchange.assert_not_called()

    def test_private_credential_copies_and_ambient_ssh_settings_are_controlled(self):
        _, calls = self.run_source()
        argv = calls[0]
        options = dict(argv[index + 1].split("=", 1) for index, part in enumerate(argv[:-1]) if part == "-o")
        for key, value in {"BatchMode": "yes", "StrictHostKeyChecking": "yes", "IdentityAgent": "none",
                "ProxyCommand": "none", "ProxyJump": "none", "ControlPath": "none", "PermitLocalCommand": "no",
                "ClearAllForwardings": "yes", "PasswordAuthentication": "no"}.items():
            self.assertEqual(options[key], value)
        self.assertFalse(Path(argv[argv.index("-i") + 1]).exists())
        self.assertFalse(Path(shlex.split(options["UserKnownHostsFile"])[0]).exists())

    def test_real_ssh_offline_configuration_has_no_proxy_agent_or_tofu(self):
        with tempfile.TemporaryDirectory() as folder:
            argv = transport.ssh_argv(deployment(), Path(folder) / "identity", Path(folder) / "hosts", 1)
            result = subprocess.run([argv[0], "-G", *argv[1:]], capture_output=True, timeout=3,
                env={"PATH": "/usr/bin:/bin", "LC_ALL": "C"})
            self.assertEqual(result.returncode, 0, result.stderr.decode())
            rows = dict(line.split(" ", 1) for line in result.stdout.decode().splitlines())
            self.assertEqual(rows["user"], files.SERVICE_USER)
            self.assertEqual(rows["hostname"], "127.0.0.1")
            self.assertEqual(rows["stricthostkeychecking"], "true")
            self.assertEqual(rows["identityagent"], "none")
            self.assertEqual(rows["hostkeyalias"], files.HOST_ALIAS)
            self.assertEqual(rows["globalknownhostsfile"], "/dev/null")
            self.assertNotIn("proxycommand", rows)
            self.assertEqual(rows["requesttty"], "false")

    def test_expired_deadline_and_oversize_input_refuse_before_spawn(self):
        for raw in (b"x" * 1025, b"no-newline"):
            with patch.object(transport.subprocess, "Popen") as spawn, self.assertRaises(c.Invalid):
                transport.exchange_process([], raw, Deadline(1), spawn=spawn)
            spawn.assert_not_called()
        deadline = Deadline(0.001); time.sleep(0.005)
        with patch.object(transport.subprocess, "Popen") as spawn, self.assertRaises(c.Invalid):
            transport.exchange_process([], b"x\n", deadline, spawn=spawn)
        spawn.assert_not_called()

    def test_selector_setup_failure_cannot_leave_a_spawned_process(self):
        children = []
        def spawn(argv, **kwargs):
            child = subprocess.Popen([sys.executable, "-I", "-B", "-c", "import time; time.sleep(20)"], **kwargs)
            children.append(child)
            return child
        try:
            with patch.object(transport.selectors, "DefaultSelector", side_effect=OSError(errno.ENFILE, "selector unavailable")), \
                    self.assertRaises(OSError):
                transport.exchange_process([], b"probe\n", Deadline(1), spawn=spawn)
            self.assertFalse(children, "Selector admission must precede process creation")
        finally:
            for child in children:
                if child.poll() is None:
                    os.killpg(child.pid, signal.SIGKILL)
                child.wait(timeout=3)
                for pipe in (child.stdin, child.stdout, child.stderr): pipe.close()

    def test_spawn_failure_closes_any_created_selector(self):
        selectors, real_selector = [], transport.selectors.DefaultSelector
        def selector():
            mux = real_selector()
            selectors.append(mux)
            return mux
        def spawn(*args, **kwargs):
            raise OSError(errno.EMFILE, "spawn unavailable")
        try:
            with patch.object(transport.selectors, "DefaultSelector", side_effect=selector), \
                    self.assertRaises(OSError):
                transport.exchange_process([], b"probe\n", Deadline(1), spawn=spawn)
            self.assertTrue(all(mux.get_map() is None for mux in selectors))
        finally:
            for mux in selectors: mux.close()

    def test_pipe_setup_failure_reaps_the_child_and_closes_resources(self):
        children = []
        mux = transport.selectors.DefaultSelector()
        def spawn(argv, **kwargs):
            child = subprocess.Popen([sys.executable, "-I", "-B", "-c", "import time; time.sleep(20)"], **kwargs)
            children.append(child)
            return child
        try:
            with patch.object(transport.selectors, "DefaultSelector", return_value=mux), \
                    patch.object(transport.os, "set_blocking", side_effect=OSError(errno.EIO, "pipe setup unavailable")), \
                    self.assertRaises(OSError):
                transport.exchange_process([], b"probe\n", Deadline(1), spawn=spawn)
            self.assertEqual(len(children), 1)
            self.assertIsNotNone(children[0].returncode)
            self.assertTrue(all(pipe.closed for pipe in (children[0].stdin, children[0].stdout, children[0].stderr)))
            self.assertIsNone(mux.get_map())
        finally:
            mux.close()
            for child in children:
                if child.poll() is None:
                    os.killpg(child.pid, signal.SIGKILL)
                child.wait(timeout=3)
                for pipe in (child.stdin, child.stdout, child.stderr): pipe.close()

    def exited_leader_setup_failure(self, operation):
        # Retain the exited leader unreaped while its owned descendant holds
        # stdout open. A duplicate reader observes actual closure after cleanup.
        actor = """
import os, time
child = os.fork()
if child == 0:
    time.sleep(20)
    os._exit(0)
os.write(1, (str(child) + "\\n").encode())
os._exit(0)
"""
        children, output, events = [], [], []
        mux, real_killpg = transport.selectors.DefaultSelector(), os.killpg
        def spawn(argv, **kwargs):
            child = subprocess.Popen([sys.executable, "-I", "-B", "-c", actor], **kwargs)
            children.append(child)
            end = time.monotonic() + 3
            while os.waitid(os.P_PID, child.pid, os.WEXITED | os.WNOWAIT | os.WNOHANG) is None:
                self.assertLess(time.monotonic(), end, "owned leader did not exit")
                time.sleep(0.005)
            self.assertIsNone(child.returncode)
            output.append(os.dup(child.stdout.fileno()))
            self.assertTrue(select.select(output, [], [], 1)[0])
            self.assertTrue(os.read(output[0], 64).strip().isdigit())
            self.assertFalse(select.select(output, [], [], 0)[0], "descendant must hold the pipe")
            wait = child.wait
            def observed_wait(*args, **kwargs):
                events.append("wait")
                return wait(*args, **kwargs)
            child.wait = observed_wait
            return child
        def killpg(pid, sig):
            self.assertEqual((pid, sig), (children[0].pid, signal.SIGKILL))
            events.append("kill")
            return real_killpg(pid, sig)
        owner, method = (transport.os, "set_blocking") if operation == "nonblocking" else (mux, "register")
        try:
            with patch.object(transport.selectors, "DefaultSelector", return_value=mux), \
                    patch.object(owner, method, side_effect=OSError(errno.EIO, "first pipe setup refused")), \
                    patch.object(transport.os, "killpg", side_effect=killpg), self.assertRaises(OSError):
                transport.exchange_process([], b"probe\n", Deadline(5), spawn=spawn)
            self.assertEqual(events[:2], ["kill", "wait"])
            self.assertIsNotNone(children[0].returncode)
            self.assertTrue(all(pipe.closed for pipe in (children[0].stdin, children[0].stdout, children[0].stderr)))
            self.assertIsNone(mux.get_map())
            self.assertTrue(select.select(output, [], [], 2)[0], "descendant still holds its pipe")
            self.assertEqual(os.read(output[0], 1), b"")
        finally:
            for child in children:
                # An unreaped leader or our still-open inherited pipe retains
                # the owned group identity, including against the unfixed code.
                if child.returncode is None or (output and not select.select(output, [], [], 0)[0]):
                    try: real_killpg(child.pid, signal.SIGKILL)
                    except ProcessLookupError: pass
                child.wait(timeout=3)
                for pipe in (child.stdin, child.stdout, child.stderr): pipe.close()
            for fd in output: os.close(fd)
            mux.close()

    def test_first_nonblocking_failure_kills_exited_leaders_descendant(self):
        self.exited_leader_setup_failure("nonblocking")

    def test_first_registration_failure_kills_exited_leaders_descendant(self):
        self.exited_leader_setup_failure("registration")

    def test_backend_preflight_binds_deployment_before_any_actor_activation(self):
        helper = admission_tests.DockerAdmissionTests()
        registry, _, backend, _, _ = helper.fixture()
        value = deployment(); value["anchor"]["daemon_sha256"] = registry.scope["daemon_sha256"]
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder) / "bundle"
            provision.create_bundle(root, value, identity_bytes(), key=KEY)
            endpoint = context.LinuxContextEndpoint(value["anchor"], KEY, fixture()[0])
            # SSH framing is covered with real pipes above; this control isolates
            # outer source/daemon/preparation joining without contacting a host.
            with patch.object(transport.SSHContextTransport, "__call__", side_effect=endpoint.exchange):
                lease = backend.acquire_provisioned_context(root / "controller", prepare(value), WireBudget(), Deadline(2), authorize_source=lambda *_: True)
            self.assertFalse(backend._actor_streams)
            self.assertEqual(lease.receipt["provisioning"]["deployment_sha256"], provision.deployment_digest(value))
            self.assertFalse(lease.receipt["provisioning"]["host_installation_verified"])
            backend._actor_streams["already-active"] = object()
            with self.assertRaisesRegex(c.Invalid, "CONTEXT_BEFORE_ACTORS"):
                backend.acquire_provisioned_context(root / "controller", prepare(value), WireBudget(), Deadline(1))


if __name__ == "__main__":
    unittest.main()
