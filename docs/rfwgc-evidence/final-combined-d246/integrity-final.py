"""Read-only final provenance verification; never promotes a missing gate."""
import pathlib,json,hashlib,tarfile,subprocess,gzip
E=pathlib.Path(__file__).resolve().parent;M=json.loads((E/'source.json').read_text());W=pathlib.Path(M['scratch'])
def sha(p):
 h=hashlib.sha256()
 with pathlib.Path(p).open('rb') as f:
  for b in iter(lambda:f.read(1048576),b''):h.update(b)
 return h.hexdigest()
rows=[]
for label in ['source','tooling','baseline']:
 before=json.loads((E/(label+'-files-before.json')).read_text());s=W/label
 changes=[dict(path=n,expected=h,actual=sha(s/n) if (s/n).is_file() else None) for n,h in before.items() if not (s/n).is_file() or sha(s/n)!=h]
 rows.append(dict(export=str(s),original_files=len(before),changes=changes))
for a in sorted(W.rglob('source.tar')):
 if a.parent==W:continue
 s=a.parent/'source';changes=[];count=0
 with tarfile.open(a) as t:
  for m in t:
   if not m.isfile():continue
   count+=1;p=s/m.name;want=hashlib.sha256(t.extractfile(m).read()).hexdigest();got=sha(p) if p.is_file() else None
   if got!=want:changes.append(dict(path=m.name,expected=want,actual=got))
 rows.append(dict(archive=str(a),archive_sha256=sha(a),export=str(s),original_files=count,changes=changes))
if (W/'tinygo-source').is_dir():
 s=W/'tinygo-source';before=json.loads((E/'source-files-before.json').read_text())
 rows.append(dict(export=str(s),original_files=len(before),changes=[dict(path=n,expected=h,actual=sha(s/n) if (s/n).is_file() else None) for n,h in before.items() if not (s/n).is_file() or sha(s/n)!=h]))
(E/'exports-integrity-after.json').write_text(json.dumps(rows,indent=2))
checks={}
for name in ['drivers-frozen.json','drivers-before.json','tools.json','tinygo-runtime-before.json','extra-tools-before.json']:
 before=json.loads((E/name).read_text());rs={}
 for n,value in before.items():
  h=value['sha256'] if isinstance(value,dict) else value;p=E/n if name.startswith('drivers') else pathlib.Path(n)
  rs[n]=dict(before=h,after=sha(p),unchanged=sha(p)==h)
 checks[name]=rs
(E/'provenance-integrity-after.json').write_text(json.dumps(checks,indent=2))
state=json.loads((E/'shared-state-before.json').read_text());index=subprocess.check_output(['git','ls-files','--stage','-z'],cwd=E.parents[2]).decode();current_files={n:sha(E.parents[2]/n) for n in state['files']}
(E/'shared-state-after.json').write_text(json.dumps(dict(files=current_files,files_unchanged=current_files==state['files'],index_unchanged=index==state['index'],cached_diff=subprocess.check_output(['git','diff','--cached','--binary'],cwd=E.parents[2]).decode()),indent=2))
assert all(x['unchanged'] for rs in checks.values() for x in rs.values()),'Provenance changed'
# Baseline graph overlays are the sole allowed replacement of original files.
allowed=json.loads((E/'baseline-graph-overlay.json').read_text()) if (E/'baseline-graph-overlay.json').exists() else {}
unexpected=[(r['export'],c) for r in rows for c in r['changes'] if not (r['export']==str(W/'baseline') and allowed.get(c['path'])==c['actual'])]
assert not unexpected,unexpected
print('Original exports and frozen tool/driver hashes verified')
