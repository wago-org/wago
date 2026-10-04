from pathlib import Path
import subprocess,os,json
p=Path(__file__).parent;source=p/'dump_join_test.go'
source.write_text('''package wagobench
import("testing";"os";"path/filepath";"fmt")
func TestSharingDumpJoin(t *testing.T){for _,f:=range sharingFixtures(){if f.name!="join"{continue};m,e:=wasmModuleForDump(f.bytes);if e!=nil{t.Fatal(e)};c,e:=benchCompileModule(m);if e!=nil{t.Fatal(e)};defer c.Close();dir:=os.Getenv("WAGO_DUMP_DIR");if e=os.WriteFile(filepath.Join(dir,"join.code"),c.Code,0600);e!=nil{t.Fatal(e)};t.Log(fmt.Sprintf("entries=%v",c.Entry))}}
'''.replace('import("testing";', 'import("github.com/wago-org/wago/src/core/compiler/wasm";"testing";').replace('wasmModuleForDump(f.bytes)','wasm.DecodeModule(f.bytes)').replace('c,e:=benchCompileModule(m)','if e=wasm.ValidateModule(m);e!=nil{t.Fatal(e)};c,e:=benchCompileModule(m)'))
env=dict(os.environ,GOCACHE=str(p/'go-cache'),GOFLAGS='-buildvcs=false');runs=[]
for rev,root in [('M',p.parent/'pr802-main'),('P',p.parent/'pr802-final')]:
 overlay=p/(rev+'-dump-overlay.json');overlay.write_text(json.dumps({'Replace':{str(root/'bench/suite/dump_join_test.go'):str(source)}}))
 b=p/(rev+'-dump.test');cmd=['go','test','-c','-overlay='+str(overlay),'-o',str(b),'./bench/suite'];subprocess.run(cmd,cwd=root,env=env,check=True)
 d=p/(rev+'-join-dump');d.mkdir(exist_ok=True)
 with(p/(rev+'-join-dump.txt')).open('w')as out:r=subprocess.run([str(b),'-test.run=^TestSharingDumpJoin$','-test.v'],cwd=root/'bench/suite',env=dict(env,WAGO_DUMP_DIR=str(d)),stdout=out,stderr=subprocess.STDOUT)
 with(p/(rev+'-join-disassembly.txt')).open('w')as out:subprocess.run(['objdump','-D','-b','binary','-m','i386:x86-64',str(d/'join.code')],stdout=out,check=True)
 runs.append(dict(revision=rev,command=cmd,exit=r.returncode))
(p/'join-dump-runs.json').write_text(json.dumps(runs,indent=2))
