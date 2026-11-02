#!/usr/bin/env python3
"""Immutable target execution; no benchmarks. Run from the repository root."""
import argparse, hashlib, json, os, pathlib, shutil, subprocess, tarfile, tempfile
p=argparse.ArgumentParser(); p.add_argument('--pin',required=True);p.add_argument('--qemu-package',required=True);p.add_argument('--out',required=True);a=p.parse_args()
root=pathlib.Path.cwd();out=pathlib.Path(a.out).resolve();out.mkdir(parents=True,exist_ok=False)
work=pathlib.Path('/tmp/wago-final-9180-mfc7qkg_')/out.name;work.mkdir();src=work/'source';src.mkdir();qemu=work/'qemu'
go='/home/dev/.local/share/mise/installs/go/1.27.3/bin/go'
def sha(p):return hashlib.sha256(pathlib.Path(p).read_bytes()).hexdigest()
pin=subprocess.check_output(['git','rev-parse',a.pin],text=True).strip()
with (work/'source.tar').open('wb') as f:subprocess.run(['git','archive',pin],stdout=f,check=True)
with tarfile.open(work/'source.tar') as f:f.extractall(src,filter='data')
subprocess.run(['dpkg-deb','-x',a.qemu_package,str(qemu)],check=True)
# Test-only portability adapter: preserve the original fixture body exactly.
original=src/'src/wago/gc_stage7_guest_amd64_test.go'
adapter=src/'src/wago/platform_stage7_test.go'
adapter.write_text('//go:build linux && (arm64 || riscv64) && !tinygo\n'+original.read_text().split('\n',1)[1].replace('.WithOptimization("dead-gc-new", false)', ''))
# RV64 bounds constant-expression array construction to 256 entries. Keep
# the 60000-element graph, allocating it in guest code before the same loop.
body=adapter.read_text()
body=body.replace('global := append([]byte{0x63, 1, 1, 0x41}, wasmtest.SLEB32(60000)...)\n\tglobal = append(global, 0xfb, 7, 1, 0x0b)', 'global := []byte{0x63, 1, 1, 0xd0, 1, 0x0b}')
body=body.replace('body := []byte{1, 1, 0x7f, 0x03', 'body := []byte{1, 1, 0x7f, 0x41, 0xe0, 0xd4, 0x03, 0xfb, 7, 1, 0x24, 0, 0x03')
adapter.write_text(body)
# Preserve both H6 test bodies byte-for-byte; add ARM64-only test copies.
h6=[]
for name in ['function_environment_nested_owner_host_test.go','function_environment_wakeup_host_test.go']:
 old=src/'src/wago'/name;new=src/'src/wago'/('platform_h6_'+name)
 body=old.read_bytes().split(b'\n',1)[1]
 new.write_bytes(b'//go:build linux && arm64 && !tinygo && !wago_target_tinygo\n'+body)
 assert new.read_bytes().split(b'\n',1)[1]==body
 h6.append(dict(original=str(old.relative_to(src)),adapter=str(new.relative_to(src)),original_sha256=sha(old),adapter_sha256=sha(new),body_sha256=hashlib.sha256(body).hexdigest(),body_unchanged=True))
(out/'h6-test-adapters.json').write_text(json.dumps(h6,indent=2)+'\n')
probe='''package wago
import("os";"runtime";"testing")
func TestPlatformExecutionArchitecture(t *testing.T) { want:=os.Getenv("WAGO_EXPECT_ARCH"); if runtime.GOARCH!=want || runtime.GOOS!="linux" { t.Fatalf("actual %s/%s expected linux/%s",runtime.GOOS,runtime.GOARCH,want) }; t.Logf("EXECUTED linux/%s",runtime.GOARCH) }
'''
(src/'src/wago/platform_arch_test.go').write_text(probe)
meta={'pin':pin,'archive_sha256':sha(work/'source.tar'),'driver_sha256':sha(__file__),'go':subprocess.check_output([go,'version'],text=True).strip(),'package':a.qemu_package,'package_sha256':sha(a.qemu_package),'export':str(src),'adapter_original_sha256':sha(original),'adapter_sha256':sha(adapter),'probe':probe,'host':os.uname()._asdict() if hasattr(os.uname(),'_asdict') else list(os.uname())}
(out/'source.json').write_text(json.dumps(meta,indent=2)+'\n');results=[]
def run(label,cmd,env):
 log=out/(label+'.log')
 with log.open('w') as f:
  try:r=subprocess.run(cmd,cwd=(src/'src/wago' if label.endswith('-guest') else src),env=env,stdout=f,stderr=subprocess.STDOUT,timeout=900);code=r.returncode
  except subprocess.TimeoutExpired:code=124
 text=log.read_text();
 if code and ('STOP ' in text or 'WARNING: DATA RACE' in text or 'internal compiler error' in text):
  (out.parent/'SAFETY-STOP.json').write_text(json.dumps(dict(source=pin,gate=label,command=cmd,reason='H6 target invariant failed; retained raw log '+str(log)),indent=2))
 row={'label':label,'command':cmd,'exit':code,'pass':[x for x in text.splitlines() if '--- PASS:' in x],'skip':[x for x in text.splitlines() if '--- SKIP:' in x],'fail':[x for x in text.splitlines() if '--- FAIL:' in x]};results.append(row)
 (out/'results.json').write_text(json.dumps(results,indent=2)+'\n');print(label,code,flush=True);return code
for arch,target in [('arm64','aarch64')]:
 emulator=str(qemu/'usr/bin'/('qemu-'+target));run(arch+'-version',[emulator,'--version'],os.environ.copy())
 cpu=['-cpu','max'] if arch=='arm64' else ['-cpu','rv64,zba=false,zbb=false,zbc=false,zbs=false,v=false']
 env=dict(os.environ,GOOS='linux',GOARCH=arch,CGO_ENABLED='0',GOROOT=str(pathlib.Path(go).parent.parent),GOMAXPROCS='4',GOWORK='off',WAGO_EXPECT_ARCH=arch,WAGO_RV64_TEST_RUNNER='qemu')
 for mode in ['ordinary','wagodebug','wago_regalloccheck']:
  tags=[] if mode=='ordinary' else ['-tags='+mode]
  for suite,pkg,regex in [('guest','./src/wago','^(TestPlatformExecutionArchitecture|TestR9Ownership.*)$')]:
   label=arch+'-'+mode+'-'+suite;binary=work/(label+'.test')
   if run(label+'-build',[go,'test','-c','-p=2',*tags,'-o',str(binary),pkg],env):raise SystemExit(1)
   machine=int.from_bytes(binary.read_bytes()[18:20], 'little'); assert machine==({'arm64':183,'riscv64':243}[arch]); (out/(label+'-elf.json')).write_text(json.dumps({'machine':machine,'arch':arch}))
   (out/(label+'-binary.sha256')).write_text(sha(binary)+'\n')
   if run(label,[emulator,*cpu,str(binary),'-test.v','-test.failfast','-test.count=1','-test.timeout=12m','-test.run='+regex],env): raise SystemExit(1)
(out/'driver-final.json').write_text(json.dumps({'driver_unchanged':sha(__file__)==meta['driver_sha256'],'export':str(src)},indent=2)+'\n')
