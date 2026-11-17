#!/usr/bin/env python3
"""Fetch immutable artifact addresses; fail closed on any SHA mismatch."""
import argparse,hashlib,json,pathlib,re,urllib.request
p=argparse.ArgumentParser();p.add_argument('--contracts',required=True);a=p.parse_args()
for r in json.loads(pathlib.Path(a.contracts).read_text()):
 sha=r['artifact_sha256'];assert re.fullmatch('[0-9a-f]{64}',sha)
 dest=pathlib.Path(r['path']);assert not dest.is_absolute() and '..' not in dest.parts
 if dest.exists():data=dest.read_bytes()
 else:
  with urllib.request.urlopen('https://wasm.fyi/api/artifacts/'+sha+'.wasm',timeout=120) as response:data=response.read()
 assert hashlib.sha256(data).hexdigest()==sha,r['id']
 if not dest.exists():dest.parent.mkdir(parents=True,exist_ok=True);dest.write_bytes(data)
print('All artifacts hash verified')
