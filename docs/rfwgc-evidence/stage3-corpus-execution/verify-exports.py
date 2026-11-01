#!/usr/bin/env python3
"""Read-only check that test execution left tracked export bytes unchanged."""
import hashlib,io,json,pathlib,subprocess,tarfile
HERE=pathlib.Path(__file__).resolve().parent
ROOT=HERE.parents[2]
records=[]
for pin,revision in json.loads((HERE/'identity.json').read_text())['pins'].items():
 tree=HERE/'.exports'/pin
 assert tree.is_dir(), f'missing export: {tree}'
 checked=0;differences=[]
 with tarfile.open(fileobj=io.BytesIO(subprocess.check_output(['git','archive',revision],cwd=ROOT))) as tf:
  for m in tf:
   if not m.isfile():continue
   checked+=1;p=tree/m.name
   if not p.is_file() or hashlib.sha256(p.read_bytes()).digest()!=hashlib.sha256(tf.extractfile(m).read()).digest():differences.append(m.name)
 records.append({'pin':pin,'revision':revision,'files_checked':checked,'differences':differences})
(HERE/'export-verification.json').write_text(json.dumps(records,indent=2)+'\n')
print(json.dumps(records,indent=2))
assert not any(r['differences'] for r in records)
