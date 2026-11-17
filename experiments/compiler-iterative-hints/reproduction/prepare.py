#!/usr/bin/env python3
"""Prepare exact pinned upstream sources plus the recorded compatibility patch."""
import argparse, pathlib, subprocess, shutil, hashlib, json
p=argparse.ArgumentParser();p.add_argument('--upstream',required=True);a=p.parse_args()
root=pathlib.Path(__file__).resolve().parent; source=pathlib.Path(a.upstream).resolve(); dest=root/'wasm-bench'
pin='a017c56fef370a54ffa33ca49ad9b6827e127cc4'
assert subprocess.check_output(['git','-C',str(source),'rev-parse','HEAD'],text=True).strip()==pin
assert not subprocess.check_output(['git','-C',str(source),'status','--porcelain']), 'upstream checkout must be clean'
assert not dest.exists(), 'refusing to overwrite prepared source'
dest.mkdir()
for name in ['adapters','protocol','collectors','corpus']:shutil.copytree(source/name,dest/name)
for name in ['go.mod','go.sum']:shutil.copy2(source/name,dest/name)
subprocess.run(['git','apply',str(root/'compatibility.patch')],cwd=dest,check=True)
receipt=json.loads((root/'source-receipt.json').read_text())
for name,expected in receipt['files'].items():
 assert hashlib.sha256((dest/name).read_bytes()).hexdigest()==expected, name
print('Prepared source matches recorded source receipt')
