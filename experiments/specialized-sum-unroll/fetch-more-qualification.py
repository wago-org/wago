#!/usr/bin/env python3
"""Reproduce pinned DuckDB acquisition and recorded Biowasm download attempts.

Artifacts stay outside the repository. Never execute or extract package scripts.
"""
import argparse, base64, hashlib, io, json, pathlib, tarfile, urllib.request

p = argparse.ArgumentParser()
p.add_argument('--cache', required=True)
p.add_argument('--out', required=True)
a = p.parse_args()
root = pathlib.Path(__file__).resolve().parent
rows = json.loads((root/'results/qualification/additional-manifest.json').read_text())
cache = pathlib.Path(a.cache)
packages = {}
for r in rows:
    r.pop('local_path', None)
    try:
        if r['family'] == 'duckdb':
            if r['url'] not in packages:
                with urllib.request.urlopen(r['url'], timeout=60) as response:
                    blob = response.read(160*1024*1024+1)
                assert len(blob) <= 160*1024*1024
                assert hashlib.sha256(blob).hexdigest() == r['package_sha256']
                assert 'sha512-'+base64.b64encode(hashlib.sha512(blob).digest()).decode() == r['package_integrity']
                packages[r['url']] = blob
            with tarfile.open(fileobj=io.BytesIO(packages[r['url']])) as t:
                data = t.extractfile(r['path']).read()
            assert hashlib.sha256(data).hexdigest() == r['sha256']
        else:
            # This URL returned403 in the original investigation. A later
            # successful download would need its own hash and analysis.
            tool, name = r['path'].split('/')
            r['url'] = f'https://cdn.biowasm.com/{tool}/{r["version"]}/{name}'
            with urllib.request.urlopen(r['url'], timeout=45) as response:
                data = response.read(64*1024*1024+1)
            assert len(data) <= 64*1024*1024 and data[:4] == b'\0asm'
            r['sha256'] = hashlib.sha256(data).hexdigest()
        dest = cache/r['family']/r['path']
        dest.parent.mkdir(parents=True, exist_ok=True)
        dest.write_bytes(data)
        r['local_path'] = str(dest)
        r['bytes'] = len(data)
        r.pop('error', None)
    except Exception as e:
        r['error'] = str(e)
pathlib.Path(a.out).write_text(json.dumps(rows, indent=2)+'\n')
