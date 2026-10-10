"""Gate blended wide-pin placement, then compare a focused subset."""
import os,pathlib,subprocess,time
root=pathlib.Path(__file__).resolve().parents[2];out=root/'experiments/arm64-parity';lock=str(pathlib.Path.home()/'benchmark-lock.py')
def run(name,binary,bench,label,benchtime):
 env=dict(os.environ,WAGO_PARITY_CORPUS=str((root/'../../Web/wasm.fyi/corpora/applications').resolve()),WAGO_ARM64_EXPERIMENT_WIDE_PINS='0' if label=='0' else '4',WAGO_ARM64_EXPERIMENT_WIDE_CROSS='1',WAGO_ARM64_EXPERIMENT_WIDE_BLEND=label)
 with (out/(name+'.txt')).open('w') as log:p=subprocess.run(['python3',lock,'--wait','--owner','wago-root','--',binary,'-test.run','^$','-test.bench',bench,'-test.benchtime='+benchtime],cwd=root,env=env,stdout=log,stderr=subprocess.STDOUT)
 time.sleep(10)
 return p.returncode==0
passing=[]
for label in ['2','4']:
 okay=True
 for bounds,binary in [('explicit','/tmp/arm64-parity-wide-blend-explicit.test'),('signals','/tmp/arm64-parity-wide-blend.test')]:
  okay=run('wide-blend-'+label+'-'+bounds+'-oracles',binary,'BenchmarkParity/.*/Exec$',label,'1x') and okay
 if okay:passing.append(label)
for rnd,order in enumerate([['0','2','4'],['4','2','0'],['2','0','4']]):
 for label in order:
  if label!='0' and label not in passing:continue
  if not run(f'wide-blend-round-{rnd}-setting-{label}','/tmp/arm64-parity-wide-blend.test','BenchmarkParity/(video-dct|graphics-reed-solomon|vision-dilation|crypto-aes128|compiler-register-allocation)/(Compile|Exec)$',label,'250ms'):raise SystemExit(1)
