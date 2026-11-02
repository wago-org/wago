#!/usr/bin/env python3
"""Focused immutable qualification; no benchmarks, submodules or working source."""
import hashlib, json, os, pathlib, subprocess, tempfile
repo = pathlib.Path(__file__).resolve().parents[3]
out = pathlib.Path(__file__).resolve().parent
pin = subprocess.check_output(['git','rev-parse','30b0813f7'],cwd=repo,text=True).strip()
root = pathlib.Path(tempfile.mkdtemp(prefix='wago-nonbackend-',dir='/home/dev'))
source = root/'source'; source.mkdir()
for name in ['cache','tmp','modcache']: (root/name).mkdir()
archive = subprocess.Popen(['git','archive',pin],cwd=repo,stdout=subprocess.PIPE)
subprocess.run(['tar','-x','-C',str(source)],stdin=archive.stdout,check=True)
archive.stdout.close(); assert archive.wait()==0
go='/home/dev/.local/share/mise/installs/go/1.27.1/bin/go'
qemu='/tmp/wago-pr994-tools/root/usr/bin/qemu-aarch64'
env=os.environ.copy()
for k in list(env):
    if k.startswith('WAGO_') or k in ['GOFLAGS','GOOS','GOARCH']: env.pop(k)
env['PATH']=str(pathlib.Path(go).parent)+os.pathsep+env['PATH']
env.update(GOROOT=str(pathlib.Path(go).parents[1]),GOTOOLCHAIN='local',GOCACHE=str(root/'cache'),GOTMPDIR=str(root/'tmp'),GOMODCACHE=str(root/'modcache'),CGO_ENABLED='0',GOMAXPROCS='4')
selector='^(TestExternrefGlobalHoldingConvertedGCReference|TestGCExternConversionAcrossReferenceTables|TestFixedSharedDirectGoImport|TestFixedSharedDirectGoCallerScope|TestFixedSharedDirectGoImportPolicy|TestSharedSingleEntryGuardAndGoFallback|TestSharedEntryRegisterResultAndGoFallback|TestSharedEntryRegisterArgumentAndGoFallback|TestSharedEntryGuardForeignFallback|TestSharedEntryGuardCancellationAndGC|TestSharedSingleNativePinsAndGoFallback)$'
report={'source':pin,'export':str(source),'go':subprocess.check_output(['go','version'],text=True).strip(),'qemu':qemu,'qemu_sha256':hashlib.sha256(pathlib.Path(qemu).read_bytes()).hexdigest(),'selector':selector,'cells':[]}
(out/'qualification.json').write_text(json.dumps(report,indent=2)+'\n')
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 for target in ['linux-amd64','linux-arm64','darwin-arm64']:
  goos,arch=target.split('-'); cell={'mode':mode,'target':target}; report['cells'].append(cell)
  binary=root/(target+'-'+mode+'.test')
  cmd=[go,'test','-c','-o',str(binary)]
  if mode!='ordinary':cmd+=['-tags',mode]
  cmd+=['./src/wago'];cell['build_command']=cmd
  with (out/(target+'-'+mode+'-build.log')).open('w') as f:
   cell['build_exit']=subprocess.run(cmd,cwd=source,env=dict(env,GOOS=goos,GOARCH=arch),stdout=f,stderr=subprocess.STDOUT).returncode
  if cell['build_exit']==0:
   cell['binary_sha256']=hashlib.sha256(binary.read_bytes()).hexdigest()
   if goos=='linux':
    cmd=([qemu] if arch=='arm64' else [])+[str(binary),'-test.v','-test.count=1','-test.timeout=180s','-test.run='+selector]
    cell['execution_command']=cmd
    with (out/(target+'-'+mode+'-test.log')).open('w') as f:
     cell['execution_exit']=subprocess.run(cmd,cwd=source/'src/wago',env=env,stdout=f,stderr=subprocess.STDOUT).returncode
   else:cell['execution']='NOT_RUN: Darwin compile only'
  (out/'qualification.json').write_text(json.dumps(report,indent=2)+'\n')
  print(json.dumps(cell),flush=True)
