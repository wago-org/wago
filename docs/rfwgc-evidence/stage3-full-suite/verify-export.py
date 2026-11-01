#!/usr/bin/env python3
"""Compare every tracked non-submodule blob against an immutable git revision."""
import hashlib
import pathlib
import subprocess
import sys
revision, directory = sys.argv[1:]
root = pathlib.Path(directory)
manifest = subprocess.check_output(['git', 'ls-tree', '-rz', '--full-tree', revision])
count = 0
for entry in manifest.split(b'\0'):
    if not entry:
        continue
    meta, name = entry.split(b'\t', 1)
    mode, kind, oid = meta.split()
    if kind != b'blob':
        continue
    path = root / name.decode()
    data = str(path.readlink()).encode() if path.is_symlink() else path.read_bytes()
    actual = hashlib.sha1(b'blob ' + str(len(data)).encode() + b'\0' + data).hexdigest()
    assert actual == oid.decode(), str(path)
    count += 1
print(f'{revision}: verified {count} tracked blobs in {directory}')
