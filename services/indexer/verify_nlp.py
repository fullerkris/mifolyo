#!/usr/bin/env python3
"""Independent, offline, stdlib-only source/asset gate. NEVER imports nlp/nltk.

Default mode verifies private-build source/assets; it does NOT approve distribution.
--distribution adds the fail-closed data/image licensing gate. It can be combined
with --source-only before any registry login/push without provisioning corpus data.
Trust anchor: this reviewed verifier and the source tree containing it. This is
not a signature, a pip-audit replacement, or a concurrent-writer sandbox.
"""

import argparse
import ast
import hashlib
import json
from pathlib import Path, PurePosixPath
import shutil
import stat
import sys
import tempfile

SOURCE_COMMIT = "303f6e2ba8e4548a5f54fd65d86bb5c9a949f1db"
DATA_COMMIT = "550b6625bcef1f2abff2ff770a5a0d272c9c6b2a"
DATA_INDEX_SHA256 = "97dce5e72320cd9850b7c20130196006710c18f9c03134c822a37da330198bf6"
# Deliberately outside nlp and not automatically rewritten by the generator.
INVENTORY_SHA256 = "95a22ee374f9aff19c232f49a81cf1449f7ab00d0ff5f6d6946ec21541c084a5"
REQUIRED_ASSETS = {
    "data/punkt_tab/english/collocations.tab",
    "data/punkt_tab/english/sent_starters.txt",
    "data/punkt_tab/english/abbrev_types.txt",
    "data/punkt_tab/english/ortho_context.tab",
    "data/english_stopwords.txt",
}
ADVISORY_IDS = {"PYSEC-2026-3740", "GHSA-8mgp-746c-j5xp", "CVE-2026-81726"}
REMOVED_AFFECTED_APIS = {
    "TransitionParser.train", "TransitionParser.parse", "AveragedPerceptron.save",
    "AveragedPerceptron.load", "PerceptronTagger.save_to_json", "save_maxent_params",
}
# Exact import statements, including names, not a broad stdlib namespace allowlist.
IMPORTS = {
    "__init__.py": {
        "from functools import lru_cache", "from hashlib import sha256",
        "from pathlib import Path", "import stat", "from ._asset_hashes import ASSETS",
        "from ._destructive import NLTKWordTokenizer",
        "from ._punkt import PunktParameters", "from ._punkt import PunktSentenceTokenizer",
    },
    "_asset_hashes.py": set(),
    "_destructive.py": {"import re"},
    "_punkt.py": {
        "import re", "import string", "from collections import defaultdict",
        "from collections.abc import Iterator", "from re import Match",
    },
}
FORBIDDEN_NAMES = {
    "nltk", "pickle", "marshal", "socket", "subprocess", "requests", "urllib",
    "importlib", "ctypes", "__import__", "__builtins__", "__getstate__", "__setstate__",
    "__reduce__", "__reduce_ex__", "eval", "exec", "globals", "locals", "vars",
    "PunktTrainer", "PunktTokenizer", "TransitionParser", "AveragedPerceptron",
    "PerceptronTagger", "FreqDist", "TokenizerI", "TabEncoder", "PunktDecoder",
    "punkt_pickle_load", "allowlisted_pickle_load", "load_punkt_params",
    "save_punkt_params", "save_maxent_params", "load_maxent_params", "open_datafile",
    "train", "parse", "save", "load", "dump", "dumps", "loads", "download",
    "save_to_json", "save_params", "load_lang", "_execute", "system", "popen",
    "debug_decisions", "_debug_ortho_context", "format_debug_decision", "demo",
    "sentences_from_text_legacy", "sentences_from_tokens", "_build_sentence_list",
    "clear_abbrevs", "clear_collocations", "clear_sent_starters", "clear_ortho_context",
    "add_ortho_context", "getenv", "environ", "get_data", "get_source",
}
FILE_CALLS = {
    "open", "read", "read_bytes", "read_text", "write", "write_bytes", "write_text",
    "mkdir", "makedirs", "unlink", "remove", "rename", "replace", "rmdir",
    "touch", "symlink_to", "hardlink_to", "chmod", "chown", "truncate",
}


def sha256(content):
    return hashlib.sha256(content).hexdigest()


def read_regular(path, expected_size=None):
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode):
        raise ValueError(f"Not a regular file (symlinks forbidden): {path}")
    if expected_size is not None and info.st_size != expected_size:
        raise ValueError(f"Unexpected inventoried file size: {path}")
    return path.read_bytes()


def unique_object(pairs):
    obj = {}
    for key, value in pairs:
        if key in obj:
            raise ValueError(f"Duplicate inventory key: {key}")
        obj[key] = value
    return obj


def safe_name(name):
    if not isinstance(name, str) or not name or "\\" in name:
        raise ValueError("Invalid inventory path")
    path = PurePosixPath(name)
    if path.is_absolute() or any(part in {".", "..", ""} for part in name.split("/")):
        raise ValueError(f"Unsafe inventory path: {name}")


def check_python(relative, content):
    """Additional review tripwires, not a general-purpose Python safety proof."""
    errors = []
    if relative not in IMPORTS:
        return [f"Unapproved Python source: {relative}"]
    tree = ast.parse(content, filename=relative)
    seen_imports = set()
    permitted_reads = set()
    if relative == "__init__.py":
        for node in tree.body:
            if isinstance(node, ast.FunctionDef) and node.name == "_verified_asset_lines":
                permitted_reads.update(id(child) for child in ast.walk(node))
    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            seen_imports.update("import " + alias.name for alias in node.names)
        elif isinstance(node, ast.ImportFrom):
            module = "." * node.level + (node.module or "")
            seen_imports.update(f"from {module} import {alias.name}" for alias in node.names)
        identifier = None
        if isinstance(node, ast.Name):
            identifier = node.id
        elif isinstance(node, ast.Attribute):
            identifier = node.attr
        elif isinstance(node, (ast.ClassDef, ast.FunctionDef, ast.AsyncFunctionDef)):
            identifier = node.name
        if identifier in FORBIDDEN_NAMES:
            errors.append(f"{relative}:{node.lineno}: forbidden API/name {identifier}")
        if isinstance(node, ast.Call):
            callee = node.func
            called = callee.id if isinstance(callee, ast.Name) else getattr(callee, "attr", "")
            if isinstance(callee, ast.Name) and called in {"compile", "input", "breakpoint"}:
                errors.append(f"{relative}:{node.lineno}: forbidden dynamic-code/interactive call")
            if called in FILE_CALLS:
                # str.replace is an unchanged inference operation, not file I/O.
                if called == "replace" and relative in {"_punkt.py", "_destructive.py"}:
                    continue
                if not (called == "read_bytes" and id(node) in permitted_reads):
                    errors.append(f"{relative}:{node.lineno}: unapproved file API {called}")
    if seen_imports != IMPORTS[relative]:
        errors.append(f"{relative}: import allowlist mismatch: {sorted(seen_imports ^ IMPORTS[relative])}")
    return errors


def walk_inventory(root):
    """lstat every entry; do not follow symlink files OR directories."""
    files, directories, errors = set(), set(), []
    if not stat.S_ISDIR(root.lstat().st_mode):
        raise ValueError("Component root must be a real directory, not a symlink")
    pending = [root]
    while pending:
        directory = pending.pop()
        for path in directory.iterdir():
            relative = path.relative_to(root).as_posix()
            mode = path.lstat().st_mode
            if stat.S_ISDIR(mode):
                directories.add(relative)
                pending.append(path)
            elif stat.S_ISREG(mode):
                files.add(relative)
            else:
                errors.append(f"Symlink or non-regular inventory entry: {relative}")
    return files, directories, errors


def verify(component_dir=None, *, source_only=False, distribution=False):
    root = Path(component_dir) if component_dir is not None else Path(__file__).absolute().parent / "nlp"
    report = {
        "ok": False, "integrity_ok": False, "private_build_ready": False,
        "distribution_allowed": False, "release_ready": False,
        "distribution_gate_requested": distribution,
        "mode": "source-only" if source_only else "source-and-assets (private build)",
        "errors": [], "warnings": [], "files_checked": 0, "assets_present": 0,
    }
    errors = report["errors"]
    try:
        actual_files, actual_dirs, walk_errors = walk_inventory(root)
        errors.extend(walk_errors)
        content = read_regular(root / "component.json")
        if sha256(content) != INVENTORY_SHA256:
            raise ValueError("component.json SHA-256 differs from independently reviewed verifier anchor")
        inventory = json.loads(content, object_pairs_hook=unique_object)
        if inventory["schema_version"] != 2 or inventory["upstream"] != {
            "nltk_version": "3.10.3", "nltk_commit": SOURCE_COMMIT, "nltk_data_commit": DATA_COMMIT,
        }:
            raise ValueError("Unexpected schema or immutable upstream identity")
        report["upstream"] = inventory["upstream"]
        policy = inventory["data_policy"]
        if policy != {
            "private_provisioning": "approved_by_project_owner",
            "approved_scope": "private build/test provisioning and offline runtime",
            "legal_clearance": False,
            "redistribution_review": "unresolved",
            "corpus_bytes_in_git": False,
            "data_and_image_distribution": "blocked_pending_licensing_review",
        }:
            raise ValueError("Unreviewed private provisioning / distribution policy")
        report["data_policy"] = policy
        report["license_status"] = inventory["license_status"]
        advisory = inventory["advisory_disposition"]
        if set(advisory["ids"]) != ADVISORY_IDS or advisory["status"] != "affected_code_not_present":
            raise ValueError("Missing explicit removed-code advisory disposition")
        if set(advisory["removed_affected_apis"]) != REMOVED_AFFECTED_APIS:
            raise ValueError("Incomplete affected-API removal record")
        for origin in inventory["upstream_inputs"].values():
            repo = origin["repository"].rsplit("/", 1)[-1]
            expected_commit = {"nltk": SOURCE_COMMIT, "nltk_data": DATA_COMMIT}[repo]
            if origin["commit"] != expected_commit or origin["url"] != (
                f"https://raw.githubusercontent.com/nltk/{repo}/{expected_commit}/{origin['path']}"
            ):
                raise ValueError("Non-immutable upstream provenance")
        index_origin = inventory["upstream_inputs"]["data-index.xml"]
        if index_origin["sha256"] != DATA_INDEX_SHA256 or index_origin["path"] != "index.xml":
            raise ValueError("Unexpected pinned public data index identity")
        if inventory["public_data_index"] != {"inventory_file": "provenance/data-index.json", **index_origin}:
            raise ValueError("Public data index inventory missing or altered")
        assets = inventory["required_assets"]
        if set(assets) != REQUIRED_ASSETS or any(a["commit"] != DATA_COMMIT for a in assets.values()):
            raise ValueError("Unexpected fixed English asset identity")
        files = inventory["files"]
        allowed_files = set(files) | REQUIRED_ASSETS | {"component.json"}
        allowed_dirs = set()
        for name in allowed_files:
            safe_name(name)
            allowed_dirs.update(str(p) for p in PurePosixPath(name).parents if str(p) != ".")
        errors.extend(f"Unexpected file: {name}" for name in sorted(actual_files - allowed_files))
        errors.extend(f"Unexpected directory: {name}" for name in sorted(actual_dirs - allowed_dirs))
        errors.extend(f"Missing inventoried file: {name}" for name in sorted(set(files) - actual_files))
        for name, entry in sorted({**files, **assets}.items()):
            if name not in actual_files:
                if name in assets and not source_only:
                    errors.append(f"Missing required bundled asset: {name}")
                continue
            payload = read_regular(root / name, entry["bytes"])
            if len(payload) != entry["bytes"] or sha256(payload) != entry["sha256"]:
                errors.append(f"Size/SHA-256 mismatch: {name}")
            if name.endswith(".py"):
                errors.extend(check_python(name, payload))
            if name == "_asset_hashes.py":
                table = ast.parse(payload).body
                if len(table) != 1 or not isinstance(table[0], ast.Assign):
                    raise ValueError("Unexpected runtime asset table structure")
                expected = {key: {field: value[field] for field in ("bytes", "sha256", "rows")} for key, value in assets.items()}
                if ast.literal_eval(table[0].value) != expected:
                    raise ValueError("Runtime asset allowlist does not match pinned inventory")
            if name == "provenance/data-index.json":
                index = json.loads(payload, object_pairs_hook=unique_object)
                if index["index"] != index_origin or set(index["packages"]) != {"punkt_tab", "stopwords"}:
                    raise ValueError("Incorrect public data index subset")
                for package_id, package in index["packages"].items():
                    origin = inventory["upstream_inputs"][package_id + ".zip"]
                    attributes = package["upstream_attributes"]
                    if (
                        package["immutable_download_url"] != origin["url"]
                        or attributes["id"] != package_id
                        or attributes["sha256_checksum"] != origin["sha256"]
                        or int(attributes["size"]) != origin["bytes"]
                    ):
                        raise ValueError("Public data index / pinned archive disagreement")
            report["files_checked"] += 1
            report["assets_present"] += int(name in assets)
        tool = inventory["maintenance_tool"]
        if tool["path"] != "../tools/vendor_nlp.py":
            raise ValueError("Unapproved maintenance-tool path")
        if (root.parent / "tools").is_symlink():
            raise ValueError("Maintenance-tool directory must not be a symlink")
        tool_bytes = read_regular(root.parent / "tools" / "vendor_nlp.py", tool["bytes"])
        if len(tool_bytes) != tool["bytes"] or sha256(tool_bytes) != tool["sha256"]:
            errors.append("Maintenance generator size/SHA-256 mismatch")
        report["integrity_ok"] = not errors
        report["private_build_ready"] = not source_only and not errors
        license_blocked = (
            inventory["distribution_approved"] is not True
            or inventory["data_license"] == "NOASSERTION"
            or policy["legal_clearance"] is not True
            or policy["redistribution_review"] != "resolved"
        )
        if license_blocked:
            report["warnings"].append(inventory["license_status"])
            if distribution:
                errors.append("DISTRIBUTION BLOCKED: unresolved data rights; project-owner private provisioning approval is NOT permission to publish data/images")
        if source_only:
            report["warnings"].append("Source-only verification does not require absent corpus files; any present assets must still match their inventory.")
        report["warnings"].append("Integrity success is not distribution approval. Run --distribution before registry login/push or publishing data-bearing artifacts.")
        report["warnings"].append("pip-audit does not inspect this source. Keep the independent dependency audit unchanged.")
        report["ok"] = not errors
        report["distribution_allowed"] = distribution and not errors and not license_blocked
        report["release_ready"] = not source_only and report["distribution_allowed"]
    except (OSError, ValueError, TypeError, KeyError, SyntaxError, UnicodeError) as exc:
        errors.append(str(exc))
    return report


def self_test(component_dir, work_dir):
    """Offline mutation tests in an explicitly selected private work directory."""
    source = Path(component_dir)
    work = Path(work_dir)
    if not work.is_dir() or work.is_symlink():
        raise ValueError("Self-test work directory must already exist and not be a symlink")
    if not verify(source, source_only=True)["ok"]:
        raise ValueError("Refusing self-test of an unverified source tree")
    cases = []

    def require_rejection(label, root):
        if verify(root, source_only=True)["ok"]:
            raise ValueError(f"Self-test accepted {label}")
        cases.append(label)

    with tempfile.TemporaryDirectory(prefix="nlp-verifier-", dir=work) as scratch:
        base = Path(scratch)
        (base / "tools").mkdir()
        shutil.copyfile(source.parent / "tools" / "vendor_nlp.py", base / "tools" / "vendor_nlp.py")
        root = base / "nlp"

        def reset():
            if root.is_symlink():
                root.unlink()
            elif root.exists():
                shutil.rmtree(root)
            shutil.copytree(source, root, ignore=shutil.ignore_patterns("data"))

        reset()
        if not verify(root, source_only=True)["ok"] or verify(root)["ok"]:
            raise ValueError("Expected source-only success but missing-data integrity failure")
        cases.append("valid source-only / missing-data integrity failure")
        gated = verify(root, source_only=True, distribution=True)
        if gated["ok"] or not gated["integrity_ok"] or not any("DISTRIBUTION BLOCKED" in e for e in gated["errors"]):
            raise ValueError("Source-only must not bypass the distribution gate")
        cases.append("source-only distribution gate blocks without corpus provisioning")
        if verify(root, distribution=True)["ok"]:
            raise ValueError("Missing-data distribution gate returned success")
        cases.append("default distribution gate blocks missing corpus and unresolved rights")
        (root / "_punkt.py").unlink()
        require_rejection("missing source", root)
        reset()
        path = root / "_punkt.py"
        path.write_bytes(path.read_bytes().replace(b"import re", b"import os", 1))
        require_rejection("same-size source tamper", root)
        reset()
        (root / "unexpected.py").write_text("import pickle\n", encoding="utf-8")
        require_rejection("extra source", root)
        reset()
        (root / "__pycache__").mkdir()
        require_rejection("extra empty/cache directory", root)
        reset()
        (root / "_punkt.py").unlink()
        (root / "_punkt.py").symlink_to(source / "_punkt.py")
        require_rejection("same-content source symlink", root)
        reset()
        (root / "provenance").rename(root / "moved-provenance")
        (root / "provenance").symlink_to(root / "moved-provenance", target_is_directory=True)
        require_rejection("directory symlink", root)
        reset()
        shutil.rmtree(root)
        root.symlink_to(source, target_is_directory=True)
        require_rejection("component-root symlink", root)
        reset()
        (root / "data").mkdir()
        (root / "data" / "english_stopwords.txt").write_bytes(b"incorrect\n")
        require_rejection("tampered optional-in-source-only asset", root)
        reset()
        (root / "data").mkdir()
        (root / "data" / "english_stopwords.txt").symlink_to(source / "NOTICE")
        require_rejection("asset symlink", root)
        reset()
        path = root / "component.json"
        changed = json.loads(path.read_bytes())
        changed["files"].pop("_punkt.py")
        path.write_text(json.dumps(changed), encoding="utf-8")
        require_rejection("inventory rewrite/drop-file attempt", root)
        reset()
        path = root / "component.json"
        changed = json.loads(path.read_bytes())
        changed["data_license"] = "Apache-2.0"
        changed["distribution_approved"] = True
        path.write_text(json.dumps(changed), encoding="utf-8")
        if verify(root, source_only=True, distribution=True)["ok"]:
            raise ValueError("Unreviewed inventory rewrite approved distribution")
        cases.append("forged distribution clearance rejected by external inventory anchor")
        reset()
        (base / "tools" / "vendor_nlp.py").write_bytes(b"# changed\n")
        require_rejection("maintenance-tool tamper", root)
    for text in (
        b"import pickle\npickle.loads(b'x')\n",
        b"import re\nopen('/tmp/model', 'w')\n",
        b"import re\ndef train(text): return text\n",
        b"import re\n__import__('socket')\n",
    ):
        if not check_python("_destructive.py", text):
            raise ValueError("Static source-surface test did not reject forbidden API")
        cases.append("forbidden AST surface")
    for api in sorted(REMOVED_AFFECTED_APIS):
        if "." in api:
            cls, method = api.split(".")
            text = f"import re\nclass {cls}:\n    def {method}(self): pass\n"
        else:
            text = f"import re\ndef {api}(): pass\n"
        if not any("forbidden API/name" in error for error in check_python("_destructive.py", text)):
            raise ValueError(f"Reintroduced advisory API was not rejected: {api}")
        cases.append("reintroduced advisory API: " + api)
    return cases


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--component-dir", type=Path, default=Path(__file__).absolute().parent / "nlp")
    parser.add_argument("--source-only", action="store_true", help="verify source and any present assets; do not require corpus provisioning")
    parser.add_argument("--distribution", action="store_true", help="fail closed on unresolved data/image redistribution rights, including with --source-only")
    parser.add_argument("--json", action="store_true", help="machine-readable report")
    parser.add_argument("--self-test-work-dir", type=Path, help="run offline negative tests in this existing private directory")
    args = parser.parse_args()
    if args.self_test_work_dir is not None:
        if args.distribution:
            parser.error("--self-test-work-dir cannot replace or bypass a --distribution gate")
        try:
            cases = self_test(args.component_dir, args.self_test_work_dir)
        except (OSError, ValueError) as exc:
            print(f"NLP verifier self-test FAIL: {exc}", file=sys.stderr)
            return 1
        print(f"NLP verifier self-test PASS: {len(cases)} cases; not release approval")
        return 0
    report = verify(args.component_dir, source_only=args.source_only, distribution=args.distribution)
    if args.json:
        print(json.dumps(report, indent=2, sort_keys=True))
    else:
        print(f"NLP verification {'PASS' if report['ok'] else 'FAIL'} [{report['mode']}]; private_build_ready={report['private_build_ready']}; distribution_allowed={report['distribution_allowed']}")
        for error in report["errors"]:
            print("ERROR: " + error)
        for warning in report["warnings"]:
            print("NOTE: " + warning)
    return 0 if report["ok"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
