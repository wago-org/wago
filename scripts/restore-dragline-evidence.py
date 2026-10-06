#!/usr/bin/env python3
"""Restore the lossless Dragline checkpoint archive (Python 3 + zstd required)."""
import argparse
import hashlib
import json
from pathlib import Path, PurePosixPath
import re
import shutil
import subprocess
import tarfile
import tempfile


def digest(path):
    h = hashlib.sha256()
    with path.open('rb') as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()


def main():
    root = Path(__file__).resolve().parents[1]
    default = root / 'docs/performance/dragline-20261003'
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--destination', type=Path, default=default)
    parser.add_argument('--verify-only', action='store_true')
    args = parser.parse_args()
    bundle = default / 'evidence'
    index = json.loads((bundle / 'bundle.json').read_text())
    if index['format'] != 'dragline-evidence-v1':
        raise ValueError('Unsupported archive format')
    destination = args.destination.resolve()
    with tempfile.TemporaryDirectory(prefix='dragline-evidence-') as temp:
        packed = Path(temp) / 'evidence.tar.zst'
        with packed.open('wb') as output:
            for part in index['parts']:
                if Path(part['name']).name != part['name']:
                    raise ValueError('Invalid bundle part name')
                path = bundle / part['name']
                if path.stat().st_size != part['size'] or digest(path) != part['sha256']:
                    raise ValueError('Bundle part checksum mismatch: ' + part['name'])
                with path.open('rb') as stream:
                    shutil.copyfileobj(stream, output)
        if digest(packed) != index['archive_sha256']:
            raise ValueError('Combined archive checksum mismatch')
        process = subprocess.Popen(['zstd', '-d', '--long=27', '-q', '-c', str(packed)], stdout=subprocess.PIPE)
        seen = set()
        try:
            with tarfile.open(fileobj=process.stdout, mode='r|') as archive:
                first = archive.next()
                if first is None or first.name != 'manifest.json' or not first.isfile():
                    raise ValueError('Missing manifest')
                raw = archive.extractfile(first).read()
                if hashlib.sha256(raw).hexdigest() != index['manifest_sha256']:
                    raise ValueError('Manifest checksum mismatch')
                manifest = json.loads(raw)
                records = manifest['files']
                if len(records) != index['file_count'] or sum(r['size'] for r in records) != index['logical_bytes']:
                    raise ValueError('Manifest inventory mismatch')
                objects, paths = {}, set()
                for record in records:
                    rel = PurePosixPath(record['path'])
                    if rel.is_absolute() or '..' in rel.parts or not rel.parts or record['path'] in paths:
                        raise ValueError('Invalid or duplicate archive path')
                    paths.add(record['path'])
                    target = destination.joinpath(*rel.parts)
                    if not target.resolve().is_relative_to(destination) or target.is_symlink():
                        raise ValueError('Destination escapes archive directory')
                    if not args.verify_only and target.exists() and digest(target) != record['sha256']:
                        raise ValueError('Preserving changed existing file: ' + str(target))
                    objects.setdefault(record['sha256'], []).append((target, record))
                for member in archive:
                    if member.name == 'manifest.json':
                        continue  # tarfile's iterator also yields the cached first member.
                    key = member.name.removeprefix('objects/')
                    if not member.isfile() or not re.fullmatch(r'[0-9a-f]{64}', key) or key not in objects or key in seen:
                        raise ValueError('Invalid archive object')
                    data = archive.extractfile(member).read()
                    if hashlib.sha256(data).hexdigest() != key:
                        raise ValueError('Object checksum mismatch: ' + key)
                    for target, record in objects[key]:
                        if len(data) != record['size']:
                            raise ValueError('Object size mismatch')
                        if not args.verify_only and not target.exists():
                            target.parent.mkdir(parents=True, exist_ok=True)
                            with target.open('xb') as output:
                                output.write(data)
                            target.chmod(record['mode'])
                    seen.add(key)
            if process.wait() != 0 or len(seen) != index['object_count'] or seen != set(objects):
                raise ValueError('Incomplete archive')
        finally:
            process.stdout.close()
            if process.poll() is None:
                process.terminate()
                process.wait()
    print(('Verified' if args.verify_only else 'Restored') + f" {index['file_count']:,} files ({index['logical_bytes']:,} bytes)")


if __name__ == '__main__':
    main()
