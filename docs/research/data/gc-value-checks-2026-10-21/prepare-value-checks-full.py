from pathlib import Path
import hashlib,io,json,os,shutil,subprocess,tarfile,time
root=Path('/home/dev/Projects/dewdrop');out=root/'.tmp/wago-value-checks-full';out.mkdir(exist_ok=False)
installed=root/'.tmp/wago-local-stack-2026-10-20/wago';candidate=root/'.tmp/wago-gc-value-checks';oldhost=root/'.tmp/wago-batch-cancel-full/host-source'
base='40cfab8d8ec32ca3eff1cd178fa746cb2eeb82b0';assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=installed,text=True).strip()==base
files=['src/core/compiler/backend/railshot/amd64/'+f for f in ['compile.go','driver.go','gc_direct.go','gc_resolver.go','gc_struct.go','gc_test_field.go','references.go','regalloc.go','gc_value_checks.go']]

archive=subprocess.check_output(['git','archive',base],cwd=installed);rows=[]
for variant in ['control','candidate']:
 directory=out/variant;directory.mkdir();source=directory/'source';source.mkdir();host=directory/'host-source';host.mkdir();start=time.monotonic()
 with tarfile.open(fileobj=io.BytesIO(archive)) as tar:tar.extractall(source,filter='data')
 delta={}
 if variant!='control':
  for name in files:
   content=(candidate/name).read_bytes();(source/name).write_bytes(content);delta[name]=hashlib.sha256(content).hexdigest()

 subprocess.run(['git','init','-q',str(source)],check=True)
 patch=root/'tools/wago-transform-host/patches/managed-isolated-fork.patch';subprocess.run(['git','-C',str(source),'apply','--check',str(patch)],check=True);subprocess.run(['git','-C',str(source),'apply',str(patch)],check=True)
 for path in [oldhost/'go.mod',oldhost/'go.sum',*oldhost.glob('*.go')]:shutil.copyfile(path,host/path.name)
 mod=host/'go.mod';oldsource=str(root/'.tmp/wago-batch-cancel-full/source');assert mod.read_text().count(oldsource)==1;mod.write_text(mod.read_text().replace(oldsource,str(source)))
 env=dict(os.environ,GOWORK='off',CGO_ENABLED='0',GOMAXPROCS='2',GOFLAGS='-buildvcs=false');binary=root/'.tmp/wago-local-perf-2026-10-20'/('value-checks-'+variant+'-host')
 cmd=['go','build','-mod=mod','-ldflags=-X main.runtimePatchIdentity=value-checks-'+variant+' -X main.workerPluginIdentity=value-checks-'+variant,'-o',str(binary),'.'];t=time.monotonic();p=subprocess.run(cmd,cwd=host,env=env,capture_output=True,text=True)
 row=dict(variant=variant,source_commit=base,candidate_production_delta=delta,command=cmd,seconds=time.monotonic()-t,total_prepare_seconds=time.monotonic()-start,exit=p.returncode,stdout=p.stdout,stderr=p.stderr,managed_patch_sha256=hashlib.sha256(patch.read_bytes()).hexdigest());rows.append(row)
 if not p.returncode:row['binary_sha256']=hashlib.sha256(binary.read_bytes()).hexdigest()
 (out/'build.json').write_text(json.dumps(rows,indent=2));print(json.dumps(row),flush=True);assert not p.returncode
