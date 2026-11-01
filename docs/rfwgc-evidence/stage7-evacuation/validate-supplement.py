#!/usr/bin/env python3
import subprocess,pathlib,tempfile,tarfile,json,os
root=pathlib.Path.cwd();out=root/'docs/rfwgc-evidence/stage7-evacuation'
old=out/'capacity-fixture-correction';old.mkdir(exist_ok=True)
for p in list(out.glob('capacity-*.jsonl'))+list(out.glob('supplement-*.json')):p.rename(old/p.name)
pin=subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip();work=pathlib.Path(tempfile.mkdtemp(prefix='wago-stage7-capacity-'));archive=work/'source.tar'
with archive.open('wb') as f:subprocess.run(['git','archive',pin],stdout=f,check=True)
with tarfile.open(archive) as f:f.extractall(work,filter='data')
archive.unlink();(out/'supplement-source.json').write_text(json.dumps({'pin':pin,'export':str(work)},indent=2)+'\n')
results=[];go='/home/dev/.local/share/mise/installs/go/1.27.3/bin/go'
def run(name,mode,cmd,env):
 with (out/(name+'.jsonl')).open('w') as f:r=subprocess.run(cmd,cwd=work,env=env,stdout=f,stderr=subprocess.STDOUT)
 results.append({'mode':mode,'suite':name,'command':cmd,'exit':r.returncode});(out/'supplement-results.json').write_text(json.dumps(results,indent=2)+'\n');print(name,r.returncode,flush=True)
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 for race in [False,True]:
  cmd=[go,'test','-json','-count=1','-timeout=10m','./src/core/runtime/gc/native','-run','^TestEvacuationStage7(SingleProcessor|Capacity256MiB)$']+([] if mode=='ordinary' else ['-tags='+mode])+(['-race'] if race else [])
  run('capacity-'+mode+('-race' if race else ''),mode,cmd,dict(os.environ,GOMAXPROCS='4'))
 tags='wago_target_tinygo'+('' if mode=='ordinary' else ','+mode)
 cmd=[go,'test','-json','-count=1','-timeout=3m','-tags='+tags,'./src/core/runtime/gc/native','-run','TestEvacuationStage7DisabledTarget|TestParallelDisabled']
 run('disabled-'+mode,mode,cmd,dict(os.environ,GOMAXPROCS='4'))
for arch in ['arm64','riscv64']:
 cmd=[go,'test','-c','-o',str(work/('native-'+arch+'.test')),'./src/core/runtime/gc/native']
 run('cross-'+arch,'ordinary',cmd,dict(os.environ,GOMAXPROCS='4',GOARCH=arch,CGO_ENABLED='0'))
raise SystemExit(any(x['exit'] for x in results))
