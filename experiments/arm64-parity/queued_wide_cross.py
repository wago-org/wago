import os,subprocess,time,pathlib
root=pathlib.Path(__file__).resolve().parents[2];out=root/'experiments/arm64-parity';lock=str(pathlib.Path.home()/'benchmark-lock.py')
def run(name,cmd,setting='4',cross='1'):
 env=dict(os.environ,WAGO_PARITY_CORPUS='/Users/work/Code/Web/wasm.fyi/corpora/applications',WAGO_ARM64_EXPERIMENT_WIDE_PINS=setting,WAGO_ARM64_EXPERIMENT_WIDE_CROSS=cross)
 with (out/(name+'.txt')).open('w') as log:p=subprocess.run(['python3',lock,'--wait','--owner','wago-root','--',*cmd],cwd=root,env=env,stdout=log,stderr=subprocess.STDOUT)
 time.sleep(10)
 return p.returncode==0
if not run('wide-cross-build-explicit',['go','test','-c','-o','/tmp/arm64-parity-wide-cross-explicit.test','./experiments/arm64-parity']):raise SystemExit(1)
for label,binary in [('explicit','/tmp/arm64-parity-wide-cross-explicit.test'),('signals','/tmp/arm64-parity-wide-cross.test')]:
 if not run('wide-cross-'+label+'-oracles',[binary,'-test.run','^$','-test.bench','BenchmarkParity/.*/Exec$','-test.benchtime=1x']):raise SystemExit(1)
for rnd,order in enumerate([['0','1'],['1','0'],['0','1']]):
 for cross in order:
  if not run(f'wide-cross-round-{rnd}-setting-{cross}',['/tmp/arm64-parity-wide-cross.test','-test.run','^$','-test.bench','BenchmarkParity/(video-dct|graphics-reed-solomon|vision-dilation|crypto-aes128|compiler-register-allocation)/(Compile|Exec)$','-test.benchtime=250ms'],setting='0' if cross=='0' else '4',cross=cross):raise SystemExit(1)
