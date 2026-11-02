#!/usr/bin/env python3
"""Cross builds and Linux emulation; never qualifies Darwin execution."""
import hashlib,json,os,pathlib,subprocess,sys
source=pathlib.Path(sys.argv[1]);out=pathlib.Path(sys.argv[2]).resolve();out.mkdir(exist_ok=True,parents=True)
go='/home/dev/.local/share/mise/installs/go/1.27.1/bin/go'
qemu=pathlib.Path('/tmp/wago-pr994-tools/root/usr/bin')
base=dict(os.environ,GOROOT=str(pathlib.Path(go).parent.parent),GOWORK='off',GOMAXPROCS='4',CGO_ENABLED='0',WAGO_RV64_TEST_RUNNER='qemu')
rows=[]
def run(name,cmd,env,cwd):
 with (out/(name+'.log')).open('w') as f:
  try:code=subprocess.run(cmd,env=env,cwd=cwd,stdout=f,stderr=subprocess.STDOUT,timeout=900).returncode
  except subprocess.TimeoutExpired:code=124
 rows.append(dict(name=name,command=cmd,cwd=str(cwd),exit=code,GOOS=env.get('GOOS'),GOARCH=env.get('GOARCH')))
 (out/'results.json').write_text(json.dumps(rows,indent=2)+'\n');print(name,code,flush=True);return code
for osname,arch in [('darwin','arm64'),('linux','arm64'),('linux','riscv64')]:
 env=dict(base,GOOS=osname,GOARCH=arch)
 for mode in ['ordinary','wagodebug','wago_regalloccheck']:
  tags=[] if mode=='ordinary' else ['-tags='+mode];name=osname+'-'+arch+'-'+mode
  binary=source.parent/(name+'.test')
  if run(name+'-build',[go,'test','-c',*tags,'./src/wago','-o',str(binary)],env,source):continue
  (out/(name+'-binary.sha256')).write_text(hashlib.sha256(binary.read_bytes()).hexdigest()+'\n')
  if osname=='darwin':continue
  emulator=qemu/('qemu-aarch64' if arch=='arm64' else 'qemu-riscv64')
  cpu=['-cpu','max'] if arch=='arm64' else ['-cpu','rv64,zba=false,zbb=false,zbc=false,zbs=false,v=false']
  regex='^(TestConverted.*|TestExternrefGlobalHoldingConvertedGCReference|TestR9H2.*|TestR9Ownership.*|TestGCExternConversion.*|TestRV64ManagedFunctionJoint.*|TestRV64CompactExtern.*|TestRV64CompactConvertedExternShellReclamation|TestRV64CompactForeignExternPinAndReclaim)$'
  run(name+'-qemu',[str(emulator),*cpu,str(binary),'-test.v','-test.count=1','-test.timeout=12m','-test.run='+regex],env,source/'src/wago')
  binary.unlink()
