"""Confirm retained default versus its rollback on the focused subset."""
import os,pathlib,subprocess,time
root=pathlib.Path(__file__).resolve().parents[2];out=root/'experiments/arm64-parity'
for rnd,order in enumerate([['1','0'],['0','1'],['1','0']]):
 for rollback in order:
  env=dict(os.environ,WAGO_PARITY_CORPUS=str((root/'../../Web/wasm.fyi/corpora/applications').resolve()),WAGO_ARM64_NO_WIDE_LOCAL_PINS=rollback)
  with (out/f'wide-default-round-{rnd}-rollback-{rollback}.txt').open('w') as log:
   p=subprocess.run(['python3',str(pathlib.Path.home()/'benchmark-lock.py'),'--wait','--owner','wago-root','--','/tmp/arm64-parity-wide-default.test','-test.run','^$','-test.bench','BenchmarkParity/(video-dct|graphics-reed-solomon|vision-dilation|crypto-aes128|compiler-register-allocation)/(Compile|Exec)$','-test.benchtime=250ms'],cwd=root,env=env,stdout=log,stderr=subprocess.STDOUT)
  if p.returncode:raise SystemExit(p.returncode)
  time.sleep(10)
