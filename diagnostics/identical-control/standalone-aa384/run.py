#!/usr/bin/env python3
"""Reconstructed fresh-process public Compile controller; no memory sampling."""
import fcntl,traceback,argparse,gzip,hashlib,json,os,pathlib,random,subprocess,time
import shutil,tempfile,sys
from prepare import KIT, read_checked, require
p=argparse.ArgumentParser(description=__doc__)
p.add_argument('--manifest',required=True,type=pathlib.Path)
p.add_argument('--manifest-sha256',required=True)
p.add_argument('--new-output-directory',required=True,type=pathlib.Path)
p.add_argument('--execute-local-aa384',action='store_true',required=True)
a=p.parse_args()
if sys.flags.optimize:
 p.error('do not use python -O')
m=read_checked(a.manifest,a.manifest_sha256)
pinned_manifest=a.manifest.read_bytes()
require(hashlib.sha256(pinned_manifest).hexdigest()==a.manifest_sha256,'manifest changed after preparation')
root=a.new_output_directory.resolve()
root.mkdir(mode=0o700,exist_ok=False)
(root/'path-controls').mkdir(mode=0o700)
(root/'tmp').mkdir(mode=0o700)
(root/'PRELAUNCH.json').write_bytes(pinned_manifest)
a.cpu=m['cpu'];a.out=str(root/'records.jsonl')
arms=[{'name':label,'binary':m['binary'],'env':json.loads((KIT/'flags.json').read_text())} for label in ('A','B')]
workloads=json.loads((KIT/'inputs.json').read_text())
rng=random.Random(902173);jobs=[]
for epoch in range(3):
 blocks=[(w,stratum,block,'ABBA' if block<2 else 'BAAB') for w in workloads for stratum in ['rewrite','no-rewrite'] for block in range(4)];rng.shuffle(blocks)
 for block_index,(w,stratum,block,sequence) in enumerate(blocks):
  for position,label in enumerate(sequence):jobs.append((epoch*4+block,w,arms[label=='B'],{'epoch':epoch,'block_index':block_index,'within_cell_block':block,'stratum':stratum,'sequence':sequence,'position':position+1}))
assert len(jobs)==384
lock=(root/'run.lock').open('a');fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
assert not pathlib.Path(a.out).exists() and not (root/'journal.jsonl').exists()
journal=(root/'journal.jsonl').open('x')
def event(stage,**kw):journal.write(json.dumps({'stage':stage,'wall_ns':time.time_ns(),'monotonic_ns':time.monotonic_ns(),**kw})+'\n');journal.flush();os.fsync(journal.fileno())
event('start',frozen_sha256=a.manifest_sha256,expected=384)
payloads={arm['binary']:pathlib.Path(arm['binary']).read_bytes() for arm in arms}
require(hashlib.sha256(next(iter(payloads.values()))).hexdigest()==m['binary_sha256'],'binary changed after preparation')
for arm in arms:arm['binary_sha256']=hashlib.sha256(payloads[arm['binary']]).hexdigest()
slot=root/'path-controls'/'fixed-slot'
if not slot.exists():
 with slot.open('xb') as f: f.write(b'')
 slot.chmod(0o700)
slot_identity=(slot.stat().st_dev,slot.stat().st_ino)
assert len(payloads)==1
with slot.open('r+b') as f:f.write(next(iter(payloads.values())));f.truncate(len(next(iter(payloads.values()))))
event('slot_initialized',device=slot_identity[0],inode=slot_identity[1],bytes=len(next(iter(payloads.values()))))

def run(t,w,arm,meta):
 env={'PATH':os.defpath,'HOME':str(root/'tmp'),'LANG':'C','TMPDIR':str(root/'tmp'),'GOMAXPROCS':'1'};env.update(arm.get('env',{}))
 data=payloads[arm['binary']]
 preparation_start_ns=time.time_ns();rewrite_bytes=0
 if meta['stratum']=='rewrite':
  with slot.open('r+b') as f: f.write(data);f.truncate(len(data))
  rewrite_bytes=len(data)
 preparation_write_end_ns=time.time_ns()
 st=slot.stat();assert (st.st_dev,st.st_ino)==slot_identity
 actual_sha=hashlib.sha256(slot.read_bytes()).hexdigest();assert actual_sha==arm['binary_sha256']
 slot_receipt={'device':st.st_dev,'inode':st.st_ino,'size':st.st_size,'mtime_ns':st.st_mtime_ns,'sha256':actual_sha}
 preparation_hash_end_ns=time.time_ns();launched_ns=time.time_ns();phase_stats=[]
 errfile=tempfile.TemporaryFile(dir=root/'tmp', mode='w+t')
 proc=subprocess.Popen([m['taskset'],'-c',str(a.cpu),str(slot)],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=errfile,text=True,encoding='utf-8',env=env,pass_fds=(lock.fileno(),));reqs=[];responses=[];acks=[]
 def read_error():
  errfile.seek(0);return errfile.read()
 def observe(stage):
  stat=pathlib.Path(f'/proc/{proc.pid}/stat').read_text().rsplit(')',1)[1].split()
  info={'wall_ns':time.time_ns(),'monotonic_ns':time.monotonic_ns(),'stage':stage,'minor_faults':int(stat[7]),'major_faults':int(stat[9]),'user_ticks':int(stat[11]),'system_ticks':int(stat[12]),'affinity':sorted(os.sched_getaffinity(proc.pid))}
  require(info['affinity']==[a.cpu], 'child affinity mismatch')
  for line in pathlib.Path(f'/proc/{proc.pid}/status').read_text().splitlines():
   if line.startswith(('voluntary_ctxt_switches:','nonvoluntary_ctxt_switches:')):
    k,v=line.split();info[k.rstrip(':')]=int(v)
  try:info['schedstat']=[int(v) for v in pathlib.Path(f'/proc/{proc.pid}/schedstat').read_text().split()]
  except (OSError,ValueError) as e:info['schedstat_unavailable']=str(e)
  try:info['host_cpu']=[int(v) for line in pathlib.Path('/proc/stat').read_text().splitlines() if line.startswith(f'cpu{a.cpu} ') for v in line.split()[1:]]
  except (OSError,ValueError) as e:info['host_cpu_unavailable']=str(e)
  phase_stats.append(info)
 def send(req):proc.stdin.write(json.dumps(req)+'\n');proc.stdin.flush()
 def query(req):
  reqs.append(req);send(req)
  while True:
   line=proc.stdout.readline()
   if not line:raise RuntimeError('adapter EOF '+w['id']+' '+read_error())
   r=json.loads(line);require(r.get('id')==req['id'], 'response ID mismatch');responses.append(r)
   if r['status']=='phase':
    observe(r['phase']['stage'])
    ack={'version':1,'id':req['id'],'method':'continue'};acks.append(ack);send(ack);continue
   if r['status']!='ok':raise RuntimeError(str(r))
   return r
 try:
  query({'version':1,'id':1,'method':'describe'})
  query({'version':1,'id':2,'method':'prepare','prepare':{'artifact':str(pathlib.Path(m['corpus'])/w['artifact']),'artifact_sha256':w['sha256'],'workload':w,'profile':'timing'}})
  r=query({'version':1,'id':3,'method':'run','run':{'scenario':'compile','samples':1,'operations':1,'warmup':0,'phase_barriers':True}})
  assert len(r['samples'])==1 and r['samples'][0]['verified'];assert [x['phase']['stage'] for x in responses if x['status']=='phase']==['before_compile','compiled','released']
  query({'version':1,'id':4,'method':'close'});proc.stdin.close();proc.wait();err=read_error();assert proc.returncode==0
  return {'diagnostic':meta,'preparation':{'start_wall_ns':preparation_start_ns,'write_end_wall_ns':preparation_write_end_ns,'hash_end_wall_ns':preparation_hash_end_ns,'rewrite_bytes':rewrite_bytes},'effective_env':env,'launch_wall_ns':launched_ns,'slot':slot_receipt,'phase_process_stats':phase_stats,'trial':t,'id':w['id'],'variant':arm['name'],'binary_sha256':arm['binary_sha256'],'artifact_sha256':w['sha256'],'env':arm.get('env',{}),'cpu':a.cpu,'gomaxprocs':1,'requests':reqs,'responses':responses,'acks':acks,'contracts_file_sha256':hashlib.sha256((KIT/'inputs.json').read_bytes()).hexdigest(),'compile_scope':'first public Compile after preparation; oracle after timer; no heap telemetry','verified':True,'elapsed_ns':r['samples'][0]['elapsed_ns'],'stderr':err}
 finally:
  if proc.poll() is None:proc.kill();proc.wait()
  errfile.close()
rows=[];previous_epoch=0
with open(a.out,'x') as f:
 for i,(trial,w,arm,meta) in enumerate(jobs):
  if meta['epoch']!=previous_epoch:
   event('epoch_pause_start',epoch=meta['epoch']);time.sleep(5);event('epoch_pause_end',epoch=meta['epoch']);previous_epoch=meta['epoch']
  event('child_start',index=i,id=w['id'],label=arm['name'],**meta)
  try:r=run(trial,w,arm,meta)
  except BaseException as e:event('failure',index=i,error=repr(e),traceback=traceback.format_exc());raise
  rows.append(r);f.write(json.dumps(r)+'\n');f.flush();os.fsync(f.fileno());event('child_complete',index=i,verified=r['verified'])
assert len(rows)==384 and all(x['verified'] for x in rows)
with open(a.out,'rb') as src,gzip.open(a.out+'.gz','xb') as dst:
 import shutil;shutil.copyfileobj(src,dst)
event('complete',verified=384,raw_sha256=hashlib.sha256(pathlib.Path(a.out).read_bytes()).hexdigest(),gzip_sha256=hashlib.sha256(pathlib.Path(a.out+'.gz').read_bytes()).hexdigest())
