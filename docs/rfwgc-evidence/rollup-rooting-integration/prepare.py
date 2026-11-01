#!/usr/bin/env python3
"""Export committed source plus owned rooting edits; never copy other workers' dirt."""
import hashlib,json,pathlib,subprocess,sys,tarfile,tempfile,io
root=pathlib.Path.cwd()
out=root/'docs/rfwgc-evidence/rollup-rooting-integration'
head=subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip()
dest=pathlib.Path(tempfile.mkdtemp(prefix='wago-rooting-gate-'))
with tarfile.open(fileobj=io.BytesIO(subprocess.check_output(['git','archive',head]))) as t:
 t.extractall(dest,filter='data')
manifest={}
for name in (out/'cluster-files.txt').read_text().splitlines():
 src=root/name; target=dest/name
 if not src.exists():
  target.unlink(missing_ok=True); manifest[name]=None; continue
 target.parent.mkdir(parents=True,exist_ok=True); target.write_bytes(src.read_bytes())
 manifest[name]=hashlib.sha256(src.read_bytes()).hexdigest()
(out/'snapshot.json').write_text(json.dumps({'head':head,'path':str(dest),'ownedSHA256':manifest},indent=2)+'\n')
print(dest)
