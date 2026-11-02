#!/usr/bin/env python3
"""Local controls and Mach-O cross-builds only, never native evidence."""
import argparse,json,os,subprocess,sys
from pathlib import Path
import harness as h
p=argparse.ArgumentParser();p.add_argument('--source',type=Path,required=True);p.add_argument('--tinygo',type=Path,required=True);p.add_argument('--go',type=Path,required=True);p.add_argument('--output',type=Path,required=True);a=p.parse_args()
out=a.output.resolve();h.safe_output(out);out.mkdir(exist_ok=False);source=a.source.resolve()
h.write(out/'adapters.json',h.adapt(source));before=h.files(source)
h.write(out/'source-before.json',before)
h.write(out/'identity.json',dict(evidence='CROSS MACH-O ONLY; no native execution',source=h.PIN['source'],compiler=str(a.tinygo),compiler_sha256=h.sha(a.tinygo),patch_files={n:h.sha(a.tinygo.parent.parent/n) for n in h.PIN['after']},drivers={n:h.sha(h.E/n) for n in ['harness.py','validate.py','foreign_stack_arm64_test.go.in','cancellation_test.go.in']}))
controls=[]
for mode in h.MODES:
 required=['TestA','TestA/child']
 good='--- PASS: TestA\n    --- PASS: TestA/child\n'
 assert h.census(good,required)['ok']
 for name,text in [('missing',''),('duplicate',good+good),('skip',good+'--- SKIP: TestB\n'),('fail',good+'--- FAIL: TestB\n'),('unexpected',good+'--- PASS: TestB\n'),('vacuous',good+'wat2wasm-backed test is unavailable under TinyGo')]:
  assert not h.census(text,required)['ok'];controls.append(mode+':'+name)
try:h.native_host()
except ValueError:controls.append('wrong-platform-refused')
else:raise AssertionError('local verifier expected a non-Darwin host')
h.write(out/'controls.json',controls)
env=dict(PATH=str(a.go.parent)+':/usr/bin:/bin',HOME=os.environ['HOME'],GOROOT=str(a.go.parent.parent),GOOS='darwin',GOARCH='arm64',GOTOOLCHAIN='local',GOENV='off',GOWORK='off',GOFLAGS='-mod=readonly -p=2',GOMAXPROCS='2',GOCACHE=str(out/'go-cache'),XDG_CACHE_HOME=str(out/'cache'))
results=[]
for mode in h.MODES:
 for i,(package,required) in enumerate(h.suites(mode).items()):
  label=mode+'-'+str(i);artifact=out/(label+'.test')
  argv=[str(a.tinygo),'test','-c','-p=2','-scheduler=threads','-gc=conservative','-o',str(artifact)]
  if mode!='ordinary':argv+=['-tags='+mode]
  argv+=['./'+package]
  rc=h.command(out,label,argv,source,env,1200)
  row=dict(mode=mode,package=package,exit=rc,evidence='CROSS_ONLY',required=required)
  if rc==0:
   text=artifact.read_bytes();row['sha256']=h.sha(artifact)
   row['target_darwin_arm64']=text[:8]==bytes.fromhex('cffaedfe0c000001')
   # TinyGo test table retains exact top-level names; excludes vacuous selectors.
   row['missing_symbols']=[n for n in required if '/' not in n and n.encode() not in text]
  results.append(row);h.write(out/'results.json',results)
h.write(out/'integrity.json',dict(source_unchanged=h.files(source)==before))
assert h.files(source)==before
assert all(r['exit']==0 and r['target_darwin_arm64'] and not r['missing_symbols'] for r in results),results
