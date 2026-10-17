"""Generate finite headers/profiles and statically inspect ELF; never execute target code."""
import json
from pathlib import Path
import struct
import subprocess
import sys

from contracts import TRIALS, canonical, elf_architecture, platform_digest, require, runtime_architecture, sha
from profiles import profile, target_filter_program


def parse_elf(raw, architecture="aarch64"):
    selected = runtime_architecture(architecture)
    require(elf_architecture(raw) == architecture, "ELF_ARCHITECTURE")
    require(64 <= len(raw) <= 32 * 1024 * 1024 and raw[:7] == b"\x7fELF\x02\x01\x01", "ELF")
    header = struct.unpack_from("<16sHHIQQQIHHHHHH", raw)
    require(header[1:4] == (3, selected["elf_machine"], 1), "ELF_TARGET")
    phoff, shoff, phsize, phnum, shsize, shnum, strings = header[5], header[6], header[9], header[10], header[11], header[12], header[13]
    require(phsize == 56 and 0 < phnum <= 64 and shsize == 64 and 0 < shnum <= 256 and strings < shnum, "ELF_TABLE")
    require(phoff + phnum * phsize <= len(raw) and shoff + shnum * shsize <= len(raw), "ELF_TABLE")
    programs = [struct.unpack_from("<IIQQQQQQ", raw, phoff + i * phsize) for i in range(phnum)]
    require(not any(row[0] == 3 for row in programs), "DYNAMIC_INTERPRETER")
    loads = [row for row in programs if row[0] == 1]
    require(loads and all(row[2] + row[5] <= len(raw) and row[5] <= row[6] and row[1] & 3 != 3 for row in loads), "ELF_LOAD")
    sections = [struct.unpack_from("<IIQQQQIIQQ", raw, shoff + i * shsize) for i in range(shnum)]
    require(all(row[1] == 8 or row[4] + row[5] <= len(raw) for row in sections), "ELF_SECTION")
    symbols = {}
    for section in sections:
        if section[1] != 2:
            continue
        require(section[9] == 24 and section[5] % 24 == 0 and section[6] < shnum, "SYMBOL_TABLE")
        strings_section = sections[section[6]]
        table = raw[strings_section[4]:strings_section[4] + strings_section[5]]
        for offset in range(section[4], section[4] + section[5], 24):
            name, info, _, index, address, size = struct.unpack_from("<IBBHQQ", raw, offset)
            require(name < len(table), "SYMBOL_NAME")
            end = table.find(b"\0", name)
            require(end >= name, "SYMBOL_NAME")
            label = table[name:end].decode("utf-8")
            if label.startswith("obs_") and index != 0:
                require(label not in symbols, "DUPLICATE_SYMBOL")
                symbols[label] = {"vaddr": address, "size": size, "type": info & 15}
    return loads, symbols


def instruction(raw, loads, address):
    segments = [row for row in loads if row[1] & 1 and row[3] <= address and address + 4 <= row[3] + row[5]]
    require(len(segments) == 1 and address % 4 == 0, "INSTRUCTION")
    segment = segments[0]
    offset = segment[2] + address - segment[3]
    return offset, struct.unpack_from("<I", raw, offset)[0]


def x64_code(raw, loads, address, size):
    segments = [row for row in loads if row[1] & 1 and row[3] <= address and address + size <= row[3] + row[5]]
    require(len(segments) == 1, "INSTRUCTION")
    offset = segments[0][2] + address - segments[0][3]
    return offset, raw[offset:offset + size]


def x64_markers(raw, loads, symbols):
    """Decode only the closed SysV effect/marker sequences, never arbitrary x86 code."""
    effect = bytes.fromhex("48c7012a000000488916488917c3")
    require("obs_effect" in symbols, "TARGET_SYMBOLS")
    start = symbols["obs_effect"]["vaddr"]
    require(start % 8 == 0 and symbols["obs_effect"]["type"] == 2 and symbols["obs_effect"]["size"] == len(effect) and
        symbols["obs_pre_effect"]["vaddr"] == start and symbols["obs_post_effect"]["vaddr"] == start + 13, "EFFECT_BOUNDARY")
    require(x64_code(raw, loads, start, len(effect))[1] == effect, "EFFECT_INSTRUCTIONS")
    mapping = {}
    for name, size in (("obs_pre_effect", 7), ("obs_post_effect", 1), ("obs_vm_marker", 1), ("obs_send_marker", 1)):
        symbol = symbols[name]
        if name in ("obs_vm_marker", "obs_send_marker"):
            require(symbol["vaddr"] % 8 == 0 and symbol["type"] == 2 and symbol["size"] == 2 and
                x64_code(raw, loads, symbol["vaddr"], 2)[1] == b"\x90\xc3", "MARKER")
        offset, code = x64_code(raw, loads, symbol["vaddr"], size)
        mapping[name] = symbol | {"file_offset": offset, "instruction": int.from_bytes(code, "little"), "instruction_bytes": size}
    require(len({row["vaddr"] for row in mapping.values()}) == 4, "MARKER_ALIAS")
    mapping["obs_witness_address"] = symbols["obs_witness_address"]
    return dict(sorted(mapping.items()))


def target_manifest(raw, architecture="aarch64"):
    loads, symbols = parse_elf(raw, architecture)
    wanted = {"obs_pre_effect", "obs_post_effect", "obs_vm_marker", "obs_send_marker", "obs_witness_address"}
    require(wanted <= set(symbols), "TARGET_SYMBOLS")
    executable = [row for row in loads if row[1] & 1]
    require(len(executable) == 1, "EXECUTABLE_SEGMENT")
    segment = executable[0]
    if architecture == "aarch64":
        mapping = {name: symbols[name] | ({"file_offset": instruction(raw, loads, symbols[name]["vaddr"])[0],
            "instruction": instruction(raw, loads, symbols[name]["vaddr"])[1]} if name != "obs_witness_address" else {}) for name in sorted(wanted)}
        require(mapping["obs_post_effect"]["instruction"] == 0xd65f03c0, "POST_RETURN")
        require(mapping["obs_vm_marker"]["instruction"] == mapping["obs_send_marker"]["instruction"] == 0xd503201f, "MARKER")
    else:
        mapping = x64_markers(raw, loads, symbols)
        witness = symbols["obs_witness_address"]
        require(witness["type"] == 1 and witness["size"] == 8 and witness["vaddr"] % 8 == 0 and
            any(row[1] & 2 and row[3] <= witness["vaddr"] and witness["vaddr"] + 8 <= row[3] + row[6] for row in loads), "WITNESS_SYMBOL")
    require(any(row[1] & 2 and row[3] <= symbols["obs_witness_address"]["vaddr"] < row[3] + row[6] for row in loads), "WITNESS_SYMBOL")
    return {"version": 2, "kind": "synthetic_static_instruction_manifest", "architecture": architecture,
        "platform_sha256": platform_digest(architecture),
        "elf_sha256": sha(raw), "elf_bytes": len(raw), "symbols": mapping,
        "exec_mapping": {"offset": segment[2] & ~4095, "vaddr": segment[3] & ~4095, "bytes": segment[5]},
        "witness_bytes": 8480, "descriptor_count": 1050, "same_value": 42,
        "runtime_load_bias_verified": False, "execution_authorized": False}


def write(path, raw):
    with path.open("xb") as stream:
        stream.write(raw)


def main():
    require(len(sys.argv) in (3, 4), "BUILD_ARGUMENTS")
    architecture = sys.argv[3] if len(sys.argv) == 4 else "aarch64"
    runtime_architecture(architecture)
    mode, out = sys.argv[1], Path(sys.argv[2])
    require(out.is_dir(), "OUTPUT")
    if mode == "headers":
        cases = "static const char *const OBS_CASES[276] = {\n" + ",\n".join(json.dumps(case) for case in TRIALS) + "\n};\n"
        write(out / "cases.h", cases.encode())
        program = target_filter_program(architecture)
        rows = ",\n".join("{%d,%d,%d,%du}" % row for row in program)
        write(out / "target_filter.h", ("static const struct sock_filter obs_filter[] __attribute__((section(\".obs_filter\"),used)) = {\n" + rows + "\n};\n").encode())
        write(out / "target-filter.bpf", b"".join(struct.pack("<HBBI", *row) for row in program))
        for role in ("observer", "target", "oracle"):
            write(out / (role + "-seccomp.json"), canonical(profile(role, architecture)))
    elif mode == "manifest":
        raw = (out / "target").read_bytes()
        manifest = target_manifest(raw, architecture)
        write(out / "target-manifest.json", canonical(manifest))
        mapping = manifest["symbols"]
        values = {"OBS_TARGET_SIZE": len(raw), "OBS_EXEC_OFFSET": manifest["exec_mapping"]["offset"],
            "OBS_EXEC_VADDR": manifest["exec_mapping"]["vaddr"], "OBS_EXEC_SIZE": manifest["exec_mapping"]["bytes"],
            "OBS_WITNESS_SYMBOL": mapping["obs_witness_address"]["vaddr"], "OBS_PRE_EFFECT": mapping["obs_pre_effect"]["vaddr"],
            "OBS_POST_EFFECT": mapping["obs_post_effect"]["vaddr"], "OBS_VM_MARKER": mapping["obs_vm_marker"]["vaddr"],
            "OBS_SEND_MARKER": mapping["obs_send_marker"]["vaddr"], "OBS_PRE_INSTRUCTION": mapping["obs_pre_effect"]["instruction"],
            "OBS_POST_INSTRUCTION": mapping["obs_post_effect"]["instruction"], "OBS_NOP_INSTRUCTION": mapping["obs_vm_marker"]["instruction"],
            "OBS_PRE_BYTES": mapping["obs_pre_effect"].get("instruction_bytes", 4),
            "OBS_POST_BYTES": mapping["obs_post_effect"].get("instruction_bytes", 4),
            "OBS_NOP_BYTES": mapping["obs_vm_marker"].get("instruction_bytes", 4),
            "OBS_TARGET_MACHINE": runtime_architecture(architecture)["elf_machine"]}
        header = '#define OBS_TARGET_SHA256 "' + sha(raw) + '"\n' + "".join(f"#define {name} UINT64_C({value})\n" for name, value in values.items())
        write(out / "target_manifest.h", header.encode())
    elif mode == "record":
        target = (out / "target").read_bytes()
        manifest = target_manifest(target, architecture)
        require((out / "target-manifest.json").read_bytes() == canonical(manifest), "BUILD_TARGET_MANIFEST")
        observer = (out / "observer").read_bytes()
        loads, symbols = parse_elf(observer, architecture)
        filter_symbol = symbols["obs_filter"]
        segments = [row for row in loads if row[3] <= filter_symbol["vaddr"] and filter_symbol["vaddr"] + filter_symbol["size"] <= row[3] + row[5]]
        require(len(segments) == 1, "COMPILED_FILTER")
        offset = segments[0][2] + filter_symbol["vaddr"] - segments[0][3]
        require(observer[offset:offset + filter_symbol["size"]] == (out / "target-filter.bpf").read_bytes(), "COMPILED_FILTER")
        require((out / "target-filter.bpf").read_bytes() == b"".join(struct.pack("<HBBI", *row)
            for row in target_filter_program(architecture)), "BUILD_FILTER_ARCHITECTURE")
        commands = {}
        for command in (("cc", "--version"), ("ld", "--version"), ("cc", "-dumpmachine")):
            process = subprocess.run(command, capture_output=True, check=True, timeout=10)
            commands[" ".join(command)] = process.stdout.decode()
        require(commands["cc -dumpmachine"].strip().startswith(architecture + "-"), "BUILD_ARCHITECTURE")
        libraries = {}
        for name in ("libc.a", "libcrypto.a", "crtbeginS.o"):
            path = subprocess.check_output(["cc", "-print-file-name=" + name], text=True, timeout=10).strip()
            require(Path(path).is_file(), "TOOLCHAIN_LIBRARY")
            libraries[name] = sha(Path(path).read_bytes())
        sources = {path.as_posix(): sha(path.read_bytes()) for path in Path(".").rglob("*") if path.is_file() and "__pycache__" not in path.parts}
        write(out / "toolchain.json", canonical({"version": 2, "architecture": architecture, "platform_sha256": platform_digest(architecture),
            "commands": commands, "libraries": libraries, "sources": sources,
            "target_sha256": sha(target), "observer_sha256": sha(observer),
            "native_programs_executed": False, "tracing_performed": False}))
    else:
        raise ValueError("MODE")


if __name__ == "__main__":
    main()
