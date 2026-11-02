#!/usr/bin/env python3
"""Bounded correctness matrix; no benchmark or timing comparison."""
import hashlib,json,os,subprocess
from pathlib import Path
ROOT=Path('/home/dev/.cache/wago-final-codegen')
SOURCE=ROOT/'source'
EVIDENCE=Path(__file__).resolve().parent
LOGS=EVIDENCE/'logs';LOGS.mkdir(exist_ok=True)
PIN='d6f4e94fe671542d7e22fe84e8fe1f9358661c3a'
GO='/home/dev/.local/share/mise/installs/go/1.27.1/bin/go'
QEMU='/tmp/qemu-x/usr/bin/qemu-aarch64'
BACKEND='src/core/compiler/backend/railshot/arm64'
records=[]
env={k:v for k,v in os.environ.items() if not k.startswith('WAGO_')}
env.update(GOROOT=str(Path(GO).parent.parent),GOWORK='off',GOTOOLCHAIN='local',GOARCH='arm64',CGO_ENABLED='0',GOCACHE='/home/dev/.cache/wago-arm64-recovery')
def run(name,args,cwd,goos='linux'):
 e=env|{'GOOS':goos}
 log=LOGS/(name+'.log')
 with log.open('wb') as out:
  try: rc=subprocess.run(args,cwd=cwd,env=e,stdout=out,stderr=subprocess.STDOUT,timeout=300).returncode
  except subprocess.TimeoutExpired: rc=124
 records.append(dict(name=name,argv=list(map(str,args)),cwd=str(cwd),environment={k:e[k] for k in ['GOROOT','GOWORK','GOTOOLCHAIN','GOARCH','GOOS','CGO_ENABLED','GOCACHE']},exit=rc,log=str(log.relative_to(EVIDENCE))))
 (EVIDENCE/'commands.json').write_text(json.dumps(dict(source=PIN,commands=records),indent=2)+'\n')
 print(name,rc,flush=True)
 return rc
focused='^(TestNativeImportVariantRoutesARM64|TestDynamicImportUnwindLocalJoinARM64|TestNativeLeafContextShortcutGCPolicyARM64|TestTable.*Lowering|TestBulkPhysicalArgumentSlots|TestTableSetHelperCallHintsArm64|TestWorkerModuleStatsComparisonARM64|TestConstFoldPackExecArm64|TestSameOperandCompareExecArm64)$'
diag=(ROOT/'diagnostics-selector').read_text().replace(')$','|TestSharedEstablishedPathPairs|TestSharedScalarLeafMatchesEstablishedCodeArm64)$')
public='^(TestNativeImportVariantSourceArtifactAndCalls|TestNativeImportVariantTrap|TestNativeImportVariantRespectsPolicies|TestNativeImportVariantForeignFallback)$'
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 for lane,package,selector,extra in [('focused',BACKEND,focused,''),('diagnostics',BACKEND,diag,'wago_codegenstats'),('public','src/wago',public,'')]:
  tags=','.join(x for x in [mode if mode!='ordinary' else '',extra] if x)
  binary=ROOT/(mode+'-'+lane+'.test')
  name=mode+'-'+lane
  if run(name+'-build',[GO,'test','-tags='+tags,'-c','-o',str(binary),'./'+package],SOURCE)==0:
   run(name,[QEMU,str(binary),'-test.run',selector,'-test.v','-test.count=1','-test.timeout=120s'],SOURCE/package)
 for package,label in [(BACKEND,'backend'),('src/wago','public')]:
  tags=mode if mode!='ordinary' else ''
  run(mode+'-darwin-'+label+'-build',[GO,'test','-tags='+tags,'-c','-o',str(ROOT/(mode+'-darwin-'+label+'.test')),'./'+package],SOURCE,'darwin')
# Revert only the production router fix, retaining the new byte/execution oracle.
base=ROOT/'negative-compile.go'
base.write_bytes(subprocess.check_output(['git','show','58fbb7d4:src/core/compiler/backend/railshot/arm64/compile.go'],cwd=EVIDENCE))
overlay=ROOT/'negative-overlay.json';overlay.write_text(json.dumps({'Replace':{str(SOURCE/BACKEND/'compile.go'):str(base)}}))
binary=ROOT/'negative.test'
if run('negative-build',[GO,'test','-overlay='+str(overlay),'-c','-o',str(binary),'./'+BACKEND],SOURCE)==0:
 run('negative',[QEMU,str(binary),'-test.run','^TestNativeImportVariantRoutesARM64$/compact=true/pair=false/trap=false','-test.v'],SOURCE/BACKEND)
# Evidence hashes include outputs; binaries stay in the owned cache.
(EVIDENCE/'binary-hashes.json').write_text(json.dumps({p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in ROOT.glob('*.test')},indent=2)+'\n')
