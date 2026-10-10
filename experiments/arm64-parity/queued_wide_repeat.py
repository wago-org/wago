import os,subprocess,time,pathlib
root=pathlib.Path(__file__).resolve().parents[2];out=root/'experiments/arm64-parity'
for round_number,order in enumerate([['0','4','6'],['6','4','0'],['4','0','6']]):
 for setting in order:
  env=dict(os.environ,WAGO_PARITY_CORPUS='/Users/work/Code/Web/wasm.fyi/corpora/applications',WAGO_ARM64_EXPERIMENT_WIDE_PINS=setting)
  with (out/f'wide-pins-repeat-round-{round_number}-setting-{setting}.txt').open('w') as log:
   p=subprocess.run(['python3',str(pathlib.Path.home()/'benchmark-lock.py'),'--wait','--owner','wago-root','--','/tmp/arm64-parity-wide-clean.test','-test.run','^$','-test.bench','BenchmarkParity/(video-dct|graphics-reed-solomon|vision-dilation|crypto-aes128|compiler-register-allocation)/(Compile|Exec)$','-test.benchtime=250ms'],cwd=root,env=env,stdout=log,stderr=subprocess.STDOUT)
  if p.returncode:raise SystemExit(p.returncode)
  time.sleep(10)
