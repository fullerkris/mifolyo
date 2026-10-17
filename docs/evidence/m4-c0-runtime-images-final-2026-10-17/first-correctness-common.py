"""Final review utilities. All generated fixtures stay in this new directory."""
import hashlib
import io
import json
from pathlib import Path
import struct
import tarfile

OUT = Path(__file__).resolve().parent
BASE = OUT.parent
REPO = Path('/var/home/kfuller/Documents/Git_Projects/mifolyo')
PACKAGE = BASE / 'snapshot/tests/crawl-jobs-v2-observer'
FIRST = Path('/tmp/opencode/c0-runtime-images-2026-10-17')
PREVIOUS = Path('/tmp/opencode/c0-runtime-images-remediation-2026-10-17')
DURABLE = REPO / 'out/c0-runtime-images-2026-10-17/14ac223d10a72'
ROOT = {'uid': 0, 'gid': 0, 'mode': 0o755}
INVENTORY_SHA = '83288b8fd89fcbd6453626df75b6b48dd02150fda5def59d0942d92dc6483a6b'
REVALIDATION_SHA = '40e1106f132e054fa7654a94bba30ff85a3567b738f057527e24bba21704b851'


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def hashfile(p):
    digest = hashlib.sha256()
    with p.open('rb') as f:
        for raw in iter(lambda: f.read(1024 * 1024), b''):
            digest.update(raw)
    return digest.hexdigest()


def load(p):
    return json.loads(p.read_bytes())


def files(root):
    return {p.relative_to(root).as_posix(): hashfile(p) for p in sorted(root.rglob('*')) if p.is_file()}


def image_dir(arch):
    return Path(f'/tmp/opencode/c0-images-{arch}-remediation-2026-10-17')


def make_elf(arch='x86_64', needed=(), paths=(), interpreter=None):
    """4 KiB non-executed ELF test data, independent of production test helpers."""
    raw = bytearray(4096)
    dynamic = bool(needed or paths)
    struct.pack_into('<16sHHIQQQIHHHHHH', raw, 0, b'\x7fELF\x02\x01\x01' + bytes(9), 3,
                     {'aarch64': 183, 'x86_64': 62}[arch], 1, 0x100, 64, 0, 0, 64, 56,
                     1 + dynamic + bool(interpreter), 0, 0, 0)
    struct.pack_into('<IIQQQQQQ', raw, 64, 1, 5, 0, 0, 0, len(raw), len(raw), 4096)
    cursor = 120
    if interpreter:
        text = interpreter.encode() + b'\0'
        raw[0x200:0x200 + len(text)] = text
        struct.pack_into('<IIQQQQQQ', raw, cursor, 3, 4, 0x200, 0x200, 0, len(text), len(text), 1)
        cursor += 56
    if dynamic:
        strings, tags = bytearray(b'\0'), []
        for tag, text in [(1, name) for name in needed] + list(paths):
            tags.append((tag, len(strings)))
            strings.extend(text.encode() + b'\0')
        tags = [(5, 0x500), (10, len(strings)), *tags, (0, 0)]
        raw[0x500:0x500 + len(strings)] = strings
        struct.pack_into('<IIQQQQQQ', raw, cursor, 2, 4, 0x300, 0x300, 0, 16 * len(tags), 16 * len(tags), 8)
        for index, (tag, value) in enumerate(tags):
            struct.pack_into('<QQ', raw, 0x300 + 16 * index, tag, value)
    return bytes(raw)


def fixture(label, rows):
    folder = OUT / 'inert-fixtures'
    folder.mkdir(exist_ok=True)
    p = folder / (label + '.tar')
    with tarfile.open(p, 'w', format=tarfile.USTAR_FORMAT) as archive:
        for row in rows:
            path, kind, value = row[:3]
            member = tarfile.TarInfo(path)
            member.mode = row[3] if len(row) >= 4 else 0o755
            if len(row) >= 6:
                member.uid, member.gid = row[4:6]
            if kind == 'file':
                member.size = len(value)
                archive.addfile(member, io.BytesIO(value))
            else:
                member.type = {'directory': tarfile.DIRTYPE, 'link': tarfile.SYMTYPE, 'hardlink': tarfile.LNKTYPE}[kind]
                member.linkname = value
                archive.addfile(member)
    return p
