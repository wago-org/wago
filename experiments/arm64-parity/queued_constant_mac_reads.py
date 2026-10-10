"""Gate MAC-only reuse, then compare it to broad reuse and the baseline."""
import os,pathlib,subprocess,time
root=pathlib.Path(__file__).resolve().parents[2];out=root/'experiments/arm64-parity';lock=str(pathlib.Path.home()/'benchmark-lock.py')
settings={'base':('0',''),'broad':('1',''),'mac':('1','mac')}
def run(name,binary,bench,label,benchtime):
 enabled,scope=settings[label]
 env=dict(os.environ,WAGO_PARITY_CORPUS=str((root/'../../Web/wasm.fyi/corpora/applications').resolve()),WAGO_ARM64_EXPERIMENT_CONST_READ=enabled,WAGO_ARM64_EXPERIMENT_CONST_READ_SCOPE=scope)
 with (out/(name+'.txt')).open('w') as log:p=subprocess.run(['python3',lock,'--wait','--owner','wago-root','--',binary,'-test.run','^$','-test.bench',bench,'-test.benchtime='+benchtime],cwd=root,env=env,stdout=log,stderr=subprocess.STDOUT)
 time.sleep(10)
 return p.returncode==0
for bounds,binary in [('explicit','/tmp/arm64-parity-const-mac-read-explicit.test'),('signals','/tmp/arm64-parity-const-mac-read.test')]:
 if not run('const-mac-'+bounds+'-oracles',binary,'BenchmarkParity/.*/Exec$','mac','1x'):raise SystemExit(1)
for rnd,order in enumerate([['base','mac','broad'],['broad','mac','base'],['mac','base','broad']]):
 for label in order:
  if not run(f'const-mac-round-{rnd}-setting-{label}','/tmp/arm64-parity-const-mac-read.test','BenchmarkParity/(video-dct|graphics-reed-solomon|vision-dilation|crypto-aes128|compiler-register-allocation|search-aho-corasick|hardware-prime-implicants|ml-inference|files-glob-match)/(Compile|Exec)$',label,'150ms'):raise SystemExit(1)
