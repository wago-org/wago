"""Profile the frozen qualified baseline, with exact output and native proofs."""
import base64
import json
from pathlib import Path
import subprocess

BASE = Path('/Users/work/Code/Web/wasm.fyi/corpora/applications')
OUT = Path(__file__).resolve().parent
manifest = json.loads((BASE / 'manifest.json').read_text())
proof = []
for name in ['files-glob-match', 'hardware-boolean-anf', 'ml-kmeans']:
    workload = next(w for w in manifest if w['id'] == 'applications/' + name)
    prefix = 'profile-next-20261011-' + name
    with (OUT / (prefix + '.txt')).open('w') as log:
        subprocess.run([
            '/tmp/wago-next-20261011-profile', 'profile', 'record',
            '--module', str(BASE / workload['artifact']),
            '--export', workload['export'],
            '--args', ','.join(map(str, workload['args'])),
            '--want', ','.join(map(str, workload['oracle']['expected'])),
            '--bounds', 'signals', '--mode', 'prepared', '--duration', '3s',
            '--backend', 'samply', '--samply', '/opt/homebrew/bin/samply',
            '--rate', '1000', '--include-code', '--source-maps',
            '--out', str(OUT / prefix),
        ], check=True, stdout=log, stderr=subprocess.STDOUT)
    images = [row['image'] for row in json.loads((OUT / prefix / 'images.json').read_text()) if row.get('image')]
    assert len(images) == 1, name
    code = base64.b64decode(images[0]['code'])
    assert code == (Path('/tmp/hour-20261011-app') / (name + '.bin')).read_bytes(), name
    proof.append(f'{name}: exact oracle passes; {len(code)}-byte profile image matches qualified37174169.')
    with (OUT / (prefix + '-assembly.txt')).open('w') as log:
        subprocess.run(['/tmp/wago-profile-tools/bin/python', str(OUT / 'sampled_assembly.py'), str(OUT / prefix)], check=True, stdout=log)
(OUT / 'next-20261011-additional-profile-proof.txt').write_text('\n'.join(proof) + '\n')
