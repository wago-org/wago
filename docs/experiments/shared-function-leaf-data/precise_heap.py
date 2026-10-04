from pathlib import Path
import subprocess,os
p=Path(__file__).resolve().parent;root=p.parent.parent/'pr802-final'/'bench/suite'
for rev in ['A','R']:
 d=p/'heap-diagnostic'/rev/'precise-one';d.mkdir(parents=True,exist_ok=True)
 env=dict(os.environ,GOMAXPROCS='1',GOGC='100',GOMEMLIMIT='off',WAGO_SHARING_MEMORY='1',WAGO_SHARING_WORKERS='1',WAGO_SHARING_PROFILE_DIR=str(d))
 with(d/'run.log').open('w')as f:subprocess.run([str(p/(rev+'-suite.test')),'-test.run=^TestSharingMemory$','-test.v','-test.count=1','-test.memprofilerate=1'],cwd=root,env=env,stdout=f,stderr=subprocess.STDOUT,check=True)
 for phase in ['compile_release_270','released_2','runtime_released']:
  (d/(phase+'-top.txt')).write_text(subprocess.check_output(['go','tool','pprof','-top','-inuse_space','-nodefraction=0',str(p/(rev+'-suite.test')),str(d/(phase+'.heap'))],text=True))
print('PRECISE PROFILED')
