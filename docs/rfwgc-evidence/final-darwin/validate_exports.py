#!/usr/bin/env python3
"""Cross-build and census only. Validate an immutable Git archive; never execute foreign binaries."""
import argparse, gzip, hashlib, json, os, pathlib, re, subprocess
import contracts
p=argparse.ArgumentParser(description=__doc__)
p.add_argument('source',type=pathlib.Path);p.add_argument('baseline',type=pathlib.Path);p.add_argument('output',type=pathlib.Path)
a=p.parse_args();a.output.mkdir(parents=True,exist_ok=False)
root=pathlib.Path(__file__).resolve().parents[3];go=pathlib.Path(os.environ['GOROOT'])/'bin/go'
env=dict(os.environ,GOTOOLCHAIN='local',GOWORK='off',GOENV='off',CGO_ENABLED='0',PATH=str(go.parent)+os.pathsep+os.environ['PATH'])
rows=[]
def command(label,argv,cwd,extra={}):
 with (a.output/(label+'.log')).open('w') as log:
  result=subprocess.run(argv,cwd=cwd,env=dict(env,**extra),stdout=log,stderr=subprocess.STDOUT)
 rows.append(dict(label=label,argv=argv,cwd=str(cwd),exit_code=result.returncode))
 (a.output/'commands.json').write_text(json.dumps(rows,indent=2)+'\n')
 return result.returncode

def attest(folder,commit,label):
 names=subprocess.check_output(['git','ls-tree','-r','--format=%(objecttype) %(objectname) %(path)',commit],cwd=root,text=True)
 result={}
 # Batch blobs for a byte-for-byte attestation without a process per file.
 entries=[line.split(' ',2) for line in names.splitlines() if line.startswith('blob ')]
 proc=subprocess.Popen(['git','cat-file','--batch'],cwd=root,stdin=subprocess.PIPE,stdout=subprocess.PIPE)
 for _,oid,name in entries:
  proc.stdin.write((oid+'\n').encode());proc.stdin.flush();size=int(proc.stdout.readline().split()[2]);data=proc.stdout.read(size);assert proc.stdout.read(1)==b'\n'
  actual=(folder/name).read_bytes();assert data==actual,name
  result[name]=hashlib.sha256(actual).hexdigest()
 proc.stdin.close();assert proc.wait()==0
 (a.output/(label+'-manifest.json.gz')).write_bytes(gzip.compress((json.dumps(result,sort_keys=True)+'\n').encode(),mtime=0))
 return result
before={}
for label,folder,pin in [('source',a.source,contracts.SPEC['source']),('baseline',a.baseline,contracts.SPEC['baseline'])]:before[label]=attest(folder,pin,label)
for target,folder,baseline in [('darwin',a.source,False),('windows',a.source,False),('darwin-baseline',a.baseline,True)]:
 darwin=target.startswith('darwin');osname='darwin' if darwin else 'windows';arch='arm64' if darwin else 'amd64'
 fixture='graph_test.go' if darwin else 'windows_admission_test.go'
 replacements=contracts.darwin_overlays(folder) if darwin and not baseline else {}
 replacements[str(folder/'src/wago/qualification_supplement_test.go')]=str(contracts.REV/'_fixtures'/fixture)
 overlay=a.output/(target+'-overlay.json');overlay.write_text(json.dumps({'Replace':replacements},indent=2)+'\n')
 expected=['TestRFWStage3MeasurementGuest'] if baseline else contracts.SPEC['tests'][osname]
 for mode in ['ordinary','wagodebug','wago_regalloccheck']:
  flags=['-mod=readonly','-overlay='+str(overlay)]+([] if mode=='ordinary' else ['-tags='+mode]);extra=dict(GOOS=osname,GOARCH=arch)
  label=target+'-'+mode
  assert command(label+'-census',[str(go),'list',*flags,'-json','./src/wago'],folder,extra)==0
  package=json.loads((a.output/(label+'-census.log')).read_text());names=[]
  for file in package['TestGoFiles']:
   path=str(folder/'src/wago'/file);text=pathlib.Path(replacements.get(path,path)).read_text()
   names+=re.findall(r'^func (Test\w+)\(',text,re.M)
  assert all(names.count(n)==1 for n in expected),(label,set(expected)-set(names))
  (a.output/(label+'-exact-selection.json')).write_text(json.dumps({'selected':expected,'absent':sorted(set(expected)-set(names)),'ignored_files':package.get('IgnoredGoFiles',[])},indent=2)+'\n')
  assert command(label+'-compile',[str(go),'test','-c',*flags,'-o',str(a.output/(label+'.test')),'./src/wago'],folder,extra)==0
  (a.output/(label+'.test')).unlink() # Build output isn't runtime evidence.
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 flags=[] if mode=='ordinary' else ['-tags='+mode]
 assert command('installer-'+mode,[str(go),'test','-mod=readonly',*flags,'-count=1','./...'],a.source/'cli/wago-installer')==0
 assert command('root-installer-'+mode,[str(go),'test','-mod=readonly',*flags,'-count=1','-run=^TestGoInstallBuildsNamedInstallerCommand$','.'],a.source,{'GOFLAGS':'-mod=readonly'})==0
assert command('installer-verify',[str(go),'mod','verify'],a.source/'cli/wago-installer')==0
for label,folder,pin in [('source',a.source,contracts.SPEC['source']),('baseline',a.baseline,contracts.SPEC['baseline'])]:assert before[label]==attest(folder,pin,label+'-after')
(a.output/'result.json').write_text(json.dumps({'status':'PASS','source':contracts.SPEC['source'],'native_darwin':'NOT_RUN','native_windows':'NOT_RUN','immutable_source_blobs':len(before['source']),'immutable_baseline_blobs':len(before['baseline']),'cross_builds':9,'local_installer_checks':6},indent=2)+'\n')
