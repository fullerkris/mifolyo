"""Generate oracle image artifact identities without executing the oracle worker."""
import os
import importlib.machinery
from pathlib import Path
import stat
import sys

sys.path.insert(0, str(Path(__file__).resolve().parent))
from contracts import canonical, elf_architecture, platform_digest, require, runtime_architecture, sha
from oracle_protocol import SOURCE_FILES, source_digest


def elf_bytes(path, architecture):
    fd = os.open(path, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, "rb") as stream:
        before = os.fstat(stream.fileno())
        require(stat.S_ISREG(before.st_mode) and 0 < before.st_size <= 32 * 1024 * 1024, "ORACLE_ELF_BOUND")
        raw = stream.read(before.st_size + 1)
        after = os.fstat(stream.fileno())
        require(len(raw) == before.st_size and (before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns) ==
            (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns), "ORACLE_ELF_CHANGED")
    require(elf_architecture(raw) == architecture, "ORACLE_BUILD_ARCHITECTURE")
    return raw


def extension_inventory(architecture, modules=None):
    extensions = {}
    for module in tuple(sys.modules.values()) if modules is None else modules:
        spec = getattr(module, "__spec__", None)
        if spec is not None and isinstance(spec.loader, importlib.machinery.ExtensionFileLoader):
            path = Path(spec.origin).resolve()
            extensions[str(path)] = sha(elf_bytes(path, architecture))
    require(extensions and len(extensions) <= 128, "ORACLE_EXTENSIONS")
    return extensions


def main():
    require(len(sys.argv) in (2, 3), "ORACLE_BUILD_ARGUMENTS")
    architecture = sys.argv[2] if len(sys.argv) == 3 else "aarch64"
    runtime_architecture(architecture)
    require(os.uname().sysname == "Linux" and os.uname().machine == architecture, "ORACLE_BUILD_PLATFORM")
    destination = Path(sys.argv[1])
    require(destination.is_absolute() and not destination.exists(), "ORACLE_BUILD_OUTPUT")
    root = Path(__file__).resolve().parent
    executable = elf_bytes(Path(sys.executable).resolve(), architecture)
    files = {name: sha((root / name).read_bytes()) for name in SOURCE_FILES}
    # Import definitions only. No Engine/reader/client is instantiated and no
    # worker main or Linux collector runs. Record the needed extension modules.
    import oracle_worker
    require(oracle_worker.__file__ == str(root / "oracle_worker.py"), "ORACLE_IMPORT_SOURCE")
    extensions = extension_inventory(architecture)
    require(files == {name: sha((root / name).read_bytes()) for name in SOURCE_FILES}, "ORACLE_SOURCE_CHANGED")
    record = {"version": 3, "architecture": architecture, "platform_sha256": platform_digest(architecture), "python": sys.version,
        "oracle_elf": sha(executable), "oracle_worker_source": source_digest(),
        "files": files, "extensions": extensions, "worker_import_checked": True,
        "worker_executed": False, "runtime_feasibility_proven": False, "execution_authorized": False}
    destination.mkdir(mode=0o755)
    (destination / "oracle-artifacts.json").write_bytes(canonical(record))


if __name__ == "__main__":
    main()
