"""Compare read-only reuse separately from an expanded constant cache."""
import os,pathlib,subprocess,time
root=pathlib.Path(__file__).resolve().parents[2];out=root/'experiments/arm64-parity';lock=str(pathlib.Path.home()/'benchmark-lock.py')
settings={'base':('4','0'),'read4':('4','1'),'read6':('6','1')}
def run(name,binary,bench,label,benchtime):
 capacity,read=settings[label]
 env=dict(os.environ,WAGO_PARITY_CORPUS=str((root/'../../Web/wasm.fyi/corpora/applications').resolve()),WAGO_ARM64_EXPERIMENT_CONST_CACHE=capacity,WAGO_ARM64_EXPERIMENT_CONST_READ=read)
 with (out/(name+'.txt')).open('w') as log:p=subprocess.run(['python3',lock,'--wait','--owner','wago-root','--',binary,'-test.run','^$','-test.bench',bench,'-test.benchtime='+benchtime],cwd=root,env=env,stdout=log,stderr=subprocess.STDOUT)
 time.sleep(10)
 return p.returncode==0
passing=[]
for label in ['read4','read6']:
 okay=True
 for bounds,binary in [('explicit','/tmp/arm64-parity-const-read-explicit.test'),('signals','/tmp/arm64-parity-const-read.test')]:
  okay=run('const-read-'+label+'-'+bounds+'-oracles',binary,'BenchmarkParity/.*/Exec$',label,'1x') and okay
 if okay:passing.append(label)
for rnd,order in enumerate([['base','read4','read6'],['read6','read4','base'],['read4','base','read6']]):
 for label in order:
  if label!='base' and label not in passing:continue
  if not run(f'const-read-round-{rnd}-setting-{label}','/tmp/arm64-parity-const-read.test','BenchmarkParity/(video-dct|graphics-reed-solomon|vision-dilation|crypto-aes128|compiler-register-allocation|search-aho-corasick|hardware-prime-implicants|ml-inference|files-glob-match)/(Compile|Exec)$',label,'150ms'):raise SystemExit(1)
