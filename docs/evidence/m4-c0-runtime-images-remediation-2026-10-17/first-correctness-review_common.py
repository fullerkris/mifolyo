"""Local review utilities; no engine access or output to reviewed inputs."""
import hashlib
import io
import json
from pathlib import Path
import struct
import tarfile

OUT = Path(__file__).resolve().parent
BASE = OUT.parent
PACKAGE = BASE / 'snapshot/tests/crawl-jobs-v2-observer'
REPO = Path('/var/home/kfuller/Documents/Git_Projects/mifolyo')
OLD = Path('/tmp/opencode/c0-runtime-images-2026-10-17/correctness')
ROOT = {'mode': 0o755, 'uid': 0, 'gid': 0}


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def hashfile(path):
    h = hashlib.sha256()
    with path.open('rb') as f:
        for raw in iter(lambda: f.read(1024 * 1024), b''):
            h.update(raw)
    return h.hexdigest()


def load(path):
    return json.loads(path.read_bytes())


def files(root):
    return {p.relative_to(root).as_posix(): hashfile(p) for p in sorted(root.rglob('*')) if p.is_file()}


def make_elf(architecture='x86_64', needed=(), paths=(), interpreter=None):
    b = bytearray(4096)
    dynamic = bool(needed or paths)
    struct.pack_into('<16sHHIQQQIHHHHHH', b, 0, b'\x7fELF\x02\x01\x01' + bytes(9), 3,
                     {'x86_64': 62, 'aarch64': 183}[architecture], 1, 0x100, 64, 0, 0, 64, 56,
                     1 + dynamic + bool(interpreter), 0, 0, 0)
    struct.pack_into('<IIQQQQQQ', b, 64, 1, 5, 0, 0, 0, len(b), len(b), 4096)
    cursor = 120
    if interpreter:
        text = interpreter.encode() + b'\0'
        b[0x200:0x200 + len(text)] = text
        struct.pack_into('<IIQQQQQQ', b, cursor, 3, 4, 0x200, 0x200, 0, len(text), len(text), 1)
        cursor += 56
    if dynamic:
        strings, tags = bytearray(b'\0'), []
        for tag, text in [(1, n) for n in needed] + list(paths):
            tags.append((tag, len(strings)))
            strings.extend(text.encode() + b'\0')
        tags = [(5, 0x500), (10, len(strings)), *tags, (0, 0)]
        b[0x500:0x500 + len(strings)] = strings
        struct.pack_into('<IIQQQQQQ', b, cursor, 2, 4, 0x300, 0x300, 0, 16 * len(tags), 16 * len(tags), 8)
        for i, (tag, value) in enumerate(tags):
            struct.pack_into('<QQ', b, 0x300 + 16 * i, tag, value)
    return bytes(b)


def tar_bytes(rows):
    out = io.BytesIO()
    with tarfile.open(fileobj=out, mode='w', format=tarfile.USTAR_FORMAT) as t:
        for row in rows:
            path, kind, value = row[:3]
            m = tarfile.TarInfo(path)
            m.mode = row[3] if len(row) >= 4 else 0o755
            if len(row) >= 6:
                m.uid, m.gid = row[4:6]
            if kind == 'file':
                m.size = len(value)
                t.addfile(m, io.BytesIO(value))
            else:
                m.type = {'directory': tarfile.DIRTYPE, 'link': tarfile.SYMTYPE, 'hardlink': tarfile.LNKTYPE}[kind]
                m.linkname = value
                t.addfile(m)
    return out.getvalue()


def fixture(label, rows):
    folder = OUT / 'inert-fixtures'
    folder.mkdir(exist_ok=True)
    p = folder / (label + '.tar')
    p.write_bytes(tar_bytes(rows))
    return p
