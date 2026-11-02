#!/usr/bin/env python3
"""Terminate only this qualification's active execution trees after unsafe findings."""
import pathlib,os,signal,json
E=pathlib.Path(__file__).resolve().parent
proc={}
for p in pathlib.Path('/proc').iterdir():
 if not p.name.isdigit():continue
 try:
  args=(p/'cmdline').read_bytes().split(b'\0');args=[x.decode(errors='replace') for x in args if x]
  stat=(p/'stat').read_text();ppid=int(stat[stat.rfind(')')+2:].split()[1]);proc[int(p.name)]={'args':args,'parent':ppid}
 except (OSError,ValueError):pass
names={'extra.py','run.py','lanes.py','remaining.py','qemu-guest.py','default-diagnostics-complete.py','tinygo-34.sh','graphs.py'}
roots=[]
for pid,d in proc.items():
 if len(d['args'])>1 and pathlib.Path(d['args'][1]).name in names and 'combined-store-hook-gates/' in d['args'][1]:roots.append(pid)
def descendants(pid):
 ds=[]
 for child,d in proc.items():
  if d['parent']==pid:ds+=[child]+descendants(child)
 return ds
records=[]
for pid in roots:
 if any(pid in descendants(other) for other in roots if other!=pid):continue
 ds=descendants(pid)
 records.append({'root':pid,'root_command':proc[pid]['args'],'descendants':[{'pid':x,**proc[x]} for x in ds]})
 # Stop orchestrator first to forbid launching another command during teardown.
 try:os.kill(pid,signal.SIGSTOP)
 except ProcessLookupError:continue
 for child in reversed(ds):
  try:os.kill(child,signal.SIGTERM)
  except ProcessLookupError:pass
 # Driver is deliberately terminated, so no child after unsafe boundary is launched.
 try:os.kill(pid,signal.SIGKILL)
 except ProcessLookupError:pass
unfinished=[str(f.parent.relative_to(E)) for f in E.rglob('command.json') if not (f.parent/'result.json').exists()]
(E/'safety-stop.json').write_text(json.dumps({'reason':'Stop combined-source execution following race in TestPreparedGCRevocableEntryExternalAccess/reference-store and regalloccheck deferred-load overwrite in explicitly enabled TestRFWStage3CorpusCompile/clang-cpp-kernels. No failed source is repaired or waived.','processes':records,'unfinished_command_directories':unfinished},indent=2)+'\n')
print('Stopped',len(records),'owned execution roots; unfinished commands:',unfinished)
