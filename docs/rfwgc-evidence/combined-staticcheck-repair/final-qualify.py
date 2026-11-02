#!/usr/bin/env python3
"""Qualification of an immutable commit export. No benchmarks or timing runs."""
import pathlib,subprocess,os,json,hashlib,sys
E=pathlib.Path(__file__).resolve().parent; W=pathlib.Path((E/'workspace.txt').read_text().strip());S=W/'final'
G='/home/dev/.local/share/mise/installs/go/1.27.1';Q='/tmp/qemu-x/usr/bin'
O=E/'final';O.mkdir(exist_ok=True)
def sha(p):return hashlib.sha256(pathlib.Path(p).read_bytes()).hexdigest()
D=sha(__file__)
env=dict(os.environ,GOROOT=G,GOTOOLCHAIN='local',GOMAXPROCS='2',GOFLAGS='-p=2',PATH=str(S/'.tools/staticcheck')+':'+G+'/bin:/usr/bin:/bin')
env.update(WAGO_SPEC_INTERPRETER='/home/dev/Projects/wago/.tools/spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa/wasm',WAGO_SPEC_INTERPRETER_REVISION='9d36019973201a19f9c9ebb0f10828b2fe2374aa',WAGO_WAST2JSON='/home/dev/Projects/wago/.tools/wabt-1.0.41-linux-x64/bin/wast2json',WAGO_WABT_VERSION='1.0.41')
env['PATH']='/home/dev/Projects/wago/.tools/wabt-1.0.41-linux-x64/bin:'+env['PATH']
for k in ['GOARCH','GOOS','WAGO_SHARED_SCALAR','GOEXPERIMENT','GOWORK']:env.pop(k,None)
def run(label,cmd,extra=None,cwd=None):
 d=O/label;d.mkdir(exist_ok=False);ee=dict(env,**(extra or {}))
 (d/'command.json').write_text(json.dumps(dict(source=json.loads((E/'final-source.json').read_text())['pin'],argv=cmd,cwd=str(cwd or S),environment={k:ee.get(k) for k in ['GOROOT','GOTOOLCHAIN','GOMAXPROCS','GOFLAGS','PATH','GOOS','GOARCH','CGO_ENABLED','WAGO_RV64_TEST_RUNNER','WAGO_SPEC_INTERPRETER','WAGO_SPEC_INTERPRETER_REVISION','WAGO_WAST2JSON','WAGO_WABT_VERSION']},driver_sha256=D),indent=2)+'\n')
 with (d/'raw.log').open('w') as f:
  try: code=subprocess.run(cmd,cwd=cwd or S,env=ee,stdout=f,stderr=subprocess.STDOUT,timeout=2400).returncode
  except subprocess.TimeoutExpired:code=124
 (d/'result.json').write_text(json.dumps({'exit':code,'raw_sha256':sha(d/'raw.log')})+'\n');print(label,code,flush=True)
 assert sha(__file__)==D,'driver mutated during execution'
 return code
lane=sys.argv[1]
if lane=='lint':
 if run('bootstrap',['/bin/sh','scripts/bootstrap-staticcheck.sh','--verify']):sys.exit(1)
 run('staticcheck',[G+'/bin/go','run','./tests/tools/staticcheck-baseline','verify'])
elif lane=='host':
 for mode in ['ordinary','wagodebug','wago_regalloccheck']:
  tags=[] if mode=='ordinary' else ['-tags='+mode]
  args=[G+'/bin/go','test','-json','-count=1','-timeout=35m','-failfast',*tags]
  if run('packages-'+mode,args+['./src/core/runtime/gc/...','./src/core/runtime','./src/wago']):sys.exit(1)
  if run('ownership-race-'+mode,args+['-race','-run=^(TestR9H2.*|TestR9Ownership.*|TestGCExternConversion.*|TestGCConversion.*|TestPreparedGCRevocable.*|TestManagedFunction.*)$','./src/wago']):sys.exit(1)
elif lane=='targets':
 for arch,target in [('arm64','aarch64'),('riscv64','riscv64')]:
  emulator=Q+'/qemu-'+target
  run(arch+'-qemu-version',[emulator,'--version'])
  ee=dict(GOOS='linux',GOARCH=arch,CGO_ENABLED='0',WAGO_RV64_TEST_RUNNER='qemu')
  cpu=['-cpu','max'] if arch=='arm64' else ['-cpu','rv64,zba=false,zbb=false,zbc=false,zbs=false,v=false']
  for mode in ['ordinary','wagodebug','wago_regalloccheck']:
   tags=[] if mode=='ordinary' else ['-tags='+mode]
   binary=W/(arch+'-'+mode+'.test');label=arch+'-'+mode
   if run(label+'-build',[G+'/bin/go','test','-c',*tags,'-o',str(binary),'./src/wago'],ee):sys.exit(1)
   machine=int.from_bytes(binary.read_bytes()[18:20],'little');assert machine=={'arm64':183,'riscv64':243}[arch]
   (O/(label+'-elf.json')).write_text(json.dumps({'machine':machine,'sha256':sha(binary)})+'\n')
   regex='^(TestManagedFunction.*|TestR9H2.*|TestR9Ownership.*|TestGCExternConversion.*|TestRV64ManagedFunction.*|TestRV64CompactExtern.*|TestRV64CompactConvertedExternShellReclamation|TestRV64CompactForeignExternPinAndReclaim|Test.*Dependency.*)$'
   if run(label+'-execution',[emulator,*cpu,str(binary),'-test.v','-test.count=1','-test.timeout=35m','-test.failfast','-test.run='+regex],ee,S/'src/wago'):sys.exit(1)
elif lane=='build-tags':
 for arch in ['amd64','arm64','riscv64','386']:
  for tags in ['','tinygo','wago_target_tinygo']:
   run('tags-'+arch+'-'+(tags or 'ordinary'),[G+'/bin/go','list','-f','{{join .GoFiles "\n"}}',*(['-tags='+tags] if tags else []),'./src/wago'],dict(GOOS='linux',GOARCH=arch,CGO_ENABLED='0'))
