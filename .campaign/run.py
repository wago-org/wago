#!/usr/bin/env python3
import fcntl,sys,subprocess,pathlib,time,os,json,threading
root=pathlib.Path(__file__).parent
kind=sys.argv[1]
if kind not in ('build','bench','remote-bench'): raise SystemExit('run.py build|bench|remote-bench COMMAND...')
if kind!='build':
 serial=(root/'locks/benchmark-session').open('w');fcntl.flock(serial,fcntl.LOCK_EX)
# Reserve a build slot before entering the build gate. Queued builders must
# not hold reader locks while waiting for a slot.
if kind in ('build','remote-bench'):
 while True:
  for n in range(2):
   slot=(root/f'locks/build{n}').open('w')
   try: fcntl.flock(slot,fcntl.LOCK_EX|fcntl.LOCK_NB); break
   except BlockingIOError: slot.close()
  else: time.sleep(1); continue
  break
if kind!='remote-bench':
 turn=(root/'locks/build-admission').open('w');fcntl.flock(turn,fcntl.LOCK_EX)
 gate=(root/'locks/gate').open('w');fcntl.flock(gate,fcntl.LOCK_EX if kind=='bench' else fcntl.LOCK_SH)
 if kind=='build':turn.close()
def external_go():
 rows=subprocess.check_output(['ps','-eo','pid,comm,args'],text=True).splitlines()
 return [r.strip() for r in rows if len(r.split(None,2))==3 and r.split(None,2)[1] in ('go','compile','link')]
if kind=='bench':
 while external_go():
  print('benchmark waits for external Go processes: '+json.dumps(external_go()),file=sys.stderr,flush=True)
  time.sleep(10)
stop=threading.Event()
if kind!='build':
 sessions=root/'evidence'/'sessions';sessions.mkdir(parents=True,exist_ok=True)
 log=sessions/f'{kind}-{os.getpid()}.jsonl'
 def sample():
  with log.open('w') as f:
   while not stop.is_set():
    rows=subprocess.check_output(['ps','-eo','pid,pcpu,comm'],text=True).splitlines()[1:]
    rows=[r.strip() for r in rows if len(r.split())==3 and (float(r.split()[1])>=2 or r.split()[2] in ('go','compile','link'))]
    f.write(json.dumps({'time':time.time(),'processes':rows})+'\n');f.flush()
    stop.wait(2)
 threading.Thread(target=sample,daemon=True).start()
 print(f'process samples: {log}',file=sys.stderr,flush=True)
print(f'campaign {kind} start pid={os.getpid()} cwd={os.getcwd()}',file=sys.stderr,flush=True)
code=subprocess.call(sys.argv[2:]);stop.set();raise SystemExit(code)
