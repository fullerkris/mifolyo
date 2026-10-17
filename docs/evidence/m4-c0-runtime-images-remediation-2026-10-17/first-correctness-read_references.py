"""Read public reference text, never an image registry or container engine."""
import json
import urllib.request
from review_common import OUT, sha

urls = {
    'podman-4.9.3-build.md': 'https://raw.githubusercontent.com/containers/podman/v4.9.3/docs/source/markdown/podman-build.1.md.in',
    'podman-4.9.3-mount.md': 'https://raw.githubusercontent.com/containers/podman/v4.9.3/docs/source/markdown/podman-mount.1.md.in',
    'podman-4.9.3-unshare.md': 'https://raw.githubusercontent.com/containers/podman/v4.9.3/docs/source/markdown/podman-unshare.1.md',
    'podman-4.9.3-unmount.md': 'https://raw.githubusercontent.com/containers/podman/v4.9.3/docs/source/markdown/podman-unmount.1.md.in',
    'ubuntu-24-runner.md': 'https://raw.githubusercontent.com/actions/runner-images/main/images/ubuntu/Ubuntu2404-Readme.md',
    'ubuntu-24-arm-runner.md': 'https://raw.githubusercontent.com/actions/partner-runner-images/main/images/arm-ubuntu-24-image.md',
    'linux-v6.18-binfmt_elf.c': 'https://raw.githubusercontent.com/torvalds/linux/v6.18/fs/binfmt_elf.c',
    'linux-v6.18-exec.c': 'https://raw.githubusercontent.com/torvalds/linux/v6.18/fs/exec.c',
}
records = []
for file, url in urls.items():
    try:
        with urllib.request.urlopen(url, timeout=30) as r:
            raw = r.read(4 * 1024 * 1024 + 1)
            assert len(raw) <= 4 * 1024 * 1024
            status = r.status
        (OUT / file).write_bytes(raw)
        records.append({'file': file, 'url': url, 'status': status, 'sha256': sha(raw), 'bytes': len(raw)})
    except Exception as e:
        records.append({'file': file, 'url': url, 'error': repr(e)})
(OUT / 'references.json').write_text(json.dumps(records, indent=2) + '\n')
print(json.dumps(records, indent=2))
