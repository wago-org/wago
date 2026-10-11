from pathlib import Path
import base64,json,subprocess

cache=Path('/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache')
root=Path('experiments/arm64-parity').resolve()
w=next(x for x in json.loads((cache/'manifest.json').read_text())['workloads'] if x['id']=='wago/libtommath/modular-arithmetic')
assert not w.get('initialize') and not w.get('input') and not w.get('host_profile') and w['oracle']['kind']=='exact_u64'
out=root/'profile-next-20261011-libtommath'
with (root/'profile-next-20261011-libtommath.txt').open('w') as log:
 subprocess.run(['/tmp/wago-next-20261011-profile','profile','record','--module',str(cache/w['artifact']),'--export',w['export'],'--args',','.join(map(str,w['args'])),'--want',','.join(map(str,w['oracle']['expected'])),'--bounds','signals','--mode','prepared','--duration','3s','--backend','samply','--samply','/opt/homebrew/bin/samply','--rate','1000','--include-code','--source-maps','--out',str(out)],stdout=log,stderr=subprocess.STDOUT,check=True)
images=[e['image'] for e in json.loads((out/'images.json').read_text()) if e.get('image')]
assert len(images)==1
code=base64.b64decode(images[0]['code'])
assert code==(Path('/tmp/hour-20261011-core')/'wago__libtommath__modular-arithmetic.bin').read_bytes()
(root/'next-20261011-libtommath-native-proof.txt').write_text(f'Exact libtommath oracle passes; {len(code)}-byte profile image matches current qualified baseline.\n')
with (root/'profile-next-20261011-libtommath-assembly.txt').open('w') as log:
 subprocess.run(['/tmp/wago-profile-tools/bin/python',str(root/'sampled_assembly.py'),str(out)],stdout=log,check=True)
