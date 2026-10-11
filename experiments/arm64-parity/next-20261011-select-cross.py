"""Focused reversed-process source comparison of three retained defaults."""
import json
import os
from pathlib import Path
import subprocess

root = Path(__file__).resolve().parent
prefix = 'next-20261011-select-cross'
names = 'geo-point-in-polygon,hardware-boolean-anf,ml-kmeans,geo-polygon-clipping,compiler-register-allocation,files-glob-match,audio-adpcm'
rows = []
for phase in ['exec', 'compile']:
    for pair in range(4):
        for candidate in [bool(pair % 2), not bool(pair % 2)]:
            state = 'candidate' if candidate else 'baseline'
            binary = '/tmp/paired-next-20261011-select-retained' if candidate else '/tmp/paired-next-20261011-base'
            env = dict(os.environ, WAGO_SHARED_SCALAR='0')
            for key in ['WAGO_ARM64_NO_GUARDED_BOOLEAN_XOR', 'WAGO_ARM64_NO_TAKEN_ZERO_BRANCH', 'WAGO_ARM64_NO_INTEGER_SELECT_LOCAL_READ']:
                env.pop(key, None)
            raw = subprocess.run([binary, '-option', 'loop-memory-base', '-workloads', names,
                '-phase', phase, '-rounds', '1', '-budget', '150ms', '-code-dir', '/tmp/' + prefix + '-' + state],
                env=env, text=True, capture_output=True, check=True).stdout
            (root / f'{prefix}-{phase}-{pair}-{state}.txt').write_text(raw)
            for line in raw.splitlines():
                if not line.startswith('{'):
                    continue
                row = json.loads(line)
                if row['on']:
                    row['on'], row['round'] = candidate, pair
                    rows.append(row)
            (root / (prefix + '-build.jsonl')).write_text(''.join(json.dumps(row) + '\n' for row in rows))
proof = []
for name in names.split(','):
    for state, inventory in [('candidate', '/tmp/next-20261011-select-on-app'), ('baseline', '/tmp/hour-20261011-app')]:
        image = (Path('/tmp/' + prefix + '-' + state) / (name + '-1.bin')).read_bytes()
        assert image == (Path(inventory) / (name + '.bin')).read_bytes(), (name, state)
    proof.append(name + ': both timed source images match exact qualified inventories.')
(root / (prefix + '-native-proof.txt')).write_text('\n'.join(proof) + '\n')
output = root / (prefix + '-summary.md')
with output.open('w') as log:
    subprocess.run(['python3', str(root / 'summarize_paired.py'), str(root / (prefix + '-build.jsonl'))], check=True, stdout=log)
output.write_text(output.read_text().replace('Paired ratios retain the alternating same-thread comparisons;',
    'Ratios pair separate processes in reversed source-build order, selecting loop-memory-base ON in both binaries;'))
