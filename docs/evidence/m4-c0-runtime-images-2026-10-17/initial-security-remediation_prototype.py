"""Detached remediation helpers; not installed into the repository.

Integration points are described in review.md. Only image-local strings are
resolved here; none of these helpers accesses a host filesystem path.
"""
from collections import deque
from pathlib import PurePosixPath

from contracts import require
from image_artifacts import normalized


def portable_member_key(member):
    require(not member.name.startswith('/') and '..' not in member.name.split('/'), 'IMAGE_SAVE_PATH')
    return normalized(member.name)


def check_unique_portable_members(archive):
    seen = set()
    for count, member in enumerate(archive, 1):
        require(count <= 1024, 'IMAGE_SAVE_MEMBER')
        key = portable_member_key(member)
        require(key not in seen, 'IMAGE_SAVE_DUPLICATE_PATH')
        seen.add(key)
    return len(seen)


def check_root_member_budget(archive):
    root_seen = False
    count = 0
    for count, member in enumerate(archive, 1):
        require(count <= 20000, 'IMAGE_DUPLICATE_OR_COUNT')
        if member.name.rstrip('/') in ('', '.'):
            require(member.isdir() and not root_seen, 'IMAGE_ROOT_DUPLICATE')
            root_seen = True
    return count


def absolute_link_target(name, target, symbolic):
    require(type(target) is str and target, 'IMAGE_LINK')
    base = str(PurePosixPath(name).parent) + '/' if symbolic and not target.startswith('/') else '/'
    raw_target = base + target
    normalized(raw_target)  # Validate bounds/characters/lexical escapes only.
    return raw_target      # Do not discard '..' before following earlier links.


def resolve_components(entries, path):
    normalized(path)  # Validate the input, but preserve its component sequence.
    pending = deque(path.split('/'))
    resolved = []
    links = 0
    while pending:
        part = pending.popleft()
        if part in ('', '.'):
            continue
        if part == '..':
            require(bool(resolved), 'IMAGE_PATH_ESCAPE')
            resolved.pop()
            continue
        prefix = '/' + '/'.join([*resolved, part])
        row = entries.get(prefix)
        require(row is not None, 'IMAGE_FILE_MISSING')
        if row['kind'] == 'link':
            links += 1
            require(links <= 40, 'IMAGE_LINK_DEPTH')
            combined = row['target'] + ('/' + '/'.join(pending) if pending else '')
            require(len(combined) <= 4096, 'IMAGE_PATH')
            pending = deque(combined.split('/'))
            resolved = []  # Stored targets are absolute image-local paths.
        else:
            require(not pending or row['kind'] == 'directory', 'IMAGE_PATH_PARENT')
            resolved.append(part)
    result = '/' + '/'.join(resolved)
    require(result in entries, 'IMAGE_FILE_MISSING')
    return result
