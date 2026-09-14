#!/usr/bin/env python3
"""Explicit private build provisioning / deterministic source maintenance.

No dependency on nltk, no upstream code execution, no generic archive extraction.
Corpus bytes stay OUT of Git. Private build/test provisioning is project-owner
approved, NOT legal clearance; data/image distribution remains blocked.
Manual changes belong here, not in generated source. See ../nlp/README.md.
The optional private-bundle test executes only this component's disposable copy.
"""

import argparse
import ast
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import shutil
import stat
import subprocess
import sys
import tempfile
from urllib.request import HTTPRedirectHandler, build_opener
from xml.etree import ElementTree
import zipfile

SOURCE_COMMIT = "303f6e2ba8e4548a5f54fd65d86bb5c9a949f1db"
DATA_COMMIT = "550b6625bcef1f2abff2ff770a5a0d272c9c6b2a"
INPUTS = {
    "punkt.py": ("nltk", "nltk/tokenize/punkt.py", "b85d1b51d1357d8c56d9739cb3d7975837ad12721c3702fae6facb47eb583b48"),
    "destructive.py": ("nltk", "nltk/tokenize/destructive.py", "7dfe473eccf8430cd03266039297f59bf7a543e2038b36c0a3929b52b89c42f7"),
    "NLTK-LICENSE.txt": ("nltk", "LICENSE.txt", "cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30"),
    "punkt_tab.zip": ("nltk_data", "packages/tokenizers/punkt_tab.zip", "e57f64187974277726a3417ca6f181ec5403676c717672eef6a748a7b20e0106"),
    "stopwords.zip": ("nltk_data", "packages/corpora/stopwords.zip", "48c0e52d8b52546e827f53761fb30300c0ab94f70660d28bd65ba0a86270946b"),
    "punkt_tab.xml": ("nltk_data", "packages/tokenizers/punkt_tab.xml", "d8ad712174bbd31a3c1dc424d92d232919e5453c7e520616affbcce4b66e3401"),
    "stopwords.xml": ("nltk_data", "packages/corpora/stopwords.xml", "25e36f0dbd1307814bda2af5b56c24d06a4d6592d769a6476b70ebb778edab0d"),
    "DATASET-LICENSES.md": ("nltk_data", "DATASET-LICENSES.md", "4285f61badbbdc755811f1f5a61e3dd341a516ff2343490819b019fd8f674ff9"),
    "LICENSE-OVERVIEW.md": ("nltk_data", "LICENSE-OVERVIEW.md", "8115f9aadc98ec981082621265a88a05c8b132c82c458b32d52e541129d61ecf"),
    "DATA-LICENSE": ("nltk_data", "LICENSE", "8d030ab5afc58f0b6a1f4207c12fd9553de6da2294efede65a0c58f9a6495fcc"),
    "data-index.xml": ("nltk_data", "index.xml", "97dce5e72320cd9850b7c20130196006710c18f9c03134c822a37da330198bf6"),
}
DATA_ARCHIVE_SIZES = {"punkt_tab.zip": 4319076, "stopwords.zip": 37733}
DATA_POLICY = {
    "private_provisioning": "approved_by_project_owner",
    "approved_scope": "private build/test provisioning and offline runtime",
    "legal_clearance": False,
    "redistribution_review": "unresolved",
    "corpus_bytes_in_git": False,
    "data_and_image_distribution": "blocked_pending_licensing_review",
}

# Positive selections: new upstream APIs can never be retained accidentally.
PUNKT_MEMBERS = {
    "PunktLanguageVars": [
        "__slots__", "sent_end_chars", "_re_sent_end_chars", "re_boundary_realignment",
        "_re_word_start", "_re_non_word_chars", "_re_multi_char_punct",
        "_word_tokenize_fmt", "_word_tokenizer_re", "word_tokenize",
        "_period_context_fmt", "period_context_re",
    ],
    "PunktParameters": ["__init__"],
    "PunktToken": [
        "_properties", "__slots__", "__init__", "_RE_ELLIPSIS", "_RE_NUMERIC",
        "_RE_INITIAL", "_get_type", "type_no_period", "type_no_sentperiod",
        "first_upper", "first_lower", "is_ellipsis", "is_initial",
    ],
    "PunktBaseClass": [
        "__init__", "_tokenize_words", "_annotate_first_pass", "_first_pass_annotation",
    ],
    "PunktSentenceTokenizer": [
        "tokenize", "span_tokenize", "sentences_from_text", "_get_last_whitespace_index",
        "_match_potential_end_contexts", "_slices_from_text", "_realign_boundaries",
        "text_contains_sentbreak", "_annotate_tokens", "PUNCTUATION",
        "_annotate_second_pass", "_second_pass_annotation", "_ortho_heuristic",
    ],
}
PUNKT_GLOBALS = [
    "_ORTHO_BEG_UC", "_ORTHO_MID_UC", "_ORTHO_UNK_UC", "_ORTHO_BEG_LC",
    "_ORTHO_MID_LC", "_ORTHO_UNK_LC", "_ORTHO_UC", "_ORTHO_LC",
    "REASON_KNOWN_COLLOCATION", "REASON_ABBR_WITH_ORTHOGRAPHIC_HEURISTIC",
    "REASON_ABBR_WITH_SENTENCE_STARTER", "REASON_INITIAL_WITH_ORTHOGRAPHIC_HEURISTIC",
    "REASON_NUMBER_WITH_ORTHOGRAPHIC_HEURISTIC",
    "REASON_INITIAL_WITH_SPECIAL_ORTHOGRAPHIC_HEURISTIC", "_pair_iter",
]
WORD_MEMBERS = {
    "MacIntyreContractions": ["CONTRACTIONS2", "CONTRACTIONS3"],
    "NLTKWordTokenizer": [
        "STARTING_QUOTES", "ENDING_QUOTES", "PUNCTUATION", "PARENS_BRACKETS",
        "DOUBLE_DASHES", "_contractions", "CONTRACTIONS2", "CONTRACTIONS3", "tokenize",
    ],
}
ASSET_MEMBERS = {
    "data/punkt_tab/english/collocations.tab": ("punkt_tab.zip", "punkt_tab/english/collocations.tab"),
    "data/punkt_tab/english/sent_starters.txt": ("punkt_tab.zip", "punkt_tab/english/sent_starters.txt"),
    "data/punkt_tab/english/abbrev_types.txt": ("punkt_tab.zip", "punkt_tab/english/abbrev_types.txt"),
    "data/punkt_tab/english/ortho_context.tab": ("punkt_tab.zip", "punkt_tab/english/ortho_context.tab"),
    "data/english_stopwords.txt": ("stopwords.zip", "stopwords/english"),
}
# Exact plaintext identities, independently checked against the pinned archives.
ASSET_IDENTITIES = {
    "data/punkt_tab/english/collocations.tab": (594, "8e2da1225e4dd2cc9dba261ee231ccb134859e21b46006e7f472c5ee269af0cf", 37),
    "data/punkt_tab/english/sent_starters.txt": (241, "f3f8535483e1dba487241b764945168123bca3209a9645e59acd1225dc76edac", 39),
    "data/punkt_tab/english/abbrev_types.txt": (619, "92a3e070f43d9b4c5534758ca40ad7343b04e7e29bfe0c2eb658a39445a4f779", 156),
    "data/punkt_tab/english/ortho_context.tab": (236303, "4bbcca25ed3d3f06c02402abf8419b9f033b8adc06e7b482eca4e45f81a5dc4c", 20366),
    "data/english_stopwords.txt": (1048, "f6d005956f407dbc6ea32e5ff0c7e8e6f71488d3239b9023efdc7fc139d6375b", 198),
}
ADAPTATIONS = [
    "Preserve exact upstream file legal headers; add prominent local modification notice.",
    "Select only listed AST nodes using original source slices, including decorators; no AST unparse or regex rewrites.",
    "Replace broad module/class overview docstrings with inference-only descriptions; remove unused standalone attribute docstrings.",
    "Replace imports with only re/string/defaultdict/Iterator/Match; remove TokenizerI base classes.",
    "PunktBaseClass.__init__(params) sets only _params, default PunktLanguageVars and PunktToken; remove training-capable sentence constructor.",
    "NLTKWordTokenizer.tokenize accepts only text; remove return_str warning and convert_parentheses branches/constant and replace their docstring; default executable statements unchanged.",
    "Remove dead commented-out dump, optional-parenthesis and CONTRACTIONS4 code from retained function bodies.",
    "Keep second-pass REASON constants still referenced by unchanged inference return statements; remove the unused debug default reason.",
    "Local wrapper implements fixed, checksum-verified UTF-8/LF tables instead of nltk.data/tabdata; no arbitrary-file loader retained.",
    "Local orthographic dict.__missing__ returns zero without inserting user words; observationally identical read-only inference lookups to defaultdict(int).",
]
REMOVED = {
    "punkt.py": [
        "_PUNKT_ALLOWED_GLOBALS", "punkt_pickle_load", "PunktTrainer (entire class)",
        "PunktLanguageVars.__getstate__/__setstate__/internal_punctuation",
        "PunktParameters.clear_*/add_ortho_context/_debug_ortho_context",
        "PunktToken.first_case/is_number/is_alpha/is_non_punct/__repr__/__str__ and unused regexes",
        "_ORTHO_MAP", "_re_non_punct", "REASON_DEFAULT_DECISION",
        "PunktSentenceTokenizer.__init__/train/debug_decisions/sentences_from_text_legacy/sentences_from_tokens/_build_sentence_list/dump",
        "PunktTokenizer (entire class)", "load_punkt_params", "save_punkt_params",
        "DEBUG_DECISION_FMT", "format_debug_decision", "demo",
    ],
    "destructive.py": [
        "TokenizerI", "align_tokens", "warnings", "unused typing imports",
        "MacIntyreContractions.CONTRACTIONS4", "NLTKWordTokenizer.span_tokenize",
        "NLTKWordTokenizer.CONVERT_PARENTHESES", "convert_parentheses/return_str optional branches",
    ],
    "not_vendored": [
        "All other NLTK modules, including data/downloader/pathsec/picklesec, parsing, tagging, classification, probability, trainers, tab encoders/decoders and persistence frameworks.",
    ],
    "rationale": "Not reachable from default fixed-English inference; remove model-controlled/caller-selected file I/O, deserialization, training and unrelated APIs rather than merely hiding exports.",
}


def digest(data):
    return hashlib.sha256(data).hexdigest()


def metadata(data):
    return {"bytes": len(data), "sha256": digest(data)}


def read_regular(path, expected_size=None):
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode):
        raise ValueError(f"Expected a regular file, not symlink: {path}")
    if expected_size is not None and info.st_size != expected_size:
        raise ValueError(f"Unexpected file size: {path}")
    return path.read_bytes()


def check_cache_location(cache, root):
    if not cache.is_dir() or cache.is_symlink():
        raise ValueError("--cache must be an existing, non-symlink directory outside the workspace")
    workspace = next((p for p in root.parents if (p / ".git").exists()), root.parent)
    if cache.resolve().is_relative_to(workspace.resolve()):
        raise ValueError("Archive cache must be outside the workspace/build source tree")


class _NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise ValueError("Refusing redirect away from the literal pinned input URL")


def acquire(cache, fetch, names=None):
    if not cache.is_dir() or cache.is_symlink():
        raise ValueError("--cache must be an existing, non-symlink directory")
    result, origins = {}, {}
    for name in INPUTS if names is None else names:
        repo, upstream_path, expected = INPUTS[name]
        commit = SOURCE_COMMIT if repo == "nltk" else DATA_COMMIT
        url = f"https://raw.githubusercontent.com/nltk/{repo}/{commit}/{upstream_path}"
        target = cache / name
        expected_size = DATA_ARCHIVE_SIZES.get(name)
        if not target.exists() and not target.is_symlink():
            if not fetch:
                raise ValueError(f"Missing cache input {name}; network requires explicit --fetch")
            with build_opener(_NoRedirect()).open(url, timeout=30) as response:
                if response.geturl() != url:
                    raise ValueError(f"Unexpected download redirect: {name}")
                data = response.read((expected_size or 8 * 1024 * 1024) + 1)
            if (expected_size is not None and len(data) != expected_size) or digest(data) != expected:
                raise ValueError(f"Upstream size/SHA-256 mismatch: {name}")
            with target.open("xb") as output:
                output.write(data)
        data = read_regular(target, expected_size)
        if (expected_size is not None and len(data) != expected_size) or digest(data) != expected:
            raise ValueError(f"Cached upstream size/SHA-256 mismatch: {name}")
        result[name] = data
        origins[name] = {
            "repository": f"https://github.com/nltk/{repo}", "commit": commit,
            "path": upstream_path, "url": url, **metadata(data),
            "git_blob_sha1": hashlib.sha1(
                b"blob " + str(len(data)).encode("ascii") + b"\0" + data
            ).hexdigest(),
        }
    return result, origins


def node_name(node):
    if isinstance(node, (ast.ClassDef, ast.FunctionDef)):
        return node.name
    if isinstance(node, ast.Assign) and len(node.targets) == 1:
        if isinstance(node.targets[0], ast.Name):
            return node.targets[0].id
    return None


def source_slice(lines, node):
    start = min([node.lineno] + [d.lineno for d in getattr(node, "decorator_list", [])])
    return "".join(lines[start - 1:node.end_lineno])


def word_tokenize_default(lines, node):
    body = node.body
    if not isinstance(body[0], ast.Expr) or not isinstance(body[0].value, ast.Constant):
        raise ValueError("Expected upstream tokenize docstring")
    removed = [
        child for child in body
        if isinstance(child, ast.If) and isinstance(child.test, ast.Name)
        and child.test.id in {"return_str", "convert_parentheses"}
    ]
    if len(removed) != 2:
        raise ValueError("Unexpected upstream default-tokenize branches")
    # Preserve every default executable statement and the intervening comments.
    skip = {line for child in removed for line in range(child.lineno, child.end_lineno + 1)}
    result = (
        "    def tokenize(self, text: str) -> list[str]:\n"
        '        """Tokenize a sentence using only the upstream default options."""\n'
        + "".join(lines[i - 1] for i in range(body[0].end_lineno + 1, node.end_lineno + 1) if i not in skip)
    )
    return result.replace("        # Optionally convert parentheses\n", "").replace(
        "        # We are not using CONTRACTIONS4 since\n"
        "        # they are also commented out in the SED scripts\n"
        "        # for regexp in self._contractions.CONTRACTIONS4:\n"
        "        #     text = regexp.sub(r' \\1 \\2 \\3 ', text)\n\n", ""
    )


def extract(data, filename, selections, globals_to_keep, imports):
    text = data.decode("utf-8")
    lines = text.splitlines(keepends=True)
    tree = ast.parse(text)
    nodes = {node_name(n): n for n in tree.body if node_name(n)}
    output = []
    # Exact complete upstream legal header, terminated by first blank line.
    for line in lines:
        if not line.strip():
            break
        output.append(line)
    output.extend([
        "\n# MODIFIED by mifolyo Indexer maintainers: inference-only source extraction.\n",
        f"# Upstream {filename}, v3.10.3, commit {SOURCE_COMMIT}.\n",
        "# Generated by ../tools/vendor_nlp.py; see README.md, NOTICE and component.json.\n",
        "# License copy: licenses/LICENSE.NLTK. Do not edit this generated file.\n\n",
        imports + "\n\n",
    ])
    for name in globals_to_keep:
        output.append(source_slice(lines, nodes[name]) + "\n\n")
    for class_name, members in selections.items():
        original = nodes[class_name]
        children = {node_name(n): n for n in original.body if node_name(n)}
        base = "(PunktBaseClass)" if class_name == "PunktSentenceTokenizer" else ""
        output.append(f"class {class_name}{base}:\n")
        if class_name == "MacIntyreContractions":
            output.append(source_slice(lines, original.body[0]) + "\n")
        else:
            output.append('    """Private, inference-only subset; see upstream ancestry in component.json."""\n\n')
        for name in members:
            child = children[name]  # Fail if upstream source/selection no longer agrees.
            if class_name == "PunktBaseClass" and name == "__init__":
                output.append(
                    "    def __init__(self, params):\n"
                    "        self._params = params\n"
                    "        self._lang_vars = PunktLanguageVars()\n"
                    "        self._Token = PunktToken\n"
                )
            elif class_name == "NLTKWordTokenizer" and name == "tokenize":
                output.append(word_tokenize_default(lines, child))
            elif class_name == "PunktSentenceTokenizer" and name == "_annotate_tokens":
                output.append(source_slice(lines, child).replace(
                    "        ## [XX] TESTING\n        # tokens = list(tokens)\n        # self.dump(tokens)\n\n", ""
                ))
            else:
                output.append(source_slice(lines, child))
            output.append("\n")
        output.append("\n")
    result = "".join(output).rstrip() + "\n"
    extracted = ast.parse(result)  # Validation without import/exec of upstream code.
    extracted_nodes = {node_name(n): n for n in extracted.body if node_name(n)}
    for name in globals_to_keep:
        if ast.dump(nodes[name]) != ast.dump(extracted_nodes[name]):
            raise ValueError(f"Changed upstream inference constant/function: {name}")
    for class_name, members in selections.items():
        original_members = {node_name(n): n for n in nodes[class_name].body if node_name(n)}
        extracted_members = {node_name(n): n for n in extracted_nodes[class_name].body if node_name(n)}
        if set(extracted_members) != set(members):
            raise ValueError(f"Unexpected retained member set: {class_name}")
        for name in members:
            if class_name == "PunktBaseClass" and name == "__init__":
                continue  # Explicit fixed-English constructor adaptation, above.
            original_node, extracted_node = original_members[name], extracted_members[name]
            if class_name == "NLTKWordTokenizer" and name == "tokenize":
                original_body = [
                    n for n in original_node.body[1:]
                    if not (isinstance(n, ast.If) and isinstance(n.test, ast.Name)
                            and n.test.id in {"return_str", "convert_parentheses"})
                ]
                if [ast.dump(n) for n in original_body] != [ast.dump(n) for n in extracted_node.body[1:]]:
                    raise ValueError("Changed upstream default word-tokenization statements")
            elif ast.dump(original_node) != ast.dump(extracted_node):
                raise ValueError(f"Changed upstream inference member: {class_name}.{name}")
    return result.encode("utf-8")


def asset_payloads(inputs):
    # Authenticate BOTH complete archives before constructing any ZIP reader.
    for name, size in DATA_ARCHIVE_SIZES.items():
        if len(inputs[name]) != size or digest(inputs[name]) != INPUTS[name][2]:
            raise ValueError(f"Archive size/SHA-256 mismatch before ZIP parsing: {name}")
    payloads = {}
    for local, (archive_name, member) in ASSET_MEMBERS.items():
        size, expected_hash, expected_rows = ASSET_IDENTITIES[local]
        with zipfile.ZipFile(io.BytesIO(inputs[archive_name])) as archive:
            entries = [info for info in archive.infolist() if info.filename == member]
            if len(entries) != 1 or entries[0].file_size != size:
                raise ValueError(f"Unexpected archive member: {member}")
            mode = stat.S_IFMT(entries[0].external_attr >> 16)
            if entries[0].is_dir() or mode not in (0, stat.S_IFREG):
                raise ValueError(f"Non-regular/symlinked archive member: {member}")
            data = archive.read(entries[0])
        if len(data) != size or digest(data) != expected_hash:
            raise ValueError(f"Plaintext size/SHA-256 mismatch: {member}")
        rows = data.decode("utf-8").removesuffix("\n").split("\n")
        if b"\r" in data or len(rows) != expected_rows or any(not row for row in rows):
            raise ValueError(f"Unexpected newline/empty record: {member}")
        payloads[local] = data
    return payloads


def asset_inventory(inputs):
    result = {}
    for local, data in asset_payloads(inputs).items():
        archive_name, member = ASSET_MEMBERS[local]
        result[local] = {
            **metadata(data), "rows": ASSET_IDENTITIES[local][2], "archive": archive_name,
            "member": member, "commit": DATA_COMMIT,
            "license": "NOASSERTION", "checked_into_git": False,
            "provisioning": "explicit_private_build_or_test_only",
            "redistribution": "blocked_pending_licensing_review",
        }
    return result


def public_data_index(inputs, origins):
    # Only the two relevant metadata records are retained, not corpus bytes.
    # acquire() has authenticated this immutable index before XML parsing.
    index = ElementTree.fromstring(inputs["data-index.xml"])
    selected = {}
    for name, size in DATA_ARCHIVE_SIZES.items():
        package_id = name.removesuffix(".zip")
        entries = index.findall(f"./packages/package[@id='{package_id}']")
        if len(entries) != 1:
            raise ValueError(f"Expected one pinned public-index record: {package_id}")
        attributes = dict(entries[0].attrib)
        if int(attributes["size"]) != size or attributes["sha256_checksum"] != INPUTS[name][2]:
            raise ValueError(f"Public index disagrees with pinned archive identity: {name}")
        selected[package_id] = {
            "upstream_attributes": attributes,
            "immutable_download_url": origins[name]["url"],
            "note": "The index's advertised gh-pages URL is recorded as provenance only; it is NEVER fetched.",
        }
    return {"index": origins["data-index.xml"], "packages": selected}


def source_verifier(root):
    spec = importlib.util.spec_from_file_location("_nlp_build_verifier", root.parent / "verify_nlp.py")
    verifier = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(verifier)
    report = verifier.verify(root, source_only=True)
    if not report["ok"]:
        raise ValueError("Source/data preflight failed: " + "; ".join(report["errors"]))
    return verifier


def provision_data(root, inputs):
    """Write only the five literal plaintext paths; no source regeneration.

    Validate all inputs and existing outputs before writes. Existing correct files
    are left untouched; stale/tampered files are never silently overwritten.
    Interrupted partial output fails the normal verifier until repaired/reviewed.
    This is not a concurrent-filesystem-writer sandbox.
    """
    verifier = source_verifier(root)
    payloads = asset_payloads(inputs)
    missing = []
    for relative, data in payloads.items():
        target = root / relative
        if target.exists() or target.is_symlink():
            if read_regular(target, len(data)) != data:
                raise ValueError(f"Refusing stale/tampered existing asset: {relative}")
        else:
            missing.append(relative)
    # Recheck directory modes rather than following an existing symlink via mkdir.
    for relative in ("data", "data/punkt_tab", "data/punkt_tab/english"):
        directory = root / relative
        if directory.exists() or directory.is_symlink():
            if not stat.S_ISDIR(directory.lstat().st_mode):
                raise ValueError(f"Refusing non-directory/symlink output: {relative}")
        else:
            directory.mkdir()
    for relative in missing:
        with (root / relative).open("xb") as output:
            output.write(payloads[relative])
    report = verifier.verify(root)
    if not report["ok"]:
        raise ValueError("Provisioned integrity check failed: " + "; ".join(report["errors"]))
    return len(missing)


def generate(inputs, origins, root):
    assets = asset_inventory(inputs)
    runtime_hashes = {name: {key: info[key] for key in ("bytes", "sha256", "rows")} for name, info in sorted(assets.items())}
    generated = {
        "_punkt.py": extract(
            inputs["punkt.py"], "nltk/tokenize/punkt.py", PUNKT_MEMBERS, PUNKT_GLOBALS,
            "import re\nimport string\nfrom collections import defaultdict\nfrom collections.abc import Iterator\nfrom re import Match",
        ),
        "_destructive.py": extract(
            inputs["destructive.py"], "nltk/tokenize/destructive.py", WORD_MEMBERS, [], "import re",
        ),
        "_asset_hashes.py": (
            "# Generated by ../tools/vendor_nlp.py; private build data ONLY, never checked into Git.\n"
            f"# nltk_data commit {DATA_COMMIT}. SHA-256 is over exact unmodified bytes.\n"
            "ASSETS = " + json.dumps(runtime_hashes, indent=4, sort_keys=True) + "\n"
        ).encode("utf-8"),
        "licenses/LICENSE.NLTK": inputs["NLTK-LICENSE.txt"],
        "licenses/LICENSE.nltk_data": inputs["DATA-LICENSE"],
        "provenance/DATASET-LICENSES.md": inputs["DATASET-LICENSES.md"],
        "provenance/LICENSE-OVERVIEW.md": inputs["LICENSE-OVERVIEW.md"],
        "provenance/punkt_tab.xml": inputs["punkt_tab.xml"],
        "provenance/stopwords.xml": inputs["stopwords.xml"],
        "provenance/data-index.json": (json.dumps(public_data_index(inputs, origins), indent=2, sort_keys=True) + "\n").encode("utf-8"),
    }
    inventory = {name: metadata(content) for name, content in generated.items()}
    for manual in ("__init__.py", "NOTICE", "README.md"):
        inventory[manual] = metadata(read_regular(root / manual))
    component = {
        "schema_version": 2, "name": "mifolyo-indexer-english-inference",
        "kind": "in-repository source subset; NOT a PyPI package or full nltk",
        "upstream": {"nltk_version": "3.10.3", "nltk_commit": SOURCE_COMMIT, "nltk_data_commit": DATA_COMMIT},
        "license_status": "UNRESOLVED: punkt_tab and stopwords redistribution rights; private provisioning is project-owner approved, NOT legal clearance",
        "source_license": "Apache-2.0", "data_license": "NOASSERTION", "distribution_approved": False,
        "data_policy": DATA_POLICY,
        "public_data_index": {"inventory_file": "provenance/data-index.json", **origins["data-index.xml"]},
        "upstream_inputs": origins,
        "retained": {"punkt.py": {"globals": PUNKT_GLOBALS, "classes": PUNKT_MEMBERS}, "destructive.py": {"classes": WORD_MEMBERS}},
        "adaptations": ADAPTATIONS, "removed": REMOVED,
        "required_assets": assets,
        "advisory_disposition": {
            "ids": ["PYSEC-2026-3740", "GHSA-8mgp-746c-j5xp", "CVE-2026-81726"],
            "reference": "https://github.com/nltk/nltk/security/advisories/GHSA-8mgp-746c-j5xp",
            "status": "affected_code_not_present",
            "removed_affected_apis": ["TransitionParser.train", "TransitionParser.parse", "AveragedPerceptron.save", "AveragedPerceptron.load", "PerceptronTagger.save_to_json", "save_maxent_params"],
            "rationale": "Their complete parser/tagger/classifier modules are absent. Adjacent Punkt model file load/save/dump and legacy pickle APIs also absent. Only fixed bundled paths are read by local wrapper.",
            "limitation": "Not an upstream patch or blanket vulnerability-free claim. pip-audit does not scan this source; run unchanged dependency audit AND independent source verifier.",
        },
        "maintenance": {
            "owner": "mifolyo Indexer maintainers",
            "obligations": [
                "Project owner approved private build/test provisioning only, not legal clearance. Keep corpus bytes OUT of Git and data-bearing images/artifacts private.",
                "Obtain documented redistribution rights for the exact English model and augmented stopwords before distribution; require --distribution before registry login/push. Never infer dataset licenses from source license.",
                "Monitor upstream inference security advisories and releases; re-review this retained call graph and wrapper on changes.",
                "Keep dependency audit unchanged; separately run the fail-closed source/asset verifier.",
                "Regenerate deterministically from immutable pins, review diffs, update verifier anchor only after review.",
                "Require independent offline reference parity on supported Python versions, including caller filtering/chunking, Unicode, demographic names/pronouns/dialects and adversarial input.",
                "No runtime network, dynamic model paths, training, persistence, text logging, or cache growth from unseen input words.",
            ],
        },
        "maintenance_tool": {"path": "../tools/vendor_nlp.py", **metadata(read_regular(Path(__file__)))},
        "files": dict(sorted(inventory.items())),
        "inventory_anchor": "component.json SHA-256 is pinned independently in ../verify_nlp.py; the inventory does not self-hash.",
    }
    generated["component.json"] = (json.dumps(component, indent=2, sort_keys=True) + "\n").encode("utf-8")
    return generated


def test_private_bundle(root, cache, inputs):
    """Synthetic component tests, NOT independent reference/application goldens.

    Use only a disposable private copy, not the in-repo data directory. This
    exercises the actual fixed loader without adding a resource-path option or
    mocking checksum validation. No upstream NLTK modules are imported.
    """
    allowed_test_commands = set()

    def deny_network(event, args):
        if event == "subprocess.Popen" and tuple(args[1]) in allowed_test_commands:
            return  # One literal offline provisioning CLI in a disposable copy.
        if event.startswith("socket.") or event in {"urllib.Request", "subprocess.Popen", "os.system"}:
            raise RuntimeError("Private component test attempted network or process execution")

    sys.addaudithook(deny_network)
    verifier = source_verifier(root)
    if set(inputs) != set(DATA_ARCHIVE_SIZES):
        raise ValueError("Private setup must require only the two data archives")
    cases = 0
    alias = "_nlp_private_probe"
    previous_bytecode = sys.dont_write_bytecode
    sys.dont_write_bytecode = True
    try:
        with tempfile.TemporaryDirectory(prefix="nlp-private-test-", dir=cache) as scratch, tempfile.TemporaryDirectory(prefix="nlp-cli-cache-", dir=cache) as cli_cache:
            base = Path(scratch)
            target = base / "nlp"
            shutil.copytree(root, target, ignore=shutil.ignore_patterns("data"))
            (base / "tools").mkdir()
            shutil.copyfile(Path(__file__), base / "tools" / "vendor_nlp.py")
            shutil.copyfile(root.parent / "verify_nlp.py", base / "verify_nlp.py")
            for name, payload in inputs.items():
                (Path(cli_cache) / name).write_bytes(payload)
            command = (
                sys.executable, "-I", "-S", "-B", str(base / "tools" / "vendor_nlp.py"),
                "--cache", cli_cache, "--provision-data",
            )
            # Deliberately no --fetch and no NLTK source/index cache inputs: this
            # tests the parent-facing CLI without leaving any in-repo corpus.
            allowed_test_commands.add(command)
            completed = subprocess.run(command, check=True, capture_output=True, text=True, timeout=60)
            allowed_test_commands.remove(command)
            if "5 new / 0 already verified" not in completed.stdout or not verifier.verify(target)["ok"]:
                raise ValueError("Provisioning CLI did not install exactly five verified assets")
            cases += 1
            timestamps = {name: (target / name).stat().st_mtime_ns for name in ASSET_MEMBERS}
            if provision_data(target, inputs) != 0 or any(
                (target / name).stat().st_mtime_ns != timestamp for name, timestamp in timestamps.items()
            ):
                raise ValueError("Correct existing assets must be left untouched")
            cases += 1
            spec = importlib.util.spec_from_file_location(alias, target / "__init__.py", submodule_search_locations=[str(target)])
            component = importlib.util.module_from_spec(spec)
            sys.modules[alias] = component
            spec.loader.exec_module(component)
            samples = [
                ("", []), (" \t\r\n", []), (".", ["."]),
                ("Hello world.", ["Hello", "world", "."]),
                ("Mr. Smith left. She stayed.", ["Mr.", "Smith", "left", ".", "She", "stayed", "."]),
                ("can't cannot gonna wanna ", ["ca", "n't", "can", "not", "gon", "na", "wan", "na"]),
                ("“Hello.” Next.", ["“", "Hello", ".", "”", "Next", "."]),
                ('He said, "Hello." Next.', ["He", "said", ",", "``", "Hello", ".", "''", "Next", "."]),
                ("café 東京 naïve.", ["café", "東京", "naïve", "."]),
                ("x" * 10001 + ". End.", ["x" * 10001, ".", "End", "."]),
            ]
            for text, expected in samples:
                if component.word_tokenize(text) != expected:
                    raise ValueError("Synthetic tokenization mismatch")
                cases += 1
            # Synthetic name/orthography and gender-pronoun probes across groups;
            # no inference about real people's identities or full fairness claim.
            for name in ("Emma", "Jamal", "José", "Wei", "Ravi", "Aisha", "Nguyễn"):
                for pronoun in ("He", "She", "They"):
                    expected = [name, "arrived", ".", pronoun, "left", "."]
                    if component.word_tokenize(f"{name} arrived. {pronoun} left.") != expected:
                        raise ValueError("Synthetic name/pronoun tokenization disparity")
                    cases += 1
            stopwords = component.english_stopwords()
            with zipfile.ZipFile(io.BytesIO(inputs["stopwords.zip"])) as archive:
                expected_stopwords = frozenset(archive.read("stopwords/english").decode("utf-8").splitlines())
            if not isinstance(stopwords, frozenset) or stopwords != expected_stopwords or len(stopwords) != 198:
                raise ValueError("Stopword set mismatch")
            cases += 1
            orthography = component._bundle()[0]._params.ortho_context
            before = len(orthography)
            component.word_tokenize("PrivateNovelNamexyz. UnseenInputTokenabc left.")
            if len(orthography) != before:
                raise ValueError("Inference retained new input words in model cache")
            cases += 1

            def require_data_failure():
                nonlocal cases
                component._bundle.cache_clear()
                for call in (lambda: component.word_tokenize(""), component.english_stopwords):
                    try:
                        call()
                    except component.NLPDataError:
                        cases += 1
                    else:
                        raise ValueError("Missing/corrupt/extra/symlinked data returned success")

            def require_provision_failure():
                nonlocal cases
                try:
                    provision_data(target, inputs)
                except ValueError:
                    cases += 1
                else:
                    raise ValueError("Provisioning silently repaired/accepted stale or unsafe files")

            for local in ASSET_MEMBERS:
                path = target / local
                payload = path.read_bytes()
                path.unlink()
                require_data_failure()
                path.write_bytes(bytes([payload[0] ^ 1]) + payload[1:])
                require_data_failure()
                require_provision_failure()
                path.unlink()
                path.symlink_to(target / "NOTICE")
                require_data_failure()
                require_provision_failure()
                path.unlink()
                path.write_bytes(payload)
            extra = target / "data" / "extra.txt"
            extra.write_bytes(b"unexpected\n")
            require_data_failure()
            require_provision_failure()
            extra.unlink()
            data = target / "data"
            data.rename(base / "moved-data")
            data.symlink_to(base / "moved-data", target_is_directory=True)
            require_data_failure()
            require_provision_failure()
            data.unlink()
            (base / "moved-data").rename(data)
            for source_only in (False, True):
                checked = verifier.verify(target, source_only=source_only)
                gated = verifier.verify(target, source_only=source_only, distribution=True)
                if (
                    not checked["ok"] or checked["private_build_ready"] != (not source_only)
                    or checked["distribution_allowed"] or gated["ok"] or not gated["integrity_ok"]
                ):
                    raise ValueError("Private integrity success must not override the distribution gate")
                cases += 1

            # Offline cache failures must never fall back to the network, even
            # when --fetch is set but an existing cached file is invalid.
            probe_cache = base / "archive-probes"
            probe_cache.mkdir()

            def require_cache_failure(fetch=False):
                nonlocal cases
                try:
                    acquire(probe_cache, fetch, DATA_ARCHIVE_SIZES)
                except ValueError:
                    cases += 1
                else:
                    raise ValueError("Accepted missing/corrupt/symlinked archive cache")

            require_cache_failure()
            for name, payload in inputs.items():
                (probe_cache / name).write_bytes(payload)
            acquired, _ = acquire(probe_cache, False, DATA_ARCHIVE_SIZES)
            if set(acquired) != set(DATA_ARCHIVE_SIZES):
                raise ValueError("Provisioning acquired non-data/source inputs")
            cases += 1
            for name, payload in inputs.items():
                path = probe_cache / name
                path.write_bytes(payload[:-1])
                require_cache_failure(fetch=True)
                path.write_bytes(bytes([payload[0] ^ 1]) + payload[1:])
                require_cache_failure(fetch=True)
                path.unlink()
                path.symlink_to(cache / name)
                require_cache_failure(fetch=True)
                path.unlink()
                path.write_bytes(payload)
            original_zip_reader = zipfile.ZipFile
            try:
                def reject_zip_reader(*args, **kwargs):
                    raise AssertionError("ZIP parsing attempted before archive authentication")

                zipfile.ZipFile = reject_zip_reader
                for name, payload in inputs.items():
                    for invalid in (payload[:-1], bytes([payload[0] ^ 1]) + payload[1:]):
                        try:
                            asset_payloads({**inputs, name: invalid})
                        except ValueError:
                            cases += 1
                        else:
                            raise ValueError("Unauthenticated archive was accepted")
            finally:
                zipfile.ZipFile = original_zip_reader
            try:
                _NoRedirect().redirect_request(None, None, 302, "", {}, "https://invalid.example/")
            except ValueError:
                cases += 1
            else:
                raise ValueError("Provisioning accepted a redirect to an unpinned URL")
            if "nltk" in sys.modules:
                raise ValueError("Unexpected nltk import during component-only test")
    finally:
        sys.dont_write_bytecode = previous_bytecode
        for name in list(sys.modules):
            if name == alias or name.startswith(alias + "."):
                del sys.modules[name]
    return cases


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--cache", type=Path, required=True, help="existing private archive cache OUTSIDE the workspace/build source tree")
    parser.add_argument("--fetch", action="store_true", help="explicitly permit pinned HTTPS input downloads")
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--check", action="store_true", help="compare generated bytes without writing")
    mode.add_argument("--write", action="store_true", help="write only named generated source/provenance files")
    mode.add_argument("--test-private-bundle", action="store_true", help="offline synthetic tests with disposable private cached assets; NOT redistribution")
    mode.add_argument("--provision-data", action="store_true", help="provision ONLY five fixed plaintext assets for PRIVATE build/test use, never Git/distribution")
    args = parser.parse_args()
    try:
        root = Path(__file__).absolute().parents[1] / "nlp"
        if not root.is_dir() or root.is_symlink():
            raise ValueError("Expected existing non-symlink nlp package directory")
        check_cache_location(args.cache, root)
        private_setup = args.provision_data or args.test_private_bundle
        if private_setup:
            source_verifier(root)  # Fail on stale/tampered output before any fetch.
        inputs, origins = acquire(args.cache, args.fetch, DATA_ARCHIVE_SIZES if private_setup else None)
        if args.provision_data:
            written = provision_data(root, inputs)
            print(f"Private source/asset integrity PASS: {written} new / {5 - written} already verified plaintext files")
            print("Project-owner private provisioning approval is NOT legal clearance. Keep corpus bytes OUT of Git; data/image DISTRIBUTION BLOCKED pending licensing review.")
            return 0
        if args.test_private_bundle:
            cases = test_private_bundle(root, args.cache, inputs)
            print(f"PASS: {cases} component-only synthetic/private-bundle/provisioning checks; no assets added to repository; DISTRIBUTION BLOCKED")
            return 0
        generated = generate(inputs, origins, root)
        for relative, data in generated.items():
            target = root / relative
            if args.check:
                if read_regular(target) != data:
                    raise ValueError(f"Reproducibility mismatch: {relative}")
            else:
                if target.parent.is_symlink() or target.is_symlink():
                    raise ValueError(f"Refusing symlink output: {relative}")
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(data)
        print(f"{'Verified' if args.check else 'Generated'} {len(generated)} deterministic source/provenance files; private provisioning approved / DISTRIBUTION BLOCKED")
        print("Inventory SHA-256 (review before manually updating verifier): " + digest(generated["component.json"]))
        return 0
    except (OSError, ValueError, KeyError, SyntaxError, zipfile.BadZipFile) as exc:
        print(f"vendor_nlp: FAIL: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
