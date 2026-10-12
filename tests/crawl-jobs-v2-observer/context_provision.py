"""Closed operator-supplied provisioning bundles; no host discovery or installation."""
import argparse
import base64
import copy
import ipaddress
import os
from pathlib import Path
import secrets
import struct
import sys

sys.path.insert(0, str(Path(__file__).resolve().parent))
from contracts import Invalid, canonical, decode, digest, exact, integer, require, sha
import context_files as files
from linux_observation import source_hash
import runtime_context as context

TRANSPORT_FILES = ("context_files.py", "context_provision.py", "context_transport.py")
DEPLOYMENT_FIELDS = ("version", "anchor", "host", "port", "service_uid", "service_gid", "ssh_host_key", "ssh_client_key", "ssh_client_sha256", "host_python_sha256")


def public_key(value):
    require(type(value) is str and value.startswith("ssh-ed25519 ") and len(value) <= 128, "CONTEXT_SSH_KEY")
    parts = value.split(" ")
    require(len(parts) == 2, "CONTEXT_SSH_KEY")
    try:
        raw = base64.b64decode(parts[1], validate=True)
    except (ValueError, UnicodeError):
        raise Invalid("CONTEXT_SSH_KEY") from None
    require(len(raw) == 51 and raw[:19] == struct.pack(">I", 11) + b"ssh-ed25519" + struct.pack(">I", 32) and
        raw[19:] != bytes(32) and base64.b64encode(raw).decode() == parts[1], "CONTEXT_SSH_KEY")
    return value


def validate_deployment(value):
    exact(value, DEPLOYMENT_FIELDS)
    integer(value["version"], 1, 1)
    context.validate_anchor(value["anchor"])
    require(type(value["host"]) is str and "%" not in value["host"], "CONTEXT_SSH_HOST")
    try:
        host = ipaddress.ip_address(value["host"])
    except ValueError:
        raise Invalid("CONTEXT_SSH_HOST") from None
    require(str(host) == value["host"] and not host.is_unspecified and not host.is_multicast, "CONTEXT_SSH_HOST")
    integer(value["port"], 1, 65535)
    for name in ("service_uid", "service_gid"):
        require(integer(value[name], 1, 2**31 - 1) != 999, "CONTEXT_SERVICE_IDENTITY")
    public_key(value["ssh_host_key"]); public_key(value["ssh_client_key"])
    digest(value["ssh_client_sha256"]); digest(value["host_python_sha256"])
    require(len(canonical(value)) <= 4096, "CONTEXT_DEPLOYMENT_BOUND")
    return copy.deepcopy(value)


def deployment_digest(value): return sha(canonical(validate_deployment(value)))
def transport_digest(): return source_hash(TRANSPORT_FILES)


def ssh_policy():
    return {"forcecommand": files.FORCED_COMMAND, "authorizedkeysfile": "/etc/obs1/authorized_keys",
        "authenticationmethods": "publickey", "pubkeyauthentication": "yes", "passwordauthentication": "no",
        "kbdinteractiveauthentication": "no", "permittty": "no", "disableforwarding": "yes", "permituserrc": "no"}


def sshd_fragment():
    return ("Match User " + files.SERVICE_USER + "\n" + "".join("    " + key + " " + value + "\n" for key, value in ssh_policy().items()) + "Match all\n").encode()


def validate_sshd_output(raw):
    """Validate an operator-collected sshd -T result; this does not attest a live daemon."""
    require(type(raw) is bytes and 0 < len(raw) <= 65536, "CONTEXT_SSHD_OUTPUT")
    try:
        rows = {}
        for line in raw.decode("ascii").splitlines():
            key, value = line.split(" ", 1)
            require(key not in rows, "CONTEXT_SSHD_DUPLICATE")
            rows[key] = value
    except (UnicodeError, ValueError):
        raise Invalid("CONTEXT_SSHD_OUTPUT") from None
    for key, value in ssh_policy().items():
        require(rows.get(key) == value, "CONTEXT_SSHD_POLICY")
    require(rows.get("permituserenvironment") == "no", "CONTEXT_SSHD_POLICY")
    return {"status": "PASS_POLICY_INPUT", "configuration_sha256": sha(raw), "live_daemon_verified": False}


def host_files(deployment):
    value = validate_deployment(deployment)
    require(value["anchor"]["source_sha256"] == context.source_digest(), "CONTEXT_SOURCE_BINDING")
    root = Path(__file__).resolve().parent
    result = {"code/" + name: (root / name).read_bytes() for name in context.SOURCE_FILES}
    require(sha(canonical({name: sha(result["code/" + name]) for name in context.SOURCE_FILES})) ==
        value["anchor"]["source_sha256"], "CONTEXT_SOURCE_CHANGED")
    result.update({"etc/context-source.json": canonical(value["anchor"]),
        "etc/context-service.json": canonical({"uid": value["service_uid"], "gid": value["service_gid"], "python_sha256": value["host_python_sha256"]}),
        "etc/authorized_keys": ('restrict,command="' + files.FORCED_COMMAND + '" ' + value["ssh_client_key"] + "\n").encode(),
        "sshd_config.fragment": sshd_fragment()})
    require(context.source_digest() == value["anchor"]["source_sha256"], "CONTEXT_SOURCE_CHANGED")
    return result


def write_new(path, raw, mode=0o600):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, mode)
    with os.fdopen(fd, "wb") as stream:
        stream.write(raw); stream.flush(); os.fsync(stream.fileno())


def manifest(value, public_hashes):
    return {"version": 1, "kind": "context_provisioning_bundle", "deployment_sha256": deployment_digest(value),
        "runtime_context_source": context.source_digest(), "runtime_context_transport": transport_digest(),
        "files": public_hashes, "host_installation": {"code_directory": str(files.CODE), "config_directory": str(files.CONFIG),
            "directory_owner": 0, "directory_mode": 0o755, "public_file_owner": 0, "public_file_mode": 0o444,
            "key_owner": value["service_uid"], "key_group": value["service_gid"], "key_mode": 0o600,
            "service_user": files.SERVICE_USER, "sshd_fragment": "/etc/ssh/sshd_config.d/obs1-context.conf"},
        "secret_files": ["host/etc/context-source.key", "controller/context-source.key", "controller/id_ed25519"],
        "secrets_in_manifest": False, "installation_performed": False, "runtime_evidence": False, "execution_authorized": False}


def verify_bundle(root):
    """Recheck a private staging bundle before transfer; never certify host installation."""
    root = Path(root)
    require(root.is_absolute(), "CONTEXT_BUNDLE_PATH")
    uid = os.geteuid()
    def read(parent, name, limit=262144):
        fd = files.directory(parent, uid, 0o700)
        try: return files.read_at(fd, name, uid, (0o600,), limit)
        finally: os.close(fd)
    expected_dirs = {"", "host", "host/code", "host/etc", "controller"}
    for name in expected_dirs:
        fd = files.directory(root / name, uid, 0o700)
        os.close(fd)
    value = validate_deployment(decode(read(root / "controller", "deployment.json", 4096), 4096))
    expected = {"host/" + name: raw for name, raw in host_files(value).items()}
    expected["controller/deployment.json"] = canonical(value)
    for name, raw in expected.items():
        path = root / name
        require(read(path.parent, path.name) == raw, "CONTEXT_BUNDLE_CHANGED")
    host_key = read(root / "host/etc", "context-source.key", 32)
    require(host_key == read(root / "controller", "context-source.key", 32), "CONTEXT_KEY_PAIR")
    context.mac(host_key, b"")
    identity = read(root / "controller", "id_ed25519", 16384)
    require(identity.startswith(b"-----BEGIN OPENSSH PRIVATE KEY-----\n") and
        identity.endswith(b"-----END OPENSSH PRIVATE KEY-----\n"), "CONTEXT_IDENTITY_FORMAT")
    record = manifest(value, {name: sha(raw) for name, raw in expected.items()})
    require(read(root, "manifest.json") == canonical(record), "CONTEXT_BUNDLE_MANIFEST")
    permitted = set(expected) | set(record["secret_files"]) | {"manifest.json"} | expected_dirs - {""}
    require({path.relative_to(root).as_posix() for path in root.rglob("*")} == permitted, "CONTEXT_BUNDLE_INVENTORY")
    return {"status": "PASS_PRIVATE_BUNDLE", "deployment_sha256": deployment_digest(value),
        "manifest_sha256": sha(canonical(record)), "installation_verified": False, "execution_authorized": False}


def create_bundle(destination, deployment, ssh_identity, *, key=None):
    """Create a new private staging bundle. Existing destinations are never replaced."""
    value = validate_deployment(deployment)
    require(type(ssh_identity) is bytes and 0 < len(ssh_identity) <= 16384 and
        ssh_identity.startswith(b"-----BEGIN OPENSSH PRIVATE KEY-----\n") and
        ssh_identity.endswith(b"-----END OPENSSH PRIVATE KEY-----\n"), "CONTEXT_IDENTITY_FORMAT")
    key = secrets.token_bytes(32) if key is None else key
    context.mac(key, b"")
    public = host_files(value)
    destination = Path(destination)
    require(destination.is_absolute(), "CONTEXT_BUNDLE_PATH")
    destination.mkdir(mode=0o700)
    for child in ("host", "host/code", "host/etc", "controller"):
        (destination / child).mkdir(mode=0o700)
    public_hashes = {}
    for name, raw in public.items():
        write_new(destination / "host" / name, raw)
        public_hashes["host/" + name] = sha(raw)
    write_new(destination / "host/etc/context-source.key", key)
    write_new(destination / "controller/deployment.json", canonical(value))
    public_hashes["controller/deployment.json"] = sha(canonical(value))
    write_new(destination / "controller/context-source.key", key)
    write_new(destination / "controller/id_ed25519", ssh_identity)
    # Secret material is only in the three private files; neither values nor
    # secret-file hashes are returned or included in the public manifest.
    record = manifest(value, public_hashes)
    write_new(destination / "manifest.json", canonical(record))
    verify_bundle(destination)
    return record


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--deployment", type=Path)
    parser.add_argument("--identity", type=Path)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--check-bundle", type=Path)
    args = parser.parse_args()
    try:
        if args.check_bundle is not None:
            require(args.deployment is args.identity is args.output is None, "CONTEXT_ARGUMENTS")
            print(canonical(verify_bundle(args.check_bundle)).decode(), end="")
            return 0
        require(all(path is not None for path in (args.deployment, args.identity, args.output)), "CONTEXT_ARGUMENTS")
        values = []
        for path, limit in ((args.deployment, 4096), (args.identity, 16384)):
            fd = files.directory(path.parent, os.geteuid(), 0o700)
            try: values.append(files.read_at(fd, path.name, os.geteuid(), (0o600,), limit))
            finally: os.close(fd)
        record = create_bundle(args.output, decode(values[0], 4096), values[1])
        print(canonical({key: record[key] for key in ("deployment_sha256", "runtime_context_source", "runtime_context_transport", "installation_performed")}).decode(), end="")
        return 0
    except Exception:
        print("CONTEXT_PROVISIONING_REFUSED", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
