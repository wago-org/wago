"""Inventory only owned processes and artifacts; never touch unrelated work."""
import pathlib,json,os,hashlib,gzip,shutil,sys
E=pathlib.Path(__file__).resolve().parent;M=json.loads((E/'source.json').read_text());W=pathlib.Path(M['scratch']);owned=[str(W),str(E)]
# Finish the passive safety watcher; this does not signal any runtime process.
(E/'WORK-DRAINED').write_text('Qualification commands completed; passive watcher may exit.\n')
groups={json.loads(f.read_text())['pid'] for f in E.glob('*/process.json')}
procs=[]
for p in pathlib.Path('/proc').iterdir():
 if not p.name.isdigit() or int(p.name)==os.getpid():continue
 try:
  stat=(p/'stat').read_text();group=int(stat[stat.rfind(')')+2:].split()[2])
  args=(p/'cmdline').read_bytes().replace(b'\0',b' ').decode(errors='replace');cwd=str((p/'cwd').resolve());exe=str((p/'exe').resolve())
 except (OSError,ValueError):continue
 if group in groups or any(x in args or cwd.startswith(x) or exe.startswith(x) for x in owned):
  # This report and its shell wrapper are not test runtime load.
  if 'drain.py' in args or 'safety-watch.py' in args:continue
  procs.append(dict(pid=int(p.name),group=group,command=args,cwd=cwd,exe=exe))
result=dict(source=M['pin'],owned_runtime_processes=procs,drained=not procs,scope='Only this qualification; no assertion about other workers or a measurement lease.')
(E/'process-drain.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result,indent=2))
if procs or '--cleanup' not in sys.argv:raise SystemExit(bool(procs))
# Capture every generated file identity before deleting the recorded private export/cache root.
entries=[]
for p in sorted(W.rglob('*')):
 if p.is_file() and not p.is_symlink():
  h=hashlib.sha256()
  with p.open('rb') as f:
   for block in iter(lambda:f.read(1024*1024),b''):h.update(block)
  entries.append(dict(path=str(p.relative_to(W)),size=p.stat().st_size,sha256=h.hexdigest()))
with gzip.open(E/'owned-artifact-inventory.json.gz','wt') as f:json.dump(entries,f)
cache=E/'__pycache__';paths=json.loads((E/'owned-paths.json').read_text());assert paths==[str(W)]
removed=[]
for p in [W,cache]:
 if p.exists():shutil.rmtree(p);removed.append(str(p))
(E/'cleanup.json').write_text(json.dumps(dict(recorded_exports=paths,owned_python_cache=str(cache),removed=removed,remaining=[p for p in removed if pathlib.Path(p).exists()],inventory='owned-artifact-inventory.json.gz'),indent=2)+'\n')
