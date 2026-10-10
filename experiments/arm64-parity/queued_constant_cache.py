"""Gate the larger immutable constant cache before timing focused applications."""
import os,pathlib,subprocess,time
root=pathlib.Path(__file__).resolve().parents[2];out=root/'experiments/arm64-parity';lock=str(pathlib.Path.home()/'benchmark-lock.py')
def run(name,binary,bench,capacity,benchtime):
 env=dict(os.environ,WAGO_PARITY_CORPUS=str((root/'../../Web/wasm.fyi/corpora/applications').resolve()),WAGO_ARM64_EXPERIMENT_CONST_CACHE=capacity)
 with (out/(name+'.txt')).open('w') as log:p=subprocess.run(['python3',lock,'--wait','--owner','wago-root','--',binary,'-test.run','^$','-test.bench',bench,'-test.benchtime='+benchtime],cwd=root,env=env,stdout=log,stderr=subprocess.STDOUT)
 time.sleep(10)
 return p.returncode==0
for bounds,binary in [('explicit','/tmp/arm64-parity-six-const-explicit.test'),('signals','/tmp/arm64-parity-six-const.test')]:
 if not run('six-const-'+bounds+'-oracles',binary,'BenchmarkParity/.*/Exec$','6','1x'):raise SystemExit(1)
for rnd,order in enumerate([['4','6'],['6','4'],['4','6']]):
 for capacity in order:
  if not run(f'six-const-round-{rnd}-capacity-{capacity}','/tmp/arm64-parity-six-const.test','BenchmarkParity/(video-dct|graphics-reed-solomon|vision-dilation|crypto-aes128|compiler-register-allocation|search-aho-corasick|hardware-prime-implicants|ml-inference|files-glob-match)/(Compile|Exec)$',capacity,'150ms'):raise SystemExit(1)
