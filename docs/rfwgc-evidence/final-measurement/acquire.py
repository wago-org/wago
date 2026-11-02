#!/usr/bin/env python3
"""Future acquisition executor. Refuses INVALID historical seals before any flock.
Not invoked during preparation. Owner must supply a new qualified seal and grant.
"""
import argparse,ctypes,fcntl,hashlib,json,os,pathlib,re,signal,subprocess,time,socket
H=pathlib.Path(__file__).resolve().parent
LOCKS=['/home/dev/Projects/wago/.performance-measurement.lock','/tmp/gc-trim-regate-laptop-timing.lock']
def sha(p):return hashlib.sha256(pathlib.Path(p).read_bytes()).hexdigest()
def check(cond,message):
 if not cond:raise RuntimeError(message)
def parse(raw,kind,expected,elapsed):
 check('FAIL' not in raw and '--- SKIP:' not in raw,'oracle failure/skip')
 if kind=='test-cycle':
  names=re.findall(r'^\s*--- PASS: (\S+)',raw,re.M);names=sorted(n for n in names if not any(x.startswith(n+'/') for x in names))
  check(names==sorted(expected) and bool(names),'changed/empty test leaf census')
  return {'complete-test-cycle':elapsed} # Leaf PASS census is a gate, not per-test latency.
 values={}
 for line in raw.splitlines():
  fields=line.split()
  if fields and fields[0].startswith('Benchmark') and 'ns/op' in fields:
   name=re.sub(r'-\d+$','',fields[0]);check(name not in values,'duplicate benchmark leaf')
   values[name]=float(fields[fields.index('ns/op')-1]);check(values[name]>0,'nonpositive result')
 check(sorted(values)==sorted(expected) and bool(values),'changed/empty benchmark leaf census')
 return values
def controls(samples,order):
 for side in 'AB':
  pair=[samples[i] for i,s in enumerate(order) if s==side]
  check(set(pair[0])==set(pair[1]),'repeat leaf mismatch')
  for leaf in pair[0]:check(.90<=pair[1][leaf]/pair[0][leaf]<=1.10,'repeat control outside [0.90,1.10]: '+leaf)
def census():
 result={}
 for p in pathlib.Path('/proc').iterdir():
  if not p.name.isdigit():continue
  try:
   text=(p/'stat').read_text();f=text[text.rfind(')')+2:].split();result[p.name]={'ppid':int(f[1]),'start':f[19],'ticks':int(f[11])+int(f[12])}
  except FileNotFoundError:continue
 return result
def telemetry(grant):
 sensors={p:int(pathlib.Path(p).read_text()) for p in grant['sensors_mC']}
 check(bool(sensors) and all(-20000<v<150000 for v in sensors.values()),'missing/invalid CPU sensors')
 throttles={p:int(pathlib.Path(p).read_text()) for p in grant['throttle_counters']}
 check(bool(throttles),'throttling visibility unqualified')
 policy={p:pathlib.Path(p).read_text().strip() for p in grant['fixed_policy_files']}
 return {'fixed_policy_files':policy,'sensors_mC':sensors,'throttle_counters':throttles,'processes':census(),'proc_stat':pathlib.Path('/proc/stat').read_text(),'cpu_pressure':pathlib.Path('/proc/pressure/cpu').read_text(),'frequency':{str(p):p.read_text().strip() for p in pathlib.Path('/sys/devices/system/cpu/cpufreq').glob('policy*/scaling_cur_freq')}}
def inspect(t,previous,grant,child=None):
 reasons=[]
 if t['fixed_policy_files']!=grant['fixed_policy_files']:reasons.append('power-or-sensor-identity-drift')
 if max(t['sensors_mC'].values())>=85000:reasons.append('overheat')
 if t['throttle_counters']!=previous['throttle_counters']:reasons.append('throttling-counter-change')
 allowed={int(pid) for pid in grant['allowed_processes']}|{os.getpid()}
 if child:allowed.add(child)
 changed=True
 while changed:
  new={int(pid) for pid,r in t['processes'].items() if r['ppid'] in allowed and (int(pid)==child or r['ppid'] not in {int(p) for p in grant['allowed_processes']})};changed=not new.issubset(allowed);allowed|=new
 for pid,r in t['processes'].items():
  if int(pid) not in allowed:reasons.append('foreign-process:'+pid);continue
  if pid in grant['allowed_processes']:
   check(r['start']==grant['allowed_processes'][pid]['start'],'allowed PID reused')
   old=previous['processes'].get(pid,r)
   # Explicitly approved idle services may consume at most two ticks per poll.
   if r['ticks']-old['ticks']>2:reasons.append('foreign-cpu:'+pid)
 return reasons
def main():
 ap=argparse.ArgumentParser();ap.add_argument('--seal',required=True,type=pathlib.Path);ap.add_argument('--grant',required=True,type=pathlib.Path);ap.add_argument('--out',required=True,type=pathlib.Path);a=ap.parse_args()
 seal=json.loads(a.seal.read_text());grant=json.loads(a.grant.read_text())
 check(seal['status']=='SEALED_CANDIDATE' and seal.get('candidate_valid') is True,'INVALID/unqualified candidate: repair and reseal first')
 check(json.loads((H/'candidate-status.json').read_text())['status']!='INVALID','candidate invalidation remains active')
 check(re.fullmatch(r'[0-9a-f]{40}',grant['protocol_commit']) is not None,'immutable protocol commit required')
 relative=str(a.seal.resolve().relative_to(H.parents[2]))
 committed=subprocess.check_output(['git','show',grant['protocol_commit']+':'+relative],cwd=H.parents[2])
 check(hashlib.sha256(committed).hexdigest()==sha(a.seal),'seal not committed at protocol commit')
 check(grant['seal_sha256']==sha(a.seal) and grant['hostname']==socket.gethostname(),'grant seal/host mismatch')
 check(grant['measurement_authorized'] is True and grant['authorization_reference'] and grant['independent_measurer'],'no owner authorization')
 for key in ['reviewer_and_worker_drain_receipts','host_namespace_and_remote_drain','lock_owner_reconciliation','power_policy_attestation','fixed_policy_files','sensor_identity_attestation','throttle_counters','sensors_mC']:check(bool(grant[key]),'missing owner attestation: '+key)
 check(0<grant['budget_seconds']<=14400,'one bounded window <=4 hours per grant; no silent resume')
 for p,d in seal['files'].items():check(sha(p)==d,'seal drift: '+p)
 for p,v in grant['fixed_policy_files'].items():check(pathlib.Path(p).read_text().strip()==v,'power/governor policy drift')
 check(set(grant['cpus']).issubset(os.sched_getaffinity(0)),'CPU affinity unavailable')
 a.out.mkdir();events=a.out/'events.jsonl'
 def log(x):
  with events.open('a') as f:f.write(json.dumps(x)+'\n');f.flush();os.fsync(f.fileno())
 descriptors=[];child=None;start=None
 def interrupted(signum,frame):raise KeyboardInterrupt('termination signal '+str(signum))
 signal.signal(signal.SIGTERM,interrupted)
 check(ctypes.CDLL(None,use_errno=True).prctl(36,1,0,0,0)==0,'cannot establish owned-child subreaper')
 try:
  for p in LOCKS:
   fd=os.open(p,os.O_RDWR);descriptors.append(fd);st=os.fstat(fd)
   check([st.st_dev,st.st_ino]==grant['lock_identities'][p],'lock inode changed')
   fcntl.flock(fd,fcntl.LOCK_EX|fcntl.LOCK_NB)
  start=time.monotonic();log({'event':'acquired','grant':grant,'seal_sha256':sha(a.seal)})
  baseline=telemetry(grant)
  check(not inspect(baseline,baseline,grant),'foreign workload at acquisition')
  for cell in seal['cells']:
   if not cell['paired_leaf_match'] or any(s['qualification']!='QUALIFIED_HISTORICAL' for s in cell['samples'].values()):
    log({'event':'unsupported','cell':cell['id'],'samples':cell['samples']});continue
   for block in range(6):
    order='ABBA' if block%2==0 else 'BAAB';accepted=False
    for attempt in range(3):
     results=[];reasons=[]
     for index,side in enumerate(order):
      check(time.monotonic()-start<grant['budget_seconds'],'window budget exhausted; incomplete, new grant required')
      deadline=time.monotonic()+300
      while True:
       before=telemetry(grant);noise=inspect(before,baseline,grant)
       if max(before['sensors_mC'].values())<=60000 and not noise:break
       log({'event':'admission-rejected','cell':cell['id'],'telemetry':before,'reasons':noise})
       check(time.monotonic()<deadline,'300-second admission exhausted');time.sleep(.25);baseline=before
      spec=cell['samples'][side];check(sha(spec['argv'][3])==spec['binary_sha256'],'binary drift')
      name=f"{seal['cells'].index(cell):03d}-{block}-{attempt}-{index}";path=a.out/(name+'.txt');polls=[];bad=[]
      log({'event':'sample-start','cell':cell['id'],'block':block,'attempt':attempt,'index':index,'side':side,'output':path.name,'spec':spec})
      with path.open('wb') as output:
       began=time.monotonic();child=subprocess.Popen(spec['argv'],cwd=spec['cwd'],env=spec['env'],stdout=output,stderr=subprocess.STDOUT,start_new_session=True)
       previous=before
       while True:
        try:child.wait(timeout=.25);finished=True
        except subprocess.TimeoutExpired:finished=False
        try:
         snap=telemetry(grant);bad+=inspect(snap,previous,grant,child.pid);polls.append(snap);previous=snap
        except (OSError,ValueError,RuntimeError) as e:bad.append('telemetry-loss:'+str(e))
        if finished:break
        if time.monotonic()-began>180:
         os.killpg(child.pid,signal.SIGKILL);child.wait();bad.append('process-timeout');break
       elapsed=time.monotonic()-began;code=child.returncode
       # Kill/reap owned helper group before releasing lease; any survivors invalidate.
       try:os.killpg(child.pid,0)
       except ProcessLookupError:pass
       else:os.killpg(child.pid,signal.SIGKILL);bad.append('owned-helper-survived')
       child=None
       # Reap orphaned helpers after subreaper adoption; never reap other owners.
       while True:
        try:os.waitpid(-1,0)
        except ChildProcessError:break
      if code:bad.append('process-error:'+str(code))
      try:value=parse(path.read_text(),cell['kind'],spec['expected_leaves'],elapsed)
      except RuntimeError as e:bad.append(str(e));value={}
      results.append(value);reasons+=bad;baseline=previous
      log({'event':'sample','cell':cell['id'],'block':block,'attempt':attempt,'index':index,'side':side,'output':path.name,'returncode':code,'elapsed_seconds':elapsed,'values':value,'reasons':bad,'before':before,'polls':polls})
     if not reasons:
      try:controls(results,order)
      except RuntimeError as e:reasons.append(str(e))
     log({'event':'block','cell':cell['id'],'block':block,'attempt':attempt,'order':order,'accepted':not reasons,'reasons':reasons,'values':results})
     if reasons and any(x in cell['id'] for x in ['/scalar','/noGC']):raise RuntimeError('scalar/noGC control failure invalidates segment for review')
     if not reasons:accepted=True;break
    check(accepted,'three attempts exhausted; incomplete')
  log({'event':'window-completed'})
 except BaseException as e:
  log({'event':'stopped-incomplete','reason':str(e)});raise
 finally:
  if child is not None:
   try:os.killpg(child.pid,signal.SIGKILL)
   except ProcessLookupError:pass
   child.wait()
  for fd in reversed(descriptors):fcntl.flock(fd,fcntl.LOCK_UN);os.close(fd)
  log({'event':'released','acquired':start is not None,'scope':'owned processes only; no global drain claim'})
if __name__=='__main__':main()
