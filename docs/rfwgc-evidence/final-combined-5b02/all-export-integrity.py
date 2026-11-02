"""Verify every original blob in actual TinyGo and QEMU execution exports."""
import pathlib,json,hashlib
E=pathlib.Path(__file__).resolve().parent;M=json.loads((E/'source.json').read_text());W=pathlib.Path(M['scratch']);original=json.loads((E/'source-files-before.json').read_text());rows=[]
def sha(p):
 h=hashlib.sha256()
 with p.open('rb') as f:
  for b in iter(lambda:f.read(1048576),b''):h.update(b)
 return h.hexdigest()
exports=[W/'tinygo-source']+[pathlib.Path(json.loads((p/'source.json').read_text())['export']) for p in E.glob('qemu-*') if p.is_dir() and (p/'source.json').exists()]
for S in exports:
 for name,h in original.items():assert sha(S/name)==h,(S,name)
 added={str(p.relative_to(S)):sha(p) for p in (S/'src').rglob('*.go') if str(p.relative_to(S)) not in original}
 rows.append({'export':str(S),'original_blob_count':len(original),'original_blobs_unchanged':True,'additional_test_adapter_files':added})
(E/'all-export-integrity.json').write_text(json.dumps({'source':M['pin'],'exports':rows,'passed':True},indent=2)+'\n');print('Verified original blobs in',len(rows),'TinyGo/QEMU exports')
