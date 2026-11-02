import pathlib,os,json,hashlib,subprocess,tarfile,re,gzip
E=pathlib.Path(__file__).resolve().parent
M=json.loads((E/'source.json').read_text()); W=pathlib.Path(M['workspace']); S=W/'resume-source'
def sha(p):return hashlib.sha256(pathlib.Path(p).read_bytes()).hexdigest()
D=sha(__file__)
S.mkdir()
with (W/'resume-source.tar').open('wb') as f:subprocess.run(['git','archive',M['pin']],stdout=f,check=True)
with tarfile.open(W/'resume-source.tar') as f:f.extractall(S,filter='data')
original=S/'src/wago/design_repair_unsupported_test.go'
adapter=S/'src/wago/final_token_tinygo_rejection_test.go'
adapter.write_text('//go:build tinygo || wago_target_tinygo\n'+original.read_text().split('\n',1)[1])
owned=E.parents[2]/'src/wago/design_repair_public_global_token_test.go'
(S/'src/wago'/owned.name).write_bytes(owned.read_bytes())
files={str(p.relative_to(S)):sha(p) for p in S.rglob('*') if p.is_file()}
(E/'resume-export-before.json').write_text(json.dumps(files,indent=2))
G='/home/dev/.local/share/mise/installs/go/1.27.1'; T='/home/dev/.local/share/wago-tinygo-investigation/tinygo/bin/tinygo'
env=dict(os.environ,GOROOT=G,GOTOOLCHAIN='local',GOWORK='off',GOMAXPROCS='2',GOFLAGS='-p=2',GOMODCACHE=str(W/'gomodcache'),GOCACHE=str(W/'tinygo-cache/go-build'),XDG_CACHE_HOME=str(W/'tinygo-cache'))
env['PATH']=G+'/bin:/home/dev/Projects/wago/.tools/wabt-1.0.41-linux-x64/bin:'+env['PATH']
meta=dict(pin=M['pin'],archive_sha256=sha(W/'resume-source.tar'),driver_sha256=D,original_sha256=sha(original),adapter_sha256=sha(adapter),regression_sha256=sha(owned),adapter_change='build constraint only; original body unchanged')
(E/'resume-provenance.json').write_text(json.dumps(meta,indent=2))
results=[]
def run(label,cmd,extra=None,cwd=S,required=None):
 label='resume-'+label
 ev=env|(extra or {}); log=E/(label+'.log')
 (E/(label+'.command.json')).write_text(json.dumps(dict(argv=cmd,cwd=str(cwd),environment=ev,driver_sha256=D,pin=M['pin']),indent=2))
 with log.open('w') as f:
  try:rc=subprocess.run(cmd,cwd=cwd,env=ev,stdout=f,stderr=subprocess.STDOUT,timeout=1800).returncode
  except subprocess.TimeoutExpired:rc=124
 text=log.read_text(); passed=re.findall(r'^\s*--- PASS: (\S+)',text,re.M); skipped=re.findall(r'^\s*--- SKIP: (\S+)',text,re.M); failed=re.findall(r'^\s*--- FAIL: (\S+)',text,re.M)
 missing=[x for x in required or [] if x not in passed]
 row=dict(label=label,exit=rc,passed=passed,skipped=skipped,failed=failed,missing=missing,raw_sha256=sha(log));results.append(row)
 (E/'resume-results.json').write_text(json.dumps(results,indent=2)); print(label,rc,len(passed),'missing',missing,flush=True)
 if rc or failed or missing:
  (E/'SAFETY-STOP.json').write_text(json.dumps(row,indent=2));raise SystemExit(1)
 return passed
modes=['ordinary','wagodebug','wago_regalloccheck']
for mode in modes:
 tags=[] if mode=='ordinary' else ['-tags='+mode]
 required=['TestDesignRepairPublicGlobalTokenClassification'] + ['TestDesignRepairPublicGlobalTokenClassification/'+target+'/'+case for target in ['local','owned'] for case in ['foreign','forged','expired','authenticated-wrapper','ordinary-and-null']]
 run('tinygo-regression-'+mode,[T,'test','-scheduler=threads','-gc=conservative',*tags,'-v','-run','^TestDesignRepairPublicGlobalTokenClassification$','./src/wago'],required=required)
pattern='^(TestDesignRepair.*|TestNullableLocalExternrefGlobals|TestLocalExternrefGlobalsRespectFeatureStoreAndLifetimeBoundaries|TestDesignRepairPublicTableOrdinary|TestGCExternConversion.*)$'
required=['TestDesignRepairPublicGlobalTokenClassification','TestNullableLocalExternrefGlobals','TestLocalExternrefGlobalsRespectFeatureStoreAndLifetimeBoundaries'] + ['TestDesignRepairPublicGlobalTokenClassification/'+target+'/'+case for target in ['local','owned'] for case in ['foreign','forged','expired','ordinary-and-null']]
for mode in modes:
 tags=[] if mode=='ordinary' else ['-tags='+mode]
 run('native-amd64-'+mode,[G+'/bin/go','test','-v','-count=1',*tags,'-run',pattern,'./src/wago'],required=required)
Q=W/'qemu';subprocess.run(['dpkg-deb','-x','/tmp/qemu-user.deb',str(Q)],check=True)
for arch,target,cpu in [('arm64','aarch64','max'),('riscv64','riscv64','rv64,zba=false,zbb=false,zbc=false,zbs=false,v=false')]:
 for mode in modes:
  tags=[] if mode=='ordinary' else ['-tags='+mode]; binary=W/(arch+'-'+mode+'.test')
  ev=dict(GOARCH=arch,GOOS='linux',CGO_ENABLED='0',WAGO_RV64_TEST_RUNNER='qemu')
  label='native-'+arch+'-'+mode
  run(label+'-build',[G+'/bin/go','test','-c',*tags,'-o',str(binary),'./src/wago'],extra=ev)
  (E/(label+'.binary.json')).write_text(json.dumps(dict(sha256=sha(binary),elf_machine=int.from_bytes(binary.read_bytes()[18:20],'little'))))
  run(label,[str(Q/'usr/bin'/('qemu-'+target)),'-cpu',cpu,str(binary),'-test.v','-test.count=1','-test.run='+pattern],extra=ev,cwd=S/'src/wago',required=required)
after={str(p.relative_to(S)):sha(p) for p in S.rglob('*') if p.is_file()}
(E/'resume-integrity.json').write_text(json.dumps(dict(driver_unchanged=D==sha(__file__),changed=[p for p in files if after.get(p)!=files[p]],added=sorted(set(after)-set(files))),indent=2))
