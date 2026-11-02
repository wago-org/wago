import hashlib,json,pathlib,subprocess,tempfile
root=pathlib.Path.cwd();out=root/'docs/rfwgc-evidence/darwin-two-result-bridge/fixed';out.mkdir(exist_ok=False)
p=json.loads((root/'docs/rfwgc-evidence/qemu-arm64-final-diagnosis/provenance.json').read_text()); source=pathlib.Path(p['export'])
work=pathlib.Path(tempfile.mkdtemp(prefix='_wago_bridge_diagnostic_',dir='/home/dev/.cache'))
rel='src/core/compiler/backend/railshot/arm64/callexec_arm64_test.go'
copy=work/'callexec_arm64_test.go';copy.write_bytes((root/rel).read_bytes());(out/'callexec-overlay.txt').write_bytes(copy.read_bytes())
overlay=work/'overlay.json';overlay.write_text(json.dumps({'Replace':{str(source/rel):str(copy)}}))
env=dict(p['environment']);env['GOROOT']='/home/dev/.local/share/mise/installs/go/1.27.1';go=env['GOROOT']+'/bin/go';env['GOTOOLCHAIN']='local';env['WAGO_SHARED_SCALAR']='0';env['CGO_ENABLED']='0';env['GOARCH']='arm64';env['GOOS']='linux'
results=[]
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 tags='wago_codegenstats'+('' if mode=='ordinary' else ','+mode)
 for platform in ['linux','darwin']:
  env['GOOS']=platform;binary=work/(mode+'-'+platform+'.test')
  argv=[go,'test','-c','-p=2','-mod=readonly','-overlay',str(overlay),'-tags='+tags,'-o',str(binary),'./src/core/compiler/backend/railshot/arm64']
  log=out/(mode+'-'+platform+'-build.log')
  with log.open('wb') as f:r=subprocess.run(argv,cwd=source,env=env,stdout=f,stderr=subprocess.STDOUT,timeout=300)
  row={'mode':mode,'platform':platform,'kind':'build','argv':argv,'exit':r.returncode,'binary':str(binary)}
  if r.returncode==0:row['binary_sha256']=hashlib.sha256(binary.read_bytes()).hexdigest()
  results.append(row);(out/'results.json').write_text(json.dumps(results,indent=2)+'\n');print(mode,platform,'build',r.returncode,flush=True)
  if r.returncode:raise SystemExit(r.returncode)
  if platform=='linux':
   argv=[p['qemu'],'-cpu','max','-t','32 36 28,64 64 1',str(binary),'-test.v','-test.count=32','-test.timeout=3m','-test.run=^TestTwoIntegerResultRegisterCall$']
   log=out/(mode+'-qemu.log')
   with log.open('wb') as f:r=subprocess.run(argv,cwd=source,env=env,stdout=f,stderr=subprocess.STDOUT,timeout=240)
   results.append({'mode':mode,'kind':'qemu-isolated-repeat','argv':argv,'exit':r.returncode,'log_sha256':hashlib.sha256(log.read_bytes()).hexdigest()});(out/'results.json').write_text(json.dumps(results,indent=2)+'\n');print(mode,'qemu',r.returncode,flush=True)
   if r.returncode:raise SystemExit(r.returncode)
(out/'provenance.json').write_text(json.dumps({'source':p['pin'],'source_export':str(source),'original_test_sha256':hashlib.sha256((source/rel).read_bytes()).hexdigest(),'overlay_sha256':hashlib.sha256(copy.read_bytes()).hexdigest(),'go':go,'go_sha256':hashlib.sha256(pathlib.Path(go).read_bytes()).hexdigest(),'qemu':p['qemu'],'qemu_sha256':p['qemu_sha256'],'environment':{k:env[k] for k in ['GOROOT','GOTOOLCHAIN','WAGO_SHARED_SCALAR','CGO_ENABLED','GOARCH']},'scope':'isolated repeated QEMU test plus Darwin cross-build; no native Darwin execution'},indent=2)+'\n')
