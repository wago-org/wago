#!/usr/bin/env python3
"""Exact-pin preparation/native qualification. Never execute cross artifacts."""
import argparse, collections, hashlib, json, os, platform, re, signal, subprocess, sys, tarfile
from pathlib import Path
E=Path(__file__).resolve().parent
PIN=json.loads((E/'toolchain.json').read_text())
MODES=('ordinary','wagodebug','wago_regalloccheck')
def sha(p): return hashlib.sha256(Path(p).read_bytes()).hexdigest()
def write(p,v): Path(p).write_text(json.dumps(v,indent=2,sort_keys=True)+'\n')
def files(root): return {str(p.relative_to(root)):sha(p) for p in root.rglob('*') if p.is_file()}
def capture(argv,cwd=None): return subprocess.check_output(argv,cwd=cwd,text=True,timeout=60).strip()
def check_hashes(root,expected):
 for n,h in expected.items():
  if sha(root/n)!=h: raise ValueError('identity mismatch: '+n)
def safe_output(path):
 # Private Go sources/toolchains must never become packages in root ./... .
 repo=Path(capture(['git','rev-parse','--show-toplevel'],E))
 try: relative=path.resolve().relative_to(repo)
 except ValueError: return
 if not any(part.startswith(('_','.')) for part in relative.parts):
  raise ValueError('in-repository output must be beneath an underscore/hidden directory')
def native_host():
 if (platform.system(),platform.machine())!=('Darwin','arm64'): raise ValueError('requires native Darwin arm64; cross compilation is not execution')
 p=subprocess.run(['sysctl','-in','sysctl.proc_translated'],capture_output=True,text=True,timeout=10)
 if p.stdout.strip()=='1': raise ValueError('Rosetta rejected')
def census(text,required):
 passed=re.findall(r'^\s*--- PASS: (\S+)',text,re.M)
 failed=re.findall(r'^\s*--- FAIL: (\S+)',text,re.M)
 skipped=re.findall(r'^\s*--- SKIP: (\S+)',text,re.M)
 top=[n for n in passed if '/' not in n]
 missing=[n for n in required if passed.count(n)!=1]
 unexpected=sorted(set(top)-{n for n in required if '/' not in n})
 duplicates=[n for n,c in collections.Counter(passed).items() if c!=1]
 # Existing TinyGo helpers can return early without t.Skip: reject their messages too.
 vacuous=bool(re.search('unavailable under TinyGo|depends on standard Go testing|no tests to run',text))
 ok=not(failed or skipped or missing or unexpected or duplicates or vacuous)
 return dict(ok=ok,passed=passed,failed=failed,skipped=skipped,missing=missing,unexpected=unexpected,duplicates=duplicates,vacuous=vacuous)
def adapt(source):
 changes={}
 for src,dst in [('src/wago/gc_stage8_tinygo_guest_amd64_test.go','src/wago/supplement_stage8_test.go'),('src/wago/design_repair_unsupported_test.go','src/wago/supplement_rejection_test.go')]:
  old=(source/src).read_bytes()
  data=b'//go:build darwin && arm64 && tinygo\n'+old.split(b'\n',1)[1]
  (source/dst).write_bytes(data)
  changes[dst]=dict(original=src,original_sha256=hashlib.sha256(old).hexdigest(),sha256=sha(source/dst),change='build constraint and filename only; body unchanged')
 for src,dst in [('foreign_stack_arm64_test.go.in','src/core/runtime/supplement_foreign_stack_arm64_test.go'),('cancellation_test.go.in','src/wago/supplement_cancellation_test.go')]:
  (source/dst).write_bytes((E/src).read_bytes());changes[dst]=dict(adapter=src,sha256=sha(source/dst))
 return changes

def suites(mode):
 runtime=['TestTinyGoForeignStackConcurrentGC','TestTinyGoForeignStackGCPreparedEntry','TestTinyGoForeignStackGCHostReentry','TestTinyGoForeignStackGCCloseThunkFailure','TestTinyGoTrampolineFailurePreservesInterrupt','TestTinyGoNativeResumeUsesPhysicalStackTop','TestPreparedIntThunkOwnedByEngine']
 runtime += ['TestTinyGoForeignStackConcurrentGC/'+n for n in ['return','trap-unwind','interrupt']]
 evacuation=['TestEvacuationTinyGoPolicy','TestEvacuationTinyGoExactAdmissionEdges','TestEvacuationTinyGoCheckedGeneration','TestEvacuationTinyGoCardsLOSAndYoung','TestEvacuationTinyGoRequiredPressure']
 evacuation += ['TestEvacuationStage7'+n for n in ['RunCredit','PolicyAndOptionalRefusal','Selection','CrossingPhysicalMove','AbsorbedTail','LargeYoungPlan']]
 if mode=='wagodebug': evacuation += ['TestEvacuationStage7'+n for n in ['AdmissionFaults','CommittedMaintenance','SerialPartialCopyRollback']]
 wago=['TestGCStage8TinyGoGuestRelocatedRead','TestGCStage8TinyGoControls','TestDarwinTinyGoCancellationAndGC']
 wago += ['TestGCExternConversion'+n for n in ['StableIdentityAndOwnership','RejectsForeignForgedAndClosed','CollectorFirstCloseFailsClosed','RejectedReplacementPreservesRoot','PublicationOrderingAndRollback']]
 wago += ['TestDesignRepair'+n for n in ['PublicGlobalTokenClassification','UnsupportedConvertedPublication','UnsupportedBulkAtomicity','PublicTableOrdinary','UnsupportedConvertedPublicToken']]
 wago += ['TestUnsupportedConversionPublicGlobalRejectsOpaqueBeforeDirectory','TestUnsupportedConversionGlobalValidationOrdering']
 wago += ['TestDesignRepairPublicGlobalTokenClassification/'+a+'/'+b for a in ['local','owned'] for b in ['foreign','forged','expired','authenticated-wrapper','ordinary-and-null']]
 wago += ['TestUnsupportedConversionGlobalValidationOrdering/'+n for n in ['ordinary','null','invalid','cross-store','immutable-before-invalid','immutable-before-null','closed-before-invalid','closed-before-null']]
 return {'src/core/runtime':runtime,'src/core/runtime/gc/native':evacuation,'src/wago':wago}

def command(out,label,argv,cwd,env,seconds):
 write(out/(label+'.command.json'),dict(argv=argv,cwd=str(cwd),environment=env,timeout_seconds=seconds))
 with (out/(label+'.log')).open('wb') as log:
  p=subprocess.Popen(argv,cwd=cwd,env=env,stdout=log,stderr=subprocess.STDOUT,start_new_session=True)
  try: rc=p.wait(timeout=seconds)
  except subprocess.TimeoutExpired:
   os.killpg(p.pid,signal.SIGKILL);p.wait();rc=124
 write(out/(label+'.exit.json'),dict(exit=rc,log_sha256=sha(out/(label+'.log'))))
 return rc

def main():
 p=argparse.ArgumentParser(description=__doc__)
 p.add_argument('action',choices=['prepare','native'])
 p.add_argument('--repo',type=Path,required=True);p.add_argument('--archive',type=Path,required=True)
 p.add_argument('--output',type=Path,required=True);p.add_argument('--mode',choices=MODES,required=True)
 p.add_argument('--harness',required=True);p.add_argument('--go',type=Path,required=True)
 a=p.parse_args();out=a.output.resolve();safe_output(out);out.mkdir(parents=True,exist_ok=False)
 write(out/'state.json',dict(status='INCOMPLETE',action=a.action,mode=a.mode))
 try:
  if a.action=='native': native_host()
  if not re.fullmatch('[0-9a-f]{40}',a.harness): raise ValueError('full harness SHA required')
  repo=a.repo.resolve();hroot=Path(capture(['git','rev-parse','--show-toplevel'],E))
  if capture(['git','rev-parse','HEAD'],hroot)!=a.harness: raise ValueError('wrong harness HEAD')
  if a.action=='native' and capture(['git','status','--porcelain','--untracked-files=all'],hroot): raise ValueError('dirty harness')
  if a.action=='native' and os.environ.get('GITHUB_SHA')!=a.harness: raise ValueError('workflow/harness SHA mismatch')
  if sha(a.archive)!=PIN['archive_sha256']: raise ValueError('wrong release archive')
  if capture(['git','rev-parse',PIN['source']],repo)!=PIN['source']: raise ValueError('missing source pin')
  source=out/'source';source.mkdir()
  with (out/'source.tar').open('wb') as f:subprocess.run(['git','archive',PIN['source']],cwd=repo,stdout=f,check=True,timeout=120)
  with tarfile.open(out/'source.tar') as t:t.extractall(source,filter='data')
  original=files(source);write(out/'source-original.json',original)
  check_hashes(source,PIN['patch'])
  tool=out/'tool';tool.mkdir()
  with tarfile.open(a.archive) as t:t.extractall(tool,filter='data')
  root=tool/'tinygo';check_hashes(root,PIN['before'])
  if sha(root/'bin/tinygo')!=PIN['compiler_sha256']: raise ValueError('compiler mismatch')
  # Exact offline equivalent of pinned shell patch, usable for source-only Linux preparation.
  for n,old,new in [('src/internal/task/task_threads.c','void tinygo_task_exit(void) {','void tinygo_task_exit_thread(void) {'),('src/internal/task/task_threads.go','//go:linkname tinygo_task_exit tinygo_task_exit\n','//go:linkname tinygo_task_exit tinygo_task_exit_thread\n')]:
   path=root/n;text=path.read_text();assert text.count(old)==1;path.write_text(text.replace(old,new))
  env={'PATH':str(a.go.resolve().parent)+':/usr/bin:/bin:/usr/sbin:/sbin','HOME':str(out),'GOROOT':str(a.go.resolve().parent.parent),'GOTOOLCHAIN':'local','GOENV':'off','GOWORK':'off','GOFLAGS':'-mod=readonly -p=2','GOMAXPROCS':'2','GOCACHE':str(out/'go-cache'),'GOMODCACHE':str(out/'modules'),'XDG_CACHE_HOME':str(out/'cache')}
  for label,script in [('patch','scripts/patch-tinygo-foreign-stack.py'),('patch-controls','tests/scripts/patch-tinygo-foreign-stack.py')]:
   if command(out,label,[sys.executable,str(source/script),str(root)],source,env,60): raise ValueError(label+' failed')
  check_hashes(root,PIN['after']);write(out/'adapters.json',adapt(source))
  before=files(source);tools_before=files(root);driver_before={n:sha(E/n) for n in ['harness.py','toolchain.json','foreign_stack_arm64_test.go.in','cancellation_test.go.in']}
  write(out/'source-effective.json',before);write(out/'tool-effective.json',tools_before)
  write(out/'identity.json',dict(source=PIN['source'],harness=a.harness,prior_harness=PIN['prior_harness'],source_archive_sha256=sha(out/'source.tar'),toolchain=PIN,drivers=driver_before,host=platform.uname()._asdict(),python=sys.version,mode=a.mode,action=a.action,suites=suites(a.mode)))
  if a.action=='prepare':
   write(out/'state.json',dict(status='PREPARED_NOT_EXECUTED',mode=a.mode));return
  tinygo=str(root/'bin/tinygo');env['PATH']=str(root/'bin')+':'+env['PATH'];env['TINYGOROOT']=str(root)
  go_version=capture([str(a.go),'version']);version=capture([tinygo,'version'])
  if go_version!='go version go1.27.1 darwin/arm64' or not version.startswith('tinygo version 0.42.0 darwin/arm64'): raise ValueError('wrong native tools')
  write(out/'host-tools.json',dict(go=go_version,go_sha256=sha(a.go),tinygo=version,sw_vers=capture(['sw_vers']),xcode=capture(['xcodebuild','-version']),sdk=capture(['xcrun','--show-sdk-path']),clang=capture(['xcrun','clang','--version'])))
  results=[]
  for index,(package,required) in enumerate(suites(a.mode).items()):
   label=str(index);binary=out/(label+'.test')
   cmd=[tinygo,'test','-c','-p=2','-scheduler=threads','-gc=conservative','-o',str(binary)]
   if a.mode!='ordinary':cmd+=['-tags='+a.mode]
   cmd+=['./'+package]
   rc=command(out,label+'-build',cmd,source,env,1200)
   row=dict(package=package,build_exit=rc,required=required);results.append(row)
   if rc==0:
    data=binary.read_bytes()
    if data[:8]!=bytes.fromhex('cffaedfe0c000001'): raise ValueError('not Mach-O ARM64')
    row['artifact_sha256']=sha(binary)
    write(out/(label+'.artifact.json'),dict(sha256=sha(binary),size=len(data),file=capture(['file',str(binary)]),loads=capture(['otool','-L',str(binary)])))
    pattern='^('+'|'.join(n for n in required if '/' not in n)+')$'
    row['exit']=command(out,label+'-run',[str(binary),'-test.v','-test.run='+pattern],source/package,env,600)
    row['census']=census((out/(label+'-run.log')).read_text(errors='replace'),required)
    if sha(binary)!=row['artifact_sha256']:raise ValueError('artifact changed')
   write(out/'results.json',results)
  unchanged=files(source)==before and files(root)==tools_before and all(sha(E/n)==h for n,h in driver_before.items())
  write(out/'integrity.json',dict(unchanged=unchanged))
  ok=unchanged and all(r['build_exit']==0 and r.get('exit')==0 and r.get('census',{}).get('ok') for r in results)
  write(out/'state.json',dict(status='PASS' if ok else 'FAIL',mode=a.mode));sys.exit(0 if ok else 1)
 except Exception as e:
  write(out/'failure.json',dict(error=str(e),type=type(e).__name__));raise
if __name__=='__main__':main()
