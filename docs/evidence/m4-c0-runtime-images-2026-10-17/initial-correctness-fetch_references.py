"""Read public upstream source/docs only; retain responses in the approved directory."""
import hashlib
import json
from pathlib import Path
import urllib.request

OUT = Path(__file__).resolve().parent
SOURCES = {
    'buildkit-v0.20.2-image.go': 'https://raw.githubusercontent.com/moby/buildkit/v0.20.2/frontend/dockerfile/dockerfile2llb/image.go',
    'buildkit-v0.20.2-convert.go': 'https://raw.githubusercontent.com/moby/buildkit/v0.20.2/frontend/dockerfile/dockerfile2llb/convert.go',
    'buildkit-current-image.go': 'https://raw.githubusercontent.com/moby/buildkit/master/frontend/dockerfile/dockerfile2llb/image.go',
    'buildkit-v0.20.2-path.go': 'https://raw.githubusercontent.com/moby/buildkit/v0.20.2/util/system/path.go',
    'ld-so-manual.html': 'https://man7.org/linux/man-pages/man8/ld.so.8.html',
    'checkout-pinned-action.yml': 'https://raw.githubusercontent.com/actions/checkout/11d5960a326750d5838078e36cf38b85af677262/action.yml',
    'setup-python-pinned-action.yml': 'https://raw.githubusercontent.com/actions/setup-python/a26af69be951a213d495a4c3e4e4022e16d87065/action.yml',
    'upload-artifact-pinned-action.yml': 'https://raw.githubusercontent.com/actions/upload-artifact/ea165f8d65b6e75b540449e92b4886f43607fa02/action.yml',
}
results = []
for file, url in SOURCES.items():
    try:
        with urllib.request.urlopen(url, timeout=30) as r:
            raw = r.read(4 * 1024 * 1024 + 1)
            assert len(raw) <= 4 * 1024 * 1024
            status = r.status
        (OUT / file).write_bytes(raw)
        results.append({'url': url, 'file': file, 'status': status, 'bytes': len(raw), 'sha256': hashlib.sha256(raw).hexdigest()})
    except Exception as e:
        results.append({'url': url, 'file': file, 'error': repr(e)})
(OUT / 'references.json').write_text(json.dumps(results, indent=2) + '\n')
print(json.dumps(results, indent=2))
