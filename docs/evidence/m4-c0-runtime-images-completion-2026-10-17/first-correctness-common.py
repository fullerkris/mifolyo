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
GO = Path('/tmp/opencode/c0-runtime-images-final-2026-10-17')
CONSTRUCTION = Path('/tmp/opencode/c0-runtime-images-remediation-2026-10-17')
ROOT = {'uid': 0, 'gid': 0, 'mode': 0o755}
INVENTORY_SHA = '2afe7326f090a3b8ba3ef882a8b3fa89d3e4dd73a9c8c074288d541364487043'
REVALIDATION_SHA = '55a7c9f9738d942a745ab47626afe9ef157998039a2d2e8779bee24f992aa3fd'


def sha(data):
    return hashlib.sha256(data).hexdigest()


def hashfile(p):
    h = hashlib.sha256()
    with p.open('rb') as f:
        for data in iter(lambda: f.read(1048576), b''):
            h.update(data)
    return h.hexdigest()


def load(p):
    return json.loads(p.read_bytes())


def files(root):
    return {p.relative_to(root).as_posix(): hashfile(p) for p in sorted(root.rglob('*')) if p.is_file()}


def image_dir(arch):
    return Path(f'/tmp/opencode/c0-images-{arch}-remediation-2026-10-17')


def elf(arch):
    raw = bytearray(4096)
    struct.pack_into('<16sHHIQQQIHHHHHH', raw, 0, b'\x7fELF\x02\x01\x01' + bytes(9), 3,
                     {'aarch64': 183, 'x86_64': 62}[arch], 1, 0x100, 64, 0, 0, 64, 56, 1, 0, 0, 0)
    struct.pack_into('<IIQQQQQQ', raw, 64, 1, 5, 0, 0, 0, len(raw), len(raw), 4096)
    return bytes(raw)


def fixture(label, rows):
    folder = OUT / 'inert-fixtures'
    folder.mkdir(exist_ok=True)
    p = folder / (label + '.tar')
    with tarfile.open(p, 'w', format=tarfile.USTAR_FORMAT) as archive:
        root = tarfile.TarInfo('.'); root.type = tarfile.DIRTYPE; root.mode = 0o755; archive.addfile(root)
        for path, kind, value, mode, uid, gid in rows:
            member = tarfile.TarInfo(path); member.mode, member.uid, member.gid = mode, uid, gid
            if kind == 'file':
                member.size = len(value); archive.addfile(member, io.BytesIO(value))
            else:
                member.type = {'directory': tarfile.DIRTYPE, 'link': tarfile.SYMTYPE, 'hardlink': tarfile.LNKTYPE}[kind]
                member.linkname = value; archive.addfile(member)
    return p
