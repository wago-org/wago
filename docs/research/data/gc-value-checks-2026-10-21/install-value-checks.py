from pathlib import Path
import concurrent.futures, hashlib, json, os, shutil, subprocess, tempfile, time
root=Path('/home/dev/Projects/dewdrop')
source=root/'.tmp/wago-local-stack-2026-10-20/wago'
commit=subprocess.check_output(['git','rev-parse','HEAD'],cwd=source,text=True).strip()
assert commit == os.environ['WAGO_INSTALL_EXPECTED_SHA']
out=root/'.tmp/wago-next-gc-perf'/('install-'+commit[:9]);out.mkdir(exist_ok=False)
label='local-gc-'+commit[:9]
env=dict(os.environ,CGO_ENABLED='0',GOWORK='off',GOMAXPROCS='2')
def digest(path):return hashlib.sha256(path.read_bytes()).hexdigest()
def build(name,tags):
 cmd=['go','build','-trimpath',*tags,'-ldflags=-s -w -X main.version='+label,'-o',str(out/name),'./cli/wago']
 start=time.monotonic();p=subprocess.run(cmd,cwd=source,env=env,capture_output=True,text=True)
 row=dict(name=name,command=cmd,seconds=time.monotonic()-start,exit=p.returncode,stdout=p.stdout,stderr=p.stderr)
 (out/('build-'+name+'.json')).write_text(json.dumps(row,indent=2)+'\n');assert p.returncode==0,row
 print(name,row['seconds'],flush=True);return row
builds=[build('wago',[]),build('wago-runtime',['-tags','wago_runtime'])]
manager=Path('/home/dev/.local/bin/wago')
runtime=Path('/home/dev/.local/share/wago/versions')/label/'standard/normal/wago-runtime'
old=digest(manager);shutil.copyfile(manager,out/'previous-manager')
for src,dst in [(out/'wago-runtime',runtime),(out/'wago',manager)]:
 dst.parent.mkdir(parents=True,exist_ok=True)
 with tempfile.NamedTemporaryFile(dir=dst.parent,prefix='.gc-native-install-',delete=False) as f:
  temp=Path(f.name)
 shutil.copyfile(src,temp);temp.chmod(0o755);os.replace(temp,dst)
 assert digest(src)==digest(dst)
rows=[]
def run(name,cmd,want=None):
 start=time.monotonic();p=subprocess.run(list(map(str,cmd)),cwd=root,capture_output=True,text=True)
 row=dict(name=name,command=list(map(str,cmd)),seconds=time.monotonic()-start,exit=p.returncode,stdout=p.stdout,stderr=p.stderr);rows.append(row)
 (out/'verification.json').write_text(json.dumps(rows,indent=2)+'\n')
 assert p.returncode==0,row
 if want is not None:assert p.stdout.strip()==str(want),row
 print(name,row['seconds'],p.stdout.strip(),flush=True);return p.stdout
run('select',[manager,'version','switch',label,'--profile','standard','--build','normal','--offline','--no-input'])
version=json.loads(run('version',[manager,'--json','--version']))
assert version['runtimeVersion']==label and version['managerVersion']==label
run('login-path',['bash','-lc','command -v wago; wago --json --version'])
flags=['--bare','--offline','--no-input']
fixtures=source/'tests/fixtures/wasm'
run('fib',[manager,'run',*flags,'--invoke','fib',fixtures/'fib.wasm',20],6765)
n=500000
for module,names in [('gc-allocation-mix',['scalar-structs','structs','alternating','alternating-default','alternating-fill']),('gc-shared-nursery-array',['alternating','alternating-default','alternating-fill','alternating-fill-dynamic'])]:
 artifact=out/(module+'.wago')
 run('build-'+module,[manager,'build',*flags,'--output',artifact,fixtures/(module+'.wasm')])
 for name in names:
  want=n*n if name=='alternating' else n*(n-1) if name in ('alternating-fill','alternating-fill-dynamic') else n*(n-1)//2
  run(module+'-'+name,[manager,'run',*flags,'--allow-native-artifact','--invoke',name,artifact,n],want&0xffffffff)
artifact=out/'gc-array-vector-access.wago'
run('build-vector',[manager,'build',*flags,'--output',artifact,fixtures/'gc-array-vector-access.wasm'])
run('vector-multiple-exports',[manager,'run',*flags,'--allow-native-artifact','--invoke','init','--invoke','read','--invoke','write',artifact,n,n],str(7*n)+'\n'+str(n-1))
artifact=out/'gc-cast-field-read.wago'
run('build-cast-field',[manager,'build',*flags,'--output',artifact,fixtures/'gc-cast-field-read.wasm'])
run('cast-numeric-vector',[manager,'run',*flags,'--allow-native-artifact','--invoke','init','--invoke','loop-i32','--invoke','loop-v128',artifact,n,n],str(7*n)+'\n'+str(22*n))
for count,value in [(0,7),(60,7),(61,0xffffffff),(4096,0xffffffff)]:
 run('dynamic-array-'+str(count),[manager,'run',*flags,'--allow-native-artifact','--invoke','dynamic-fill-check',out/'gc-shared-nursery-array.wago',count,value],1)
artifact=out/'gc-abstract-array-cast-len.wago'
run('build-abstract-array',[manager,'build',*flags,'--output',artifact,fixtures/'gc-abstract-array-cast-len.wasm'])
run('abstract-array-loop',[manager,'run',*flags,'--allow-native-artifact','--invoke','init','--invoke','loop',artifact,64,8192],64*8192)
run('abstract-array-live-locals',[manager,'run',*flags,'--allow-native-artifact','--invoke','init','--invoke','pressure',artifact,64,17],64+24*17+276)
artifact=out/'gc-numeric-leaf-inline.wago'
run('build-numeric-inline',[manager,'build',*flags,'--output',artifact,root/'.tmp/wago-next-gc-perf/gc-numeric-leaf-inline.wasm'])
run('numeric-inline-roots',[manager,'run',*flags,'--allow-native-artifact','--invoke','roots',artifact,17],1061)
run('numeric-inline-loop',[manager,'run',*flags,'--allow-native-artifact','--invoke','loop',artifact,8192],8192)
artifact=out/'gc-direct-forwarder-loop.wago'
run('build-forwarder',[manager,'build',*flags,'--output',artifact,root/'.tmp/wago-next-gc-perf/gc-direct-forwarder-loop.wasm'])
run('forwarder-loop',[manager,'run',*flags,'--allow-native-artifact','--invoke','loop',artifact,8192],7*8192)
old_artifact=root/'.tmp/wago-local-perf-2026-10-20/allocation-mix.wago'
run('old-native-artifact',[manager,'run',*flags,'--allow-native-artifact','--invoke','alternating',old_artifact,n],n*n&0xffffffff)
artifact=out/'gc-combined-reference-abi.wago'
run('build-combined-reference-abi',[manager,'build',*flags,'--output',artifact,root/'.tmp/wago-next-gc-perf/gc-combined-reference-abi.wasm'])
for entry in ['private_direct','addressable_direct','wrapper_indirect','wrapper_reference','terminal_indirect','terminal_reference']:
 run('combined-reference-abi-'+entry,[manager,'run',*flags,'--allow-native-artifact','--invoke',entry,artifact,17],25)
artifact=out/'gc-final-cast-array-get.wago'
run('build-final-cast-array-get',[manager,'build',*flags,'--output',artifact,fixtures/'gc-final-cast-array-get.wasm'])
for entry,want in [('loop',57344),('ref-loop',8192),('pressure',57344),('pin-entry',7),('packed-u',254),('half-u',65533)]:
 run('final-cast-array-get-'+entry,[manager,'run',*flags,'--allow-native-artifact','--invoke','init','--invoke',entry,artifact],want)
artifact=out/'gc-trapping-guard.wago'
run('build-trapping-guard',[manager,'build',*flags,'--output',artifact,fixtures/'gc-trapping-guard.wasm'])
for entry,args,want in [('loop',[],139264),('negative-loop',[],139264),('noninline-loop',[],139264),('roots',[1],17),('same-field',[1],4),('indexed-block',[1],4),('packed-u',[1],254),('pressure',[1,1,2,3,4,5,6,7,8],36)]:
 run('trapping-guard-'+entry,[manager,'run',*flags,'--allow-native-artifact','--invoke','init','--invoke',entry,artifact,*args],want)
artifact=out/'gc-test-field.wago'
run('build-test-field',[manager,'build',*flags,'--output',artifact,fixtures/'gc-test-field.wasm'])
for entry,args,want in [('loop',[8192],57344),('loop-nop',[8192],57344),('i64',[],9),('pressure',[1,2,3,4,5,6,7,8],36),('dirty',[],7),('root',[],17),('stack-root',[],17),('packed-s',[],-2),('packed-u',[],65533)]:
 run('test-field-'+entry,[manager,'run',*flags,'--allow-native-artifact','--invoke','init','--invoke',entry,artifact,*args],want)
run('test-field-wrong-loop',[manager,'run',*flags,'--allow-native-artifact','--invoke','wrong','--invoke','loop',artifact,8192],99*8192)
artifact=out/'gc-read-regions.wago'
run('build-read-regions',[manager,'build',*flags,'--output',artifact,fixtures/'gc-read-regions.wasm'])
for entry,args,want in [('loop',[8192],57344),('pressure-loop',[1,2,3,4,5,6,7,8,8192],57380),('read',[15],7),('forward',[15],7),('arms',[0],32),('arms',[1],23),('branch',[0],17),('branch',[2],23),('nested',[0],17),('nested',[1],23),('nested',[2],17),('late',[0],23),('join',[],19),('pressure',[1,2,3,4,5,6,7,8],43),('two-targets',[],24),('many',[],119),('home-branch',[1,2,3,4,5,6,7,8],151),('roots',[],1040),('stack-roots',[],1017),('pressure-root',[1,2,3,4,5,6,7,8],53)]:
 run('read-regions-'+entry+'-'+str(args),[manager,'run',*flags,'--allow-native-artifact','--invoke','init','--invoke',entry,artifact,*args],want)
run('read-regions-different',[manager,'run',*flags,'--allow-native-artifact','--invoke','init','--invoke','init-second','--invoke','different',artifact],58)
roots=json.loads(run('read-regions-exact-roots',[root/'.tmp/wago-next-gc-perf/local-admission',artifact]))
assert roots['Required'] and roots['Exact'],roots
artifact=out/'gc-value-checks.wago'
run('build-value-checks',[manager,'build',*flags,'--output',artifact,fixtures/'gc-value-checks.wasm'])
for entry,args,want in [('run',[],68),('small',[],14),('pressure',[],84),('copy',[],18),('children',[],48),('typed-children',[],18),('test',[],25),('test-wrong',[],99),('snapshot',[],20),('later',[],18),('after-call',[],34),('branch',[0],11),('branch',[1],17),('write',[],17),('loop',[8192],68*8192)]:
 run('value-checks-'+entry+'-'+str(args),[manager,'run',*flags,'--allow-native-artifact','--invoke','init','--invoke',entry,artifact,*args],want)
roots=json.loads(run('value-checks-exact-roots',[root/'.tmp/wago-next-gc-perf/local-admission',artifact]))
assert roots['Required'] and roots['Exact'],roots
manifest=dict(label=label,source_commit=commit,source_branch='dewdrop/local-gc-perf-20261020',manager=str(manager),runtime=str(runtime),manager_sha256=digest(manager),runtime_sha256=digest(runtime),previous_manager_sha256=old,builds=builds,checks=len(rows))
(out/'installation.json').write_text(json.dumps(manifest,indent=2)+'\n')
