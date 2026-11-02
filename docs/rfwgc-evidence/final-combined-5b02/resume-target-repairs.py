"""Complete missing target cells; known controlled failures remain FAIL."""
import run as q
import pathlib,json,hashlib,subprocess,os,re
E=q.E;out=E/'qemu-repairs';meta=json.loads((out/'source.json').read_text());src=pathlib.Path(meta['export']);work=src.parent;go='/home/dev/.local/share/mise/installs/go/1.27.3/bin/go'
def sha(p):return hashlib.sha256(pathlib.Path(p).read_bytes()).hexdigest()
assert meta['pin']==q.M['pin'] and sha(work/'source.tar')==meta['archive_sha256']==q.M['archive_sha256']
assert sha(E/'target-repairs.py')==meta['driver_sha256']
for n,h in json.loads((E/'extra-tools-before.json').read_text()).items():
 if n in [go,meta['package']]:assert sha(n)==h,n
for n,h in json.loads((E/'source-files-before.json').read_text()).items():assert sha(src/n)==h,n
seals=json.loads((E/'nested-integrity.json').read_text())['files']
assert json.loads((E/'target-repairs-original/command.json').read_text())['environment']==q.env
results=json.loads((out/'results.json').read_text());before=json.dumps(results,sort_keys=True);original_rows=list(results)
for r in results:
 log=out/(r['label']+'.log');assert str(log.relative_to(E)) in seals and sha(log)==seals[str(log.relative_to(E))]
for p in out.glob('*-binary.sha256'):
 binary=work/(p.name.removesuffix('-binary.sha256')+'.test');assert sha(binary)==p.read_text().strip()
def known(row):
 text=(out/(row['label']+'.log')).read_text()
 tops={m.group(1) for l in row['fail'] if (m:=re.match(r'^--- FAIL: ([^ /]+)',l))}
 return row['exit']==1 and row['label'].startswith('arm64-') and tops=={'TestRRRecheckStorageOnlyReturnBehindDynamicReader','TestSharedEntryGuardCancellationAndGC'} and 'create native deadline timer: invalid argument' in text and 'native loop did not cancel' in text and not any(t in text for t in ['panic:', 'fatal error:', 'WARNING: DATA RACE','SIGSEGV'])
for r in results:assert r['exit']==0 or known(r),r['label']
proof={'source':q.M['pin'],'original_results_sha256':hashlib.sha256(before.encode()).hexdigest(),'original_rows':original_rows,'driver_sha256':sha(__file__),'original_driver_sha256':meta['driver_sha256'],'execution_environment':dict(q.env),'policy':'Only the two exact documented ARM64 controlled test failures may continue; they remain FAIL. No completed cell reruns.'}
(out/'continuation-before.json').write_text(json.dumps(proof,indent=2)+'\n')
def run(label,cmd,env):
 previous=next((r for r in results if r['label']==label),None)
 if previous:
  assert previous['command']==cmd
  print('VERIFIED EXISTING',label,previous['exit'],flush=True);return previous
 assert not (out/(label+'.log')).exists(),label
 with (out/(label+'.log')).open('w') as f:
  try:r=subprocess.run(cmd,cwd=(src/'src/wago' if label.endswith('-guest') else src),env=env,stdout=f,stderr=subprocess.STDOUT,timeout=900);code=r.returncode
  except subprocess.TimeoutExpired:code=124
 text=(out/(label+'.log')).read_text();row={'label':label,'command':cmd,'exit':code,'pass':[x for x in text.splitlines() if '--- PASS:' in x],'skip':[x for x in text.splitlines() if '--- SKIP:' in x],'fail':[x for x in text.splitlines() if '--- FAIL:' in x],'environment':env,'cwd':str(src/'src/wago' if label.endswith('-guest') else src),'continuation_driver_sha256':proof['driver_sha256'],'raw_sha256':sha(out/(label+'.log'))};results.append(row)
 (out/'results.json').write_text(json.dumps(results,indent=2)+'\n');print(label,code,flush=True)
 assert code==0 or known(row), 'Additional target failure: '+label
 return row
for arch,target in [('arm64','aarch64'),('riscv64','riscv64')]:
 emulator=str(work/'qemu/usr/bin'/('qemu-'+target));run(arch+'-version',[emulator,'--version'],q.env.copy())
 cpu=['-cpu','max'] if arch=='arm64' else ['-cpu','rv64,zba=false,zbb=false,zbc=false,zbs=false,v=false']
 env=dict(q.env,GOOS='linux',GOARCH=arch,CGO_ENABLED='0',GOROOT=str(pathlib.Path(go).parent.parent),GOMAXPROCS='4',GOWORK='off',WAGO_EXPECT_ARCH=arch,WAGO_RV64_TEST_RUNNER='qemu')
 for mode in ['ordinary','wagodebug','wago_regalloccheck']:
  tags=[] if mode=='ordinary' else ['-tags='+mode];label=arch+'-'+mode+'-guest';binary=work/(label+'.test')
  run(label+'-build',[go,'test','-c','-p=2',*tags,'-o',str(binary),'./src/wago'],env)
  machine=int.from_bytes(binary.read_bytes()[18:20],'little');assert machine==({'arm64':183,'riscv64':243}[arch])
  (out/(label+'-elf.json')).write_text(json.dumps({'machine':machine,'arch':arch}));(out/(label+'-binary.sha256')).write_text(sha(binary)+'\n')
  regex='^(TestPlatformExecutionArchitecture|TestRR.*|TestSharedEntryGuardCancellationAndGC|TestDesign.*|TestR9H2.*|TestGCExternConversion.*|TestRV64ManagedFunctionJoint.*|TestRV64CompactExtern.*|TestRV64CompactConvertedExternShellReclamation|TestRV64CompactForeignExternPinAndReclaim)$'
  run(label,[emulator,*cpu,str(binary),'-test.v','-test.count=1','-test.timeout=12m','-test.run='+regex],env)
assert results[:len(original_rows)]==original_rows
for n,h in json.loads((E/'source-files-before.json').read_text()).items():assert sha(src/n)==h,n
assert sha(__file__)==proof['driver_sha256'] and sha(E/'target-repairs.py')==meta['driver_sha256']
(out/'continuation-final.json').write_text(json.dumps({'source':q.M['pin'],'completed':True,'source_unchanged':True,'known_failures':[r['label'] for r in results if r['exit']],'all_cells_pass':all(r['exit']==0 for r in results)},indent=2)+'\n')
# Completing a failed qualification is not a successful test verdict.
raise SystemExit(1 if any(r['exit'] for r in results) else 0)
