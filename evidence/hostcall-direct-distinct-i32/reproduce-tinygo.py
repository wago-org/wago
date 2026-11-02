#!/usr/bin/env python3
"""Correctness-only exact-base cancellation repro; no source edits or timings."""
import hashlib,json,os,shutil,subprocess,tarfile,tempfile,time
from pathlib import Path
root=Path(__file__).resolve().parents[2]
out=Path(__file__).resolve().parent
base='90f2b57dafa7f8c3cc7e3aca38b91148f15e1d1e'
tree=root/'.candidate-baseline'
tree.mkdir()
tmp=Path(tempfile.mkdtemp(prefix='wago-direct-base-repro-'))
go_root=subprocess.check_output(['go','env','GOROOT'],cwd=root,text=True).strip()
tinygo=subprocess.check_output(['mise','which','tinygo'],cwd=root,text=True).strip()
env=dict(os.environ,GOROOT=go_root,PATH=str(Path(tinygo).parent)+':'+go_root+'/bin:'+os.environ['PATH'],TMPDIR=str(tmp))
archive=subprocess.Popen(['git','archive',base],cwd=root,stdout=subprocess.PIPE)
with tarfile.open(fileobj=archive.stdout,mode='r|') as tar:tar.extractall(tree,filter='data')
assert archive.wait()==0
captured=tmp/'baseline-runner'
command=['go','test','-count=1','-v','-run','^TestTinyGoSupportsCooperativeInterruption$','./cli/manager/internal/standalone']
try:
 with (out/'baseline-tinygo-capture.log').open('w') as log:
  process=subprocess.Popen(command,cwd=tree,env=env,stdout=log,stderr=subprocess.STDOUT)
  def descendants(pid):
   try:
    children=set()
    for listing in Path(f'/proc/{pid}/task').glob('*/children'):
     try:children.update(listing.read_text().split())
     except OSError:pass
   except OSError:return []
   result=[]
   for child in children:
    result.append(child);result.extend(descendants(child))
   return result
  while process.poll() is None:
   if not captured.exists():
    for pid in descendants(process.pid):
     try:
      executable=Path(f'/proc/{pid}/exe')
      target=os.readlink(executable)
      if target.startswith(str(tmp)) and target.endswith('/runner'):
       shutil.copyfile(executable,captured);captured.chmod(0o755);break
     except OSError:pass
   time.sleep(0.002)
  build_result=process.wait()
 result=dict(base=base,source_method='git archive into assigned checkout; no source changes',command=command,build_test_exit=build_result,captured=captured.exists(),note='Copy /proc/child/exe only after the exact-base test executes its generated runner; source and test unchanged.')
 if captured.exists():
  result['binary_sha256']=hashlib.sha256(captured.read_bytes()).hexdigest()
  result['attempt_limit']=1024
  with (out/'baseline-tinygo-stress.log').open('w') as log:
   for attempt in range(1,1025):
    run=subprocess.run([str(captured)],env=env,cwd=tmp,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=10)
    if run.returncode:
     result.update(attempts=attempt,exit_code=run.returncode)
     log.write(f'attempt={attempt} exit_code={run.returncode}\n'+run.stdout.decode(errors='replace'));break
   else:result.update(attempts=1024,exit_code=0)
 (out/'baseline-tinygo-stress.json').write_text(json.dumps(result,indent=2)+'\n')
finally:
 shutil.rmtree(tmp)
 shutil.rmtree(tree)
