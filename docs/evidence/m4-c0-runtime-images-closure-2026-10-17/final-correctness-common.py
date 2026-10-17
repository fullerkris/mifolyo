"""Inert review utilities; generated files remain under this review directory."""
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
PREVIOUS = Path('/tmp/opencode/c0-runtime-images-completion-2026-10-17')
GO = Path('/tmp/opencode/c0-runtime-images-final-2026-10-17')
CONSTRUCTION = Path('/tmp/opencode/c0-runtime-images-remediation-2026-10-17')
ROOT = {'uid': 0, 'gid': 0, 'mode': 0o755}
INVENTORY_SHA = '1f0e82a5ca98f02bf36deb4292501a0b97a38ac8bf567fd170fc09c8d4890376'
REVALIDATION_SHA = 'a8f6aee7dee482c03b925bef48a57e2ed5eb5d46edc61ea11f67c9790b5211f7'


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def hashfile(path):
    digest = hashlib.sha256()
    with path.open('rb') as file:
        for raw in iter(lambda: file.read(1048576), b''):
            digest.update(raw)
    return digest.hexdigest()


def load(path):
    return json.loads(path.read_bytes())


def files(root):
    return {path.relative_to(root).as_posix(): hashfile(path) for path in sorted(root.rglob('*')) if path.is_file()}


def image_dir(architecture):
    return Path(f'/tmp/opencode/c0-images-{architecture}-remediation-2026-10-17')


def elf(architecture):
    raw = bytearray(4096)
    struct.pack_into('<16sHHIQQQIHHHHHH', raw, 0, b'\x7fELF\x02\x01\x01' + bytes(9), 3,
                     {'aarch64': 183, 'x86_64': 62}[architecture], 1, 0x100, 64, 0, 0, 64, 56, 1, 0, 0, 0)
    struct.pack_into('<IIQQQQQQ', raw, 64, 1, 5, 0, 0, 0, len(raw), len(raw), 4096)
    return bytes(raw)


def fixture(label, rows):
    directory = OUT / 'inert-fixtures'
    directory.mkdir(exist_ok=True)
    path = directory / (label + '.tar')
    with tarfile.open(path, 'w', format=tarfile.USTAR_FORMAT) as archive:
        root = tarfile.TarInfo('.'); root.type = tarfile.DIRTYPE; root.mode = 0o755; archive.addfile(root)
        for name, kind, value, mode, uid, gid in rows:
            member = tarfile.TarInfo(name); member.mode, member.uid, member.gid = mode, uid, gid
            if kind == 'file':
                member.size = len(value); archive.addfile(member, io.BytesIO(value))
            else:
                member.type = {'directory': tarfile.DIRTYPE, 'link': tarfile.SYMTYPE, 'hardlink': tarfile.LNKTYPE}[kind]
                member.linkname = value; archive.addfile(member)
    return path
