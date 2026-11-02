"""Stop only recorded qualification process groups on a detected unsafe invariant."""
import pathlib,json,os,signal,time
E=pathlib.Path(__file__).resolve().parent
(E/'safety-watch-pid.json').write_text(json.dumps({'pid':os.getpid()}))
while not (E/'WORK-DRAINED').exists():
 if (E/'SAFETY-STOP.json').exists():
  rows=[]
  for f in E.glob('*/process.json'):
   if (f.parent/'result.json').exists():continue
   pid=json.loads(f.read_text())['pid']
   try:os.killpg(pid,signal.SIGTERM);rows.append({'gate':f.parent.name,'pid':pid,'signal':'SIGTERM'})
   except ProcessLookupError:pass
  (E/'safety-stop-processes.json').write_text(json.dumps(rows,indent=2))
  break
 time.sleep(.5)
