#!/usr/bin/env python3
"""Summarize completed ledgers; never reinterpret a missing gate as a pass."""
import hashlib,json,pathlib,tarfile
root=pathlib.Path(__file__).resolve().parent
summary={}
for name in ['admission-90fac4d60','slots-3cd7e60a9']:
 p=root/name
 summary[name]=json.loads((p/'driver-final.json').read_text())
for name in ['integrated-5156fcc29','targets-5156fcc29','tinygo-5156fcc29','integrated-retirement-fixed','targets-10c2006fb','tinygo-10c2006fb']:
 p=root/name
 rows=json.loads((p/'results.json').read_text())
 final=json.loads((p/'driver-final.json').read_text())
 summary[name]={'driver':final,'commands':len(rows),'nonzero':[r for r in rows if r['exit']!=0],'skips':[{ 'suite':r.get('label',r.get('mode')), 'skips':r.get('skip',r.get('skips',[])) } for r in rows if r.get('skip',r.get('skips',[]))]}
 # Compare original archive members; QEMU's disclosed test adapter/probe are
 # additional files. Existing source members must be byte-identical.
 meta=json.loads((p/('source.json' if name.startswith('targets') else 'provenance.json')).read_text())
 src=pathlib.Path(meta.get('export',meta.get('source')))
 archive=src.parent/'source.tar' if name.startswith(('targets','integrated')) else src/'source.tar'
 mismatches=[];count=0
 with tarfile.open(archive) as tf:
  for item in tf:
   if not item.isfile():continue
   count+=1;target=src/item.name
   if not target.is_file() or target.read_bytes()!=tf.extractfile(item).read():mismatches.append(item.name)
 (p/'export-verification.json').write_text(json.dumps({'pin':meta['pin'],'archive_members':count,'mismatches':mismatches},indent=2)+'\n')
 summary[name]['archive_members_unchanged']=not mismatches
(root/'summary.json').write_text(json.dumps(summary,indent=2)+'\n')
lines=[]
for p in sorted(root.rglob('*')):
 if p.is_file() and p.name!='SHA256SUMS':lines.append(hashlib.sha256(p.read_bytes()).hexdigest()+'  '+str(p.relative_to(root)))
(root/'SHA256SUMS').write_text('\n'.join(lines)+'\n')
