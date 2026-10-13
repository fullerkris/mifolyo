"""Generate oracle image artifact identities without executing the oracle worker."""
import os
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parent))
from contracts import canonical, elf_architecture, platform_digest, require, runtime_architecture, sha
from oracle_protocol import SOURCE_FILES, source_digest


def main():
    require(len(sys.argv) in (2, 3), "ORACLE_BUILD_ARGUMENTS")
    architecture = sys.argv[2] if len(sys.argv) == 3 else "aarch64"
    runtime_architecture(architecture)
    require(os.uname().sysname == "Linux" and os.uname().machine == architecture, "ORACLE_BUILD_PLATFORM")
    destination = Path(sys.argv[1])
    require(destination.is_absolute() and not destination.exists(), "ORACLE_BUILD_OUTPUT")
    destination.mkdir(mode=0o755)
    root = Path(__file__).resolve().parent
    executable = Path(sys.executable).resolve().read_bytes()
    require(0 < len(executable) <= 32 * 1024 * 1024, "ORACLE_ELF_BOUND")
    require(elf_architecture(executable) == architecture, "ORACLE_BUILD_ARCHITECTURE")
    record = {"version": 2, "architecture": architecture, "platform_sha256": platform_digest(architecture), "python": sys.version,
        "oracle_elf": sha(executable), "oracle_worker_source": source_digest(),
        "files": {name: sha((root / name).read_bytes()) for name in SOURCE_FILES},
        "worker_executed": False, "runtime_feasibility_proven": False, "execution_authorized": False}
    (destination / "oracle-artifacts.json").write_bytes(canonical(record))


if __name__ == "__main__":
    main()
