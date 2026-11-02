"""Read-only source/dependency/tool/driver/receipt verification for this pin."""
import pathlib,json,hashlib,gzip,tarfile,subprocess
E=pathlib.Path(__file__).resolve().parent;M=json.loads((E/'source.json').read_text());W=pathlib.Path(M['scratch']);rows=[]
def sha(p):
 h=hashlib.sha256()
 with p.open('rb') as f:
  for b in iter(lambda:f.read(1048576),b''):h.update(b)
 return h.hexdigest()
allowed=json.loads((E/'baseline-graph-overlay.json').read_text()) if (E/'baseline-graph-overlay.json').exists() else {}
for label in ['source','tooling','baseline']:
 s=W/label;before=json.loads((E/(label+'-files-before.json')).read_text());changes=[]
 for n,h in before.items():
  p=s/n;actual=sha(p) if p.is_file() else None
  if actual!=h and not (label=='baseline' and allowed.get(n)==actual):changes.append(n)
 rows.append({'export':str(s),'checked':len(before),'changes':changes});assert not changes,(label,changes)
 if label!='baseline':
  for v in ['v1','v2','v3']:
   a=W/('spec-'+v+'.tar');assert sha(a)==M[v]['archive_sha256']
   with tarfile.open(a) as t:
    for f in t:
     if f.isfile():assert sha(s/'tests/conformance'/('spec-'+v)/f.name)==hashlib.sha256(t.extractfile(f).read()).hexdigest(),(label,v,f.name)
assert sha(W/'source.tar')==M['archive_sha256']
for name in ['tools.json','tinygo-runtime-before.json','extra-tools-before.json','drivers-frozen.json']:
 for n,h in json.loads((E/name).read_text()).items():
  p=E/n if name=='drivers-frozen.json' else pathlib.Path(n)
  assert sha(p)==h,(name,n)
receipts=[]
for cpath in sorted(E.glob('*/command.json')):
 d=cpath.parent;rpath=d/'result.json'
 if not rpath.exists():continue
 c=json.loads(cpath.read_text());r=json.loads(rpath.read_text());data=gzip.decompress((d/'raw.log.gz').read_bytes())
 assert r['raw_sha256']==hashlib.sha256(data).hexdigest() and r['driver_unchanged']
 assert c['driver_sha256']==sha(E/'run.py') and c['source']==r['source']
 receipts.append({'suite':d.name,'source':r['source'],'exit':r['exit'],'raw_sha256':r['raw_sha256']})
(E/'integrity-latest.json').write_text(json.dumps({'source':M['pin'],'exports':rows,'receipts':receipts,'passed':True},indent=2)+'\n');print('Integrity passed:',len(receipts),'terminal receipts')
