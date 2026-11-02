"""Capture immutable original export and driver integrity before any cleanup."""
import pathlib,json,hashlib,tarfile,subprocess
E=pathlib.Path(__file__).resolve().parent;M=json.loads((E/'source.json').read_text());W=pathlib.Path(M['scratch'])
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
rows=[]
for a in sorted(W.rglob('*.tar')):
 if a.name.startswith('spec-'):continue
 s=a.parent/a.stem if a.parent==W else a.parent/'source'
 if a.name=='source.tar' and a.parent==W and (W/'tinygo-source').exists():pass
 if not s.is_dir():continue
 changes=[];count=0
 with tarfile.open(a) as t:
  for m in t:
   if not m.isfile():continue
   count+=1;p=s/m.name;want=hashlib.sha256(t.extractfile(m).read()).hexdigest()
   got=sha(p) if p.exists() else None
   if got!=want:changes.append(dict(path=m.name,expected=want,actual=got))
 rows.append(dict(archive=str(a),export=str(s),archive_sha256=sha(a),files=count,changes=changes))
# TinyGo shell archive is streamed: compare directly to the recorded candidate manifest.
s=W/'tinygo-source'
if s.exists():
 before=json.loads((E/'source-files-before.json').read_text());changes=[dict(path=n,expected=h,actual=sha(s/n) if (s/n).exists() else None) for n,h in before.items() if not (s/n).exists() or sha(s/n)!=h]
 rows.append(dict(export=str(s),files=len(before),changes=changes))
# Verify pinned fixture files independently of main Gitlink directory entries.
for a in sorted(W.glob('spec-*.tar')):
 v=a.stem.removeprefix('spec-')
 for label in ['source','tooling']+(['tinygo-source'] if v=='v3' else []):
  s=W/label/('tests/conformance/spec-'+v)
  if not s.exists():continue
  changes=[];count=0
  with tarfile.open(a) as t:
   for m in t:
    if not m.isfile():continue
    count+=1;p=s/m.name;want=hashlib.sha256(t.extractfile(m).read()).hexdigest();got=sha(p) if p.exists() else None
    if got!=want:changes.append(dict(path=m.name,expected=want,actual=got))
  rows.append(dict(archive=str(a),export=str(s),archive_sha256=sha(a),files=count,changes=changes))
(E/'exports-integrity-after.json').write_text(json.dumps(rows,indent=2)+'\n')
drivers={}
for f in E.glob('*before.json'):
 if 'driver' not in f.name:continue
 for n,h in json.loads(f.read_text()).items():drivers[n]=dict(before=h,after=sha(E/n),unchanged=h==sha(E/n))
p=E/'powershell-tool.json'
if p.exists():
 h=json.loads(p.read_text())['driver_sha256'];drivers['root-replay.py']=dict(before=h,after=sha(E/'root-replay.py'),unchanged=h==sha(E/'root-replay.py'))
command_driver=[]
for f in E.glob('*/command.json'):
 c=json.loads(f.read_text());h=c.get('driver_sha256')
 if h:command_driver.append(dict(command=str(f.relative_to(E)),recorded=h,unchanged=h in {sha(E/n) for n in ['run.py','run-attempt1.py','resume-run.py']}))
(E/'command-driver-integrity.json').write_text(json.dumps(command_driver,indent=2)+'\n')
tools=json.loads((E/'tools.json').read_text());after={p:sha(pathlib.Path(p)) for p in tools}
(E/'tools-integrity-after.json').write_text(json.dumps(dict(before=tools,after=after,unchanged=tools==after),indent=2)+'\n')
(E/'drivers-integrity-after.json').write_text(json.dumps(drivers,indent=2)+'\n')
(E/'worktree-after.txt').write_text(subprocess.check_output(['git','status','--porcelain=v1'],cwd=E.parents[2],text=True))
(E/'index-after.txt').write_text(subprocess.check_output(['git','diff','--cached','--raw'],cwd=E.parents[2],text=True))
print('exports',len(rows),'changed originals',sum(len(r['changes']) for r in rows),'drivers unchanged',all(r['unchanged'] for r in drivers.values()))
