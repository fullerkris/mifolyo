#!/usr/bin/env python3
"""Build-time-only static Lua assembly. Never seals a Go/Python production API.

The literal inventory is completeness policy; filesystem discovery is not. BOOT
is a byte-identical passthrough and is NEVER written by this program. Missing
recipes are reported, never replaced by executable placeholders.
"""
import argparse
import hashlib
from pathlib import Path
import sys


INVENTORY = (
    "cj2_approve_boot", "cj2_install_candidate_markers", "cj2_retire_legacy_keys",
    "cj2_promote_candidate_contracts", "cj2_mark_planned_shutdown", "cj2_create_run",
    "cj2_enqueue_batch", "cj2_begin_run_audit", "cj2_audit_run_batch", "cj2_seal_run",
    "cj2_activate_run", "cj2_reject_ready", "cj2_try_claim", "cj2_renew_lease",
    "cj2_reserve_request", "cj2_start_request", "cj2_finish_request", "cj2_cancel_reservation",
    "cj2_release_before_io", "cj2_retry", "cj2_dead", "cj2_cancel_job", "cj2_complete_no_output",
    "cj2_begin_stage", "cj2_stage_page_fields", "cj2_stage_page_blob", "cj2_stage_outlinks_batch",
    "cj2_stage_discoveries_batch", "cj2_stage_aliases_batch", "cj2_stage_images_batch",
    "cj2_stage_image_manifest", "cj2_abort_stage", "cj2_seal_stage", "cj2_commit",
    "cj2_promote_due", "cj2_recover_expired", "cj2_cancel_run", "cj2_cancel_batch",
    "cj2_finalize_run", "cj2_archive_run", "cj2_purge_run_batch", "cj2_clean_stage",
    "cj2_maintain_rate_scopes",
)

# Explicit, reviewed order; no dynamic dependency scanner or runtime loader.
CORE_MODULES = (
    ("Identities", "identities.lua"), ("Schemas", "schemas.lua"),
    ("Wire", "wire.lua"), ("Context", "context.lua"), ("Read", "read.lua"),
    ("Gate", "gate.lua"), ("Memory", "memory.lua"), ("Plan", "plan.lua"),
)


def implemented(fragment: str, modules: tuple = (), with_url: bool = False) -> tuple:
    """An explicit recipe, never evidence of implementation by file discovery.

    Extra module tuples are (CJ namespace, relative chunk path). They run after
    core aliases and the optional URL factory, so they can register schemas and
    replies. Parent adds a RECIPES entry only after reviewing/testing its handler.
    """
    if not isinstance(with_url, bool) or not fragment.startswith("ops/") or not fragment.endswith(".lua"):
        raise ValueError("invalid explicit implementation recipe")
    seen = {name for name, _ in CORE_MODULES} | {"P", "Reply", "ReadProjectedState", "URL", "D"}
    for namespace, filename in modules:
        if not namespace.isascii() or not namespace.isidentifier() or namespace in seen or not filename.endswith(".lua"):
            raise ValueError("invalid/duplicate module namespace")
        seen.add(namespace)
    return modules, fragment, with_url


RECIPES = {
    "cj2_approve_boot": None,
    "cj2_install_candidate_markers": implemented("ops/cj2_install_candidate_markers.lua"),
    "cj2_create_run": implemented("ops/cj2_create_run.lua", (("Run", "ledger_run.lua"),)),
    "cj2_begin_run_audit": implemented("ops/cj2_begin_run_audit.lua", (("Run", "ledger_run.lua"),)),
    "cj2_seal_run": implemented("ops/cj2_seal_run.lua", (("Run", "ledger_run.lua"),)),
    "cj2_activate_run": implemented("ops/cj2_activate_run.lua", (("Run", "ledger_run.lua"),)),
    "cj2_cancel_run": implemented("ops/cj2_cancel_run.lua", (("Run", "ledger_run.lua"),)),
    "cj2_finalize_run": implemented("ops/cj2_finalize_run.lua", (("Run", "ledger_run.lua"),)),
    "cj2_archive_run": implemented("ops/cj2_archive_run.lua", (("Run", "ledger_run.lua"),)),
}

ADMIN_MODULES = (("Run", "ledger_run.lua"), ("Admin", "ledger_admin.lua"))
JOB_MODULES = (("Run", "ledger_run.lua"), ("Job", "ledger_job.lua"))
LIFECYCLE_MODULES = JOB_MODULES + (
    ("Request", "ledger_request.lua"), ("StageOutput", "stage_output.lua"),
    ("Stage", "ledger_stage.lua"),
)
for name in ("cj2_retire_legacy_keys", "cj2_promote_candidate_contracts", "cj2_mark_planned_shutdown"):
    RECIPES[name] = implemented("ops/" + name + ".lua", ADMIN_MODULES)
for name in ("cj2_enqueue_batch", "cj2_audit_run_batch"):
    RECIPES[name] = implemented("ops/" + name + ".lua", JOB_MODULES, with_url=True)
for name in (
    "cj2_reject_ready", "cj2_try_claim", "cj2_renew_lease", "cj2_reserve_request",
    "cj2_start_request", "cj2_finish_request", "cj2_cancel_reservation",
    "cj2_release_before_io", "cj2_retry", "cj2_dead", "cj2_cancel_job",
    "cj2_complete_no_output", "cj2_begin_stage", "cj2_stage_page_fields",
    "cj2_stage_page_blob", "cj2_stage_outlinks_batch", "cj2_stage_discoveries_batch",
    "cj2_stage_aliases_batch", "cj2_stage_images_batch", "cj2_stage_image_manifest",
    "cj2_abort_stage", "cj2_seal_stage", "cj2_commit",
):
    RECIPES[name] = implemented("ops/" + name + ".lua", LIFECYCLE_MODULES, with_url=True)
for name in (
    "cj2_promote_due", "cj2_recover_expired", "cj2_cancel_batch",
    "cj2_purge_run_batch", "cj2_clean_stage", "cj2_maintain_rate_scopes",
):
    RECIPES[name] = implemented(
        "ops/" + name + ".lua",
        LIFECYCLE_MODULES + (("Maintenance", "ledger_maintenance.lua"),),
        with_url=True,
    )


def assemble(source: Path, modules: tuple, operation: str, with_url: bool = False) -> bytes:
    def chunk(name: str) -> bytes:
        if Path(name).is_absolute() or ".." in Path(name).parts:
            raise ValueError("chunk must be relative to lua_src")
        data = (source / name).read_bytes()
        data.decode("utf-8", errors="strict")
        if not data or not data.endswith(b"\n"):
            raise ValueError(f"source must be nonempty UTF-8 ending in newline: {name}")
        return data

    pieces = [
        b"-- Generated by scripts/generate-crawl-jobs-v2-lua.py; DO NOT EDIT.\n",
        b"-- Dormant proposed source, NOT a complete or operationally approved bundle.\n",
        b"local P = (function()\n", chunk("primitives.lua"), b"end)()\n",
        b"local CJ = {P=P}\n",
    ]
    for namespace, filename in CORE_MODULES:
        pieces.extend([f"CJ.{namespace} = (function()\n".encode("ascii"), chunk(filename), b"end)()\n"])
    pieces.append(b"CJ.ReadProjectedState = CJ.Read.project\nCJ.Reply = CJ.Context.Reply\n")
    if with_url:
        pieces.extend([b"local D = (function()\n", chunk("unicode_data.lua"), b"end)()\n",
                       b"CJ.URL = (function(P,D)\n", chunk("url.lua"), b"end)(P,D)\n"])
    for namespace, filename in modules:
        pieces.extend([f"CJ.{namespace} = (function()\n".encode("ascii"), chunk(filename), b"end)()\n"])
        if namespace == "Request":
            pieces.append(b"CJ.Rate = CJ.Request.Rate\n")
    pieces.append(chunk(operation))
    return b"".join(pieces)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="check implemented recipes without writing; report incompleteness")
    parser.add_argument("--require-complete", action="store_true", help="fail unless all 43 explicit source recipes exist")
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    package = root / "services/spider/internal/database/crawljobsv2"
    if len(INVENTORY) != 43 or len(set(INVENTORY)) != 43 or set(RECIPES) - set(INVENTORY):
        raise ValueError("invalid closed operation inventory/recipes")
    for entry in (package / "lua").iterdir():
        if entry.is_symlink() or not entry.is_file() or entry.name not in {name + ".lua" for name in RECIPES}:
            raise ValueError(f"unexpected or unreviewed canonical source: {entry.name}")
    missing = [name for name in INVENTORY if name not in RECIPES]
    failures = []
    for name in INVENTORY:
        if name not in RECIPES:
            continue
        target = package / "lua" / (name + ".lua")
        recipe = RECIPES[name]
        if recipe is None:
            data = target.read_bytes()  # BOOT passthrough: no write even in build mode.
            data.decode("utf-8", errors="strict")
            if not data or not data.endswith(b"\n"):
                raise ValueError("BOOT source must be nonempty UTF-8 ending in newline")
        else:
            data = assemble(package / "lua_src", *recipe)
            if args.check:
                if not target.exists() or target.read_bytes() != data:
                    failures.append(name)
            else:
                target.write_bytes(data)
        print(f"{name}.lua bytes={len(data)} sha256={hashlib.sha256(data).hexdigest()}")
    if missing:
        print(f"INCOMPLETE: {len(RECIPES)}/43 recipes; missing {len(missing)}: " + ", ".join(missing))
    else:
        print("COMPLETE SOURCE INVENTORY: 43/43; source identity is not operational acceptance")
    if failures:
        print("stale/missing canonical sources: " + ", ".join(failures), file=sys.stderr)
    return 1 if failures or (args.require_complete and missing) else 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, ValueError) as exc:
        print(f"assembly failed: {exc}", file=sys.stderr)
        raise SystemExit(1)
