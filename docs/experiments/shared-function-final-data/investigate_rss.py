from pathlib import Path
import subprocess,os,json
p=Path(__file__).parent;orig=(p/'runtime_memory.go').read_text()
s=orig.replace('"runtime"','"runtime"\n"runtime/pprof"\n"path/filepath"')
s=s.replace('runtime.KeepAlive(mods)','if dir:=os.Getenv("WAGO_RSS_DIAGNOSTIC_DIR"); dir!="" { data,err:=os.ReadFile("/proc/self/smaps");must(err);must(os.WriteFile(filepath.Join(dir,phase+".smaps"),data,0600));f,err:=os.Create(filepath.Join(dir,phase+".heap"));must(err);must(pprof.WriteHeapProfile(f));must(f.Close()) }; runtime.KeepAlive(mods)')
(p/'runtime_memory_profile.go').write_text(s);env=dict(os.environ,GOCACHE=str(p/'go-cache'),GOFLAGS='-buildvcs=false',GOMAXPROCS='1',GOGC='100',GOMEMLIMIT='off');runs=[]
for rev,root in [('M',p.parent/'pr802-main'),('R',p.parent/'pr802-rebased'),('P',p.parent/'pr802-final')]:
 b=p/(rev+'-runtime-profile');cmd=['go','build','-trimpath','-ldflags=-s -w','-o',str(b),str(p/'runtime_memory_profile.go')]
 subprocess.run(cmd,cwd=root,env=env,check=True)
 outdir=p/(rev+'-rss-diagnostic');outdir.mkdir(exist_ok=True)
 with(p/(rev+'-rss-diagnostic.txt')).open('w')as out:res=subprocess.run([str(b)],cwd=root,env=dict(env,WAGO_RSS_DIAGNOSTIC_DIR=str(outdir)),stdout=out,stderr=subprocess.STDOUT)
 print(rev,res.returncode,flush=True);runs.append(dict(revision=rev,command=cmd,exit=res.returncode))
(p/'rss-diagnostic-runs.json').write_text(json.dumps(runs,indent=2))
