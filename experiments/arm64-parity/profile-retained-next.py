import json,os,subprocess
from pathlib import Path
os.environ['WAGO_SHARED_SCALAR']='0'
root=Path('/Users/work/Code/Web/wasm.fyi/corpora/applications')
manifest=json.loads((root/'manifest.json').read_text())
os.environ['WAGO_PARITY_CACHE']='/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache'
os.environ['WAGO_PARITY_CORE_CODE_DIR']='/tmp/arithmetic-zero-rollback-core'
subprocess.run(['go','test','-tags','wago_guardpage','-c','-o','/tmp/parity-arithmetic-zero-rollback.test','./experiments/arm64-parity'],check=True,cwd='/tmp/wago-retained-before-logical-immediate')
with Path('experiments/arm64-parity/arithmetic-zero-rollback-core.txt').open('w') as log:
 subprocess.run(['/tmp/parity-arithmetic-zero-rollback.test','-test.run=^TestCachedCoreOracles$'],stdout=log,stderr=subprocess.STDOUT,check=True)
images=list(Path('/tmp/arithmetic-zero-rollback-core').glob('*.bin'))
assert len(images)==46
assert all(p.read_bytes()==(Path('/tmp/arithmetic-zero-off-core')/p.name).read_bytes() for p in images)
Path('experiments/arm64-parity/arithmetic-zero-rollback-native.txt').write_text('46 core native images byte-identical to qualified disabled prototype baseline.\n')
binary='/tmp/wago-parity-retained-next-prof'
subprocess.run(['go','build','-tags','wago_runtime,wago_profile,wago_guardpage','-o',binary,'./cli/wago'],check=True,cwd='/tmp/wago-retained-before-logical-immediate')
for name in ['compiler-register-allocation','geo-point-in-polygon','audio-adpcm']:
 w=next(w for w in manifest if w['id']=='applications/'+name)
 out=Path('experiments/arm64-parity/profile-retained-next-'+name)
 cmd=[binary,'profile','record','--module',str(root/w['artifact']),'--export',w['export'],'--args',','.join(map(str,w['args'])),'--want',','.join(map(str,w['oracle']['expected'])),'--bounds','signals','--mode','prepared','--duration','3s','--backend','samply','--samply','/opt/homebrew/bin/samply','--rate','1000','--include-code','--source-maps','--out',str(out)]
 with Path(str(out)+'.txt').open('w') as log:subprocess.run(cmd,stdout=log,stderr=subprocess.STDOUT,check=True)
 with Path(str(out)+'-assembly.txt').open('w') as log:subprocess.run(['/tmp/wago-profile-tools/bin/python','experiments/arm64-parity/sampled_assembly.py',str(out)],stdout=log,check=True)
