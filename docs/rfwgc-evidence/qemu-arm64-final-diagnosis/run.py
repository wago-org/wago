#!/usr/bin/env python3
"""Bounded diagnosis; no production edits, no benchmark or performance clocks."""
import os,sys,json,hashlib,subprocess,tarfile,tempfile,signal
from pathlib import Path
E=Path(__file__).resolve().parent
ROOT=E.parents[2]
PIN='08f511fac41a77f4c0f9046f88ebc78ad05a5da6'
Q=Path('/home/dev/.cache/_wago_final_5b02_n61wfxab/target-repairs/qemu/usr/bin/qemu-aarch64')
GO=Path('/home/dev/.local/share/mise/installs/go/1.27.3/bin/go')
MAP='32 36 28,64 64 1'
W=Path(tempfile.mkdtemp(prefix='wago-qemu-diagnosis-',dir='/home/dev/.cache'))
S=W/'source';S.mkdir()
env={k:v for k,v in os.environ.items() if not k.startswith(('GO','QEMU','WAGO_','CGO'))}
env.update(GOROOT=str(GO.parent.parent),GOTOOLCHAIN='local',GOENV='off',GOARCH='arm64',GOOS='linux',CGO_ENABLED='0',GOMAXPROCS='2',GOFLAGS='-p=2',PATH='/home/dev/.cache/_wago_final_5b02_n61wfxab/source/.tools/wabt-1.0.41-linux-x64/bin:'+os.environ['PATH'])
rows=[]
def sha(p):return hashlib.sha256(Path(p).read_bytes()).hexdigest()
def run(label,argv,cwd=S,timeout=120,extra=None):
 argv=list(map(str,argv)); ee=env| (extra or {})
 with (E/(label+'.log')).open('wb') as f:
  p=subprocess.Popen(argv,cwd=cwd,env=ee,stdout=f,stderr=subprocess.STDOUT,start_new_session=True)
  try:rc=p.wait(timeout=timeout)
  except subprocess.TimeoutExpired:
   os.killpg(p.pid,signal.SIGKILL);p.wait();rc='TIMEOUT-KILLED'
 row=dict(label=label,argv=argv,cwd=str(cwd),exit=rc,log_sha256=sha(E/(label+'.log')))
 rows.append(row);(E/'commands.json').write_text(json.dumps(rows,indent=2)+'\n');print(label,rc,flush=True)
 return rc
with (W/'source.tar').open('wb') as f:subprocess.run(['git','archive',PIN,'src','go.mod','go.sum'],cwd=ROOT,stdout=f,check=True)
with tarfile.open(W/'source.tar') as t:t.extractall(S,filter='data')
manifest={str(p.relative_to(S)):sha(p) for p in S.rglob('*') if p.is_file()}
(E/'source-manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
meta=dict(pin=PIN,export=str(S),archive_sha256=sha(W/'source.tar'),qemu=str(Q),qemu_sha256=sha(Q),go=str(GO),go_sha256=sha(GO),environment=env,signal_map=MAP)
(E/'provenance.json').write_text(json.dumps(meta,indent=2)+'\n')
run('qemu-version',[Q,'--version']);run('qemu-help',[Q,'-help']);run('go-version',[GO,'version'])
timer=E/'timer-capability.go.txt';timer.write_bytes((ROOT/'docs/rfwgc-evidence/native-arm64-rr3/timer-capability.go.txt').read_bytes())
(W/'timer.go').write_bytes(timer.read_bytes())
assert run('timer-build',[GO,'build','-o',W/'timer',W/'timer.go'])==0
run('timer-default',[Q,'-cpu','max','-strace',W/'timer'])
run('timer-mapped',[Q,'-cpu','max','-t',MAP,'-strace',W/'timer'])
# Go compiler needs ARM64 fixture declarations otherwise supplied only on amd64.
orig=S/'src/wago/gc_stage7_guest_amd64_test.go';adapter=S/'src/wago/platform_stage7_test.go'
adapter.write_text('//go:build linux && (arm64 || riscv64) && !tinygo\n'+orig.read_text().split('\n',1)[1].replace('.WithOptimization("dead-gc-new", false)', ''))
(E/'platform_stage7_test.go.txt').write_bytes(adapter.read_bytes())
# Fail-stop only: preserve every oracle/deadline, never unwind/close live code on failure.
guard=S/'src/wago/shared_entry_guard_arm64_test.go'
text=guard.read_text().replace('"bytes"','"bytes"\n "fmt"\n "os"')
for message in ['invocation did not enter','native loop prevented GC progress','native loop did not cancel']:
 text=text.replace('t.Fatal("'+message+'")','fmt.Fprintln(os.Stderr, "FAIL-STOP: '+message+'"); os.Exit(86)')
private=W/'shared_entry_guard_arm64_test.go';private.write_text(text)
(E/'shared_entry_guard_failstop.go.txt').write_text(text)
overlay=W/'overlay.json';overlay.write_text(json.dumps({'Replace':{str(guard):str(private)}}))
(E/'overlay.json').write_bytes(overlay.read_bytes())
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 binary=W/(mode+'.test');tags=[] if mode=='ordinary' else ['-tags='+mode]
 assert run(mode+'-build',[GO,'test','-c','-overlay',overlay,*tags,'-o',binary,'./src/wago'],timeout=240)==0
 meta.setdefault('binaries',{})[mode]=dict(path=str(binary),sha256=sha(binary),elf_machine=int.from_bytes(binary.read_bytes()[18:20],'little'))
 (E/'provenance.json').write_text(json.dumps(meta,indent=2)+'\n')
 for name in ['TestRRRecheckStorageOnlyReturnBehindDynamicReader','TestSharedEntryGuardCancellationAndGC']:
  short='storage' if 'Storage' in name else 'guard'
  for config,args in [('default',[]),('mapped',['-t',MAP])]:
   run(mode+'-'+short+'-'+config,[Q,'-cpu','max',*args,'-strace',binary,'-test.run=^'+name+'$','-test.v','-test.count=1','-test.timeout=30s'],timeout=45)
assert all(sha(S/p)==h for p,h in manifest.items())
(E/'integrity.json').write_text(json.dumps({'all_original_source_files_unchanged':True,'count':len(manifest),'added_fixture':sha(adapter),'driver_sha256':sha(__file__)},indent=2)+'\n')
