"""Read-only owned-process inventory; no PID-based killing."""
import os,pathlib,json
E=pathlib.Path(__file__).resolve().parent;roots=[json.loads((E/'source.json').read_text())['scratch'],str(E)]
ancestors=set();pid=os.getpid()
while pid>1:
 ancestors.add(pid)
 try:pid=int(pathlib.Path('/proc',str(pid),'stat').read_text().rsplit(')',1)[1].split()[1])
 except OSError:break
rows=[]
for p in pathlib.Path('/proc').glob('[0-9]*'):
 if int(p.name) in ancestors:continue
 try:
  args=(p/'cmdline').read_bytes().replace(b'\0',b' ').decode(errors='replace');cwd=str((p/'cwd').resolve());stat=(p/'stat').read_text().rsplit(')',1)[1].split()
  if any(r in args or cwd.startswith(r) for r in roots):rows.append(dict(pid=int(p.name),ppid=int(stat[1]),state=stat[0],start_ticks=stat[19],command=args,cwd=cwd))
 except OSError:pass
(E/'process-drain.json').write_text(json.dumps(dict(owned_roots=roots,processes=rows,drained=not rows,excluded_inspection_ancestors=sorted(ancestors)),indent=2)+'\n')
assert not rows,rows
print('Owned processes drained')
