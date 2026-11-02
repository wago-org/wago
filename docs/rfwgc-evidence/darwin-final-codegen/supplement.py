#!/usr/bin/env python3
import json,os,subprocess
from pathlib import Path
E=Path(__file__).resolve().parent
ROOT=Path('/home/dev/.cache/wago-final-codegen');SOURCE=ROOT/'source'
GO='/home/dev/.local/share/mise/installs/go/1.27.1/bin/go';QEMU='/tmp/qemu-x/usr/bin/qemu-aarch64'
pkg='src/core/compiler/backend/railshot/arm64'
env={k:v for k,v in os.environ.items() if not k.startswith('WAGO_')}
env.update(GOROOT=str(Path(GO).parent.parent),GOWORK='off',GOTOOLCHAIN='local',GOARCH='arm64',CGO_ENABLED='0',GOCACHE='/home/dev/.cache/wago-arm64-recovery')
records=[]
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 tags=','.join(x for x in [mode if mode!='ordinary' else '', 'wago_codegenstats'] if x)
 for lane,args,goos,cwd in [
 ('worker-diagnostics',[QEMU,str(ROOT/(mode+'-diagnostics.test')),'-test.run','^TestCompileWorkers(DeterministicArm64|SizeSharedAdaptersDeterministicArm64)$/^synthetic/','-test.v','-test.timeout=120s'],'linux',SOURCE/pkg),
 ('darwin-diagnostics-build',[GO,'test','-tags='+tags,'-c','-o',str(ROOT/(mode+'-darwin-diagnostics.test')),'./'+pkg],'darwin',SOURCE)]:
  name=mode+'-'+lane; log=E/'logs'/(name+'.log');e=env|{'GOOS':goos}
  with log.open('wb') as f:rc=subprocess.run(args,cwd=cwd,env=e,stdout=f,stderr=subprocess.STDOUT,timeout=180).returncode
  records.append(dict(name=name,argv=args,cwd=str(cwd),environment={k:e[k] for k in ['GOROOT','GOWORK','GOTOOLCHAIN','GOARCH','GOOS','CGO_ENABLED','GOCACHE']},exit=rc,log=str(log.relative_to(E))))
  print(name,rc,flush=True)
(E/'supplement-commands.json').write_text(json.dumps({'source':'d6f4e94fe671542d7e22fe84e8fe1f9358661c3a','commands':records},indent=2)+'\n')
