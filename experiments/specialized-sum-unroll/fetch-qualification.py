#!/usr/bin/env python3
"""Download pinned research artifacts to an external cache; never run guest code.

Tree inventories are retained in results/qualification. No binaries are added to
the corpus. License status here is an inventory note, not redistribution consent.
"""
import argparse, concurrent.futures, hashlib, json, pathlib, urllib.request

p = argparse.ArgumentParser()
p.add_argument('--cache', required=True)
p.add_argument('--out', required=True)
a = p.parse_args()
root = pathlib.Path(__file__).resolve().parent
cache = pathlib.Path(a.cache)
sources = [
    ('sightglass', 'bytecodealliance/sightglass', root/'results/qualification/sightglass-tree.json', 'Apache-2.0 OR MIT; bundled benchmark dependencies need separate audit'),
    ('polybench-published', 'BenchmarkingWasm/BenchmarkingWebAssembly', root/'results/qualification/poly-tree.json', 'PolyBench source licenses; artifact redistribution not established'),
    ('r3', 'doehyunbaek/wasm-benchmarks', root/'results/followup/r3-tree.json', 'No repository license found; original application rights vary; no redistribution'),
]
jobs = []
for family, repo, treepath, license_note in sources:
    tree = json.loads(treepath.read_text())
    for item in tree['tree']:
        if item['type'] != 'blob' or not item['path'].endswith('.wasm'):
            continue
        if family == 'r3' and not item['path'].startswith('wasm-r3-bench/'):
            continue  # replay corpus, not duplicate reduction-tool inputs
        jobs.append(dict(family=family, repository='https://github.com/'+repo,
                         revision=tree['sha'], path=item['path'], git_blob=item['sha'],
                         bytes=item['size'], license_note=license_note,
                         url=f'https://raw.githubusercontent.com/{repo}/{tree["sha"]}/{item["path"]}'))
assert sum(x['bytes'] for x in jobs) < 256*1024*1024

def fetch(row):
    row = dict(row)
    target = cache/row['family']/row['path']
    target.parent.mkdir(parents=True, exist_ok=True)
    try:
        if not target.exists():
            with urllib.request.urlopen(row['url'], timeout=60) as response:
                data = response.read(row['bytes']+1)
            if len(data) != row['bytes']:
                raise ValueError('size mismatch')
            target.write_bytes(data)
        data = target.read_bytes()
        if len(data) != row['bytes'] or hashlib.sha1(b'blob '+str(len(data)).encode()+b'\0'+data).hexdigest() != row['git_blob']:
            raise ValueError('Git blob mismatch')
        row['sha256'] = hashlib.sha256(data).hexdigest()
        row['local_path'] = str(target)
    except Exception as e:
        row['error'] = str(e)
    return row

with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
    rows = list(pool.map(fetch, jobs))
pathlib.Path(a.out).write_text(json.dumps(rows, indent=2)+'\n')
print('modules', len(rows), 'bytes', sum(x['bytes'] for x in rows), 'errors', sum('error' in x for x in rows))
