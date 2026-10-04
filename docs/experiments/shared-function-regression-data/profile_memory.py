from pathlib import Path
import subprocess,os,json
p=Path(__file__).parent;e=p.parent;r=e.parent/'pr802-final/bench/suite';env=dict(os.environ,GOMAXPROCS='1',GOGC='100',GOMEMLIMIT='off',WAGO_SHARING_MEMORY='1');env.pop('WAGO_SHARED_SCALAR',None);runs=[]
for rev in ['M','P','F']:
 d=p/(rev+'-heap-diagnostic');d.mkdir(exist_ok=True);b=(p if rev=='F' else e)/(rev+'-suite.test');cmd=[str(b),'-test.run=^TestSharing(Memory|GCObservation)$','-test.v']
 with(p/(rev+'-heap-diagnostic.txt')).open('w') as f:res=subprocess.run(cmd,cwd=r,env=dict(env,WAGO_SHARING_PROFILE_DIR=str(d)),stdout=f,stderr=subprocess.STDOUT)
 runs.append(dict(revision=rev,command=cmd,exit=res.returncode))
 for phase in ['compile_release_270','released_2','runtime_released','engine_alive_gc0','engine_alive_gc3','engine_closed_extra_gc']:
  with(p/(rev+'-'+phase+'-heap-top.txt')).open('w') as f:subprocess.run(['go','tool','pprof','-top','-inuse_space',str(b),str(d/(phase+'.heap'))],stdout=f,stderr=subprocess.STDOUT,check=True)
(p/'profile-memory-runs.json').write_text(json.dumps(runs,indent=2))
