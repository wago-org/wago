#!/usr/bin/env python3
"""Verify every archived source member after qualification, before cleanup."""
from pathlib import Path
import hashlib,io,json,os,subprocess,sys,tarfile
root=Path(__file__).resolve().parents[3]
out=Path(sys.argv[1]).resolve(); record=json.loads((out/'source.json').read_text())
source=Path(record['source'])
archive=subprocess.check_output(['git','archive',record['head']],cwd=root)
assert hashlib.sha256(archive).hexdigest()==record['archive_sha256']
count=0
with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
 for m in tar:
  path=source/m.name
  if m.isfile():
   assert path.read_bytes()==tar.extractfile(m).read(), m.name
   count+=1
  elif m.issym():
   assert os.readlink(path)==m.linkname,m.name
   count+=1
(out/'all-export-unchanged.json').write_text(json.dumps({'head':record['head'],'verified_members':count,'result':'PASS'},indent=2)+'\n')
print('PASS',count,'archived source members unchanged')
