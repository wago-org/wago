"""Gate late-only shifted immediate selection before focused comparison."""
import os,pathlib,subprocess,time
root=pathlib.Path(__file__).resolve().parents[2];out=root/'experiments/arm64-parity';lock=str(pathlib.Path.home()/'benchmark-lock.py')
def run(name,binary,bench,enabled,benchtime):
 env=dict(os.environ,WAGO_PARITY_CORPUS=str((root/'../../Web/wasm.fyi/corpora/applications').resolve()),WAGO_ARM64_EXPERIMENT_SHIFTED_IMM=enabled,WAGO_ARM64_EXPERIMENT_SHIFTED_IMM_SCOPE='late')
 with (out/(name+'.txt')).open('w') as log:p=subprocess.run(['python3',lock,'--wait','--owner','wago-root','--',binary,'-test.run','^$','-test.bench',bench,'-test.benchtime='+benchtime],cwd=root,env=env,stdout=log,stderr=subprocess.STDOUT)
 time.sleep(10)
 return p.returncode==0
for bounds,binary in [('explicit','/tmp/arm64-parity-shifted-late-explicit.test'),('signals','/tmp/arm64-parity-shifted-late.test')]:
 if not run('shifted-late-'+bounds+'-oracles',binary,'BenchmarkParity/.*/Exec$','1','1x'):raise SystemExit(1)
for rnd,order in enumerate([['0','1'],['1','0'],['0','1']]):
 for enabled in order:
  if not run(f'shifted-late-round-{rnd}-enabled-{enabled}','/tmp/arm64-parity-shifted-late.test','BenchmarkParity/(video-dct|graphics-reed-solomon|vision-dilation|crypto-aes128|compiler-register-allocation|search-aho-corasick|hardware-prime-implicants|ml-inference|files-glob-match)/(Compile|Exec)$',enabled,'150ms'):raise SystemExit(1)
