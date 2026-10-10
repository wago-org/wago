"""Gate and compare shifted-add multiplication and constant-planner candidates."""
import os,pathlib,subprocess,time
root=pathlib.Path(__file__).resolve().parents[2];out=root/'experiments/arm64-parity';lock=str(pathlib.Path.home()/'benchmark-lock.py')
settings={'base':('0','0'),'heavy':('1','0'),'scaled':('0','1'),'both':('1','1')}
def run(name,binary,bench,label,benchtime):
 heavy,scaled=settings[label]
 env=dict(os.environ,WAGO_PARITY_CORPUS=str((root/'../../Web/wasm.fyi/corpora/applications').resolve()),WAGO_ARM64_EXPERIMENT_CONST_HEAVY=heavy,WAGO_ARM64_EXPERIMENT_SCALED_CONST_MUL=scaled,WAGO_ARM64_EXPERIMENT_SELECT_GROUP_ENTRY='0')
 with (out/(name+'.txt')).open('w') as log:p=subprocess.run(['python3',lock,'--wait','--owner','wago-root','--',binary,'-test.run','^$','-test.bench',bench,'-test.benchtime='+benchtime],cwd=root,env=env,stdout=log,stderr=subprocess.STDOUT)
 time.sleep(10)
 return p.returncode==0
passing=[]
for label in ['heavy','scaled','both']:
 okay=True
 for bounds,binary in [('explicit','/tmp/arm64-parity-scaled-heavy-explicit.test'),('signals','/tmp/arm64-parity-scaled-heavy.test')]:
  okay=run('scaled-heavy-'+label+'-'+bounds+'-oracles',binary,'BenchmarkParity/.*/Exec$',label,'1x') and okay
 if okay:passing.append(label)
for rnd,order in enumerate([['base','scaled','heavy','both'],['both','heavy','scaled','base'],['heavy','base','both','scaled']]):
 for label in order:
  if label!='base' and label not in passing:continue
  if not run(f'scaled-heavy-round-{rnd}-setting-{label}','/tmp/arm64-parity-scaled-heavy.test','BenchmarkParity/(video-dct|graphics-reed-solomon|vision-dilation|crypto-aes128|compiler-register-allocation|search-aho-corasick|hardware-prime-implicants|ml-inference|files-glob-match)/(Compile|Exec)$',label,'150ms'):raise SystemExit(1)
