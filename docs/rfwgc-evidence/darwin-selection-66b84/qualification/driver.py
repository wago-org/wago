import pathlib,subprocess,os,json,tarfile
out=pathlib.Path('/tmp/pr1048-darwin-selection-66b84');out.mkdir(exist_ok=True);src=out/'source';src.mkdir(exist_ok=True)
pin=subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip()
p=subprocess.Popen(['git','archive',pin],stdout=subprocess.PIPE)
with tarfile.open(fileobj=p.stdout,mode='r|') as t:t.extractall(src,filter='data')
assert p.wait()==0
go='/home/dev/.local/share/mise/installs/go/1.27.1/bin/go';e=dict(os.environ,GOROOT=str(pathlib.Path(go).parent.parent),GOTOOLCHAIN='local',GOPROXY='off',GOWORK='off');rows=[]
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 tags=[] if mode=='ordinary' else ['-tags='+mode]
 for system,arch in [('darwin','arm64'),('windows','amd64'),('windows','arm64'),('linux','riscv64')]:
  env=dict(e,GOOS=system,GOARCH=arch,CGO_ENABLED='0');cmd=[go,'list',*tags,'-json','./src/wago'];r=subprocess.run(cmd,cwd=src,env=env,capture_output=True,text=True,check=True);data=json.loads(r.stdout);files=data['TestGoFiles'];bad=['design_repair_unsupported_test.go','design_repair_unsupported_token_test.go']
  if system=='darwin':
   assert not any(x in files for x in bad)
   assert all(x in files for x in ['gc_conversion_classification_host_test.go','externref_global_conversion_test.go'])
  elif system=='windows':assert all(x in files for x in bad)
  else:assert bad[1] in files
  (out/(mode+'-'+system+'-'+arch+'-selection.json')).write_text(r.stdout)
  cmd=[go,'test',*tags,'./src/wago','-run','^$','-exec=true'];label=mode+'-'+system+'-'+arch
  with (out/(label+'.log')).open('w') as f:r=subprocess.run(cmd,cwd=src,env=env,stdout=f,stderr=subprocess.STDOUT)
  rows.append({'label':label,'argv':cmd,'exit':r.returncode});print(label,r.returncode,flush=True);assert r.returncode==0
 ft='wago_target_tinygo'+('' if mode=='ordinary' else ','+mode)
 cmd=[go,'test','-tags='+ft,'./src/wago','-run','^Test(UnsupportedConversion|DesignRepairUnsupportedConvertedPublicToken|DesignRepairPublicGlobalTokenClassification)','-count=1','-v']
 with (out/(mode+'-fallback.log')).open('w') as f:r=subprocess.run(cmd,cwd=src,env=e,stdout=f,stderr=subprocess.STDOUT)
 rows.append({'label':mode+'-fallback','argv':cmd,'exit':r.returncode});print(mode+'-fallback',r.returncode,flush=True);assert r.returncode==0
 (out/'results.json').write_text(json.dumps({'pin':pin,'rows':rows},indent=2))
