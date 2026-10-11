"""Compare the frozen bounded-index candidate with the actual 37174169 compiler."""
import json
import os
from pathlib import Path
import subprocess

root = Path(__file__).resolve().parent
prefix = 'bounded-branch-index-cross'
names = 'language-register-vm,compiler-register-allocation,hardware-boolean-anf,files-glob-match'
rows = []
for phase in ['exec', 'compile']:
    for pair in range(4):
        for candidate in [bool(pair % 2), not bool(pair % 2)]:
            state = 'candidate' if candidate else 'baseline'
            binary = '/tmp/paired-bounded-branch-index' if candidate else '/tmp/paired-next-20261011-base'
            env = dict(os.environ, WAGO_SHARED_SCALAR='0',
                WAGO_ARM64_EXPERIMENT_BOUNDED_BRANCH_INDEX=str(int(candidate)))
            raw = subprocess.run([binary, '-option', 'loop-memory-base', '-workloads', names,
                '-phase', phase, '-rounds', '1', '-budget', '150ms',
                '-code-dir', '/tmp/' + prefix + '-' + state], env=env,
                text=True, capture_output=True, check=True).stdout
            (root / f'{prefix}-{phase}-{pair}-{state}.txt').write_text(raw)
            for line in raw.splitlines():
                if line.startswith('{'):
                    row = json.loads(line)
                    if row['on']:
                        row['on'], row['round'] = candidate, pair
                        rows.append(row)
            (root / (prefix + '-build.jsonl')).write_text(''.join(json.dumps(row) + '\n' for row in rows))
proof = []
for name in names.split(','):
    for state, option_state in [('candidate', 1), ('baseline', 0)]:
        image = Path('/tmp/' + prefix + '-' + state) / (name + '-1.bin')
        expected = Path('/tmp/bounded-branch-index-images') / (name + f'-{option_state}.bin')
        assert image.read_bytes() == expected.read_bytes(), (name, state)
    proof.append(name + ': both process images match the screened option inventories.')
(root / (prefix + '-native-proof.txt')).write_text('\n'.join(proof) + '\n')
with (root / (prefix + '-summary.md')).open('w') as log:
    subprocess.run(['python3', str(root / 'summarize_paired.py'),
        str(root / (prefix + '-build.jsonl'))], check=True, stdout=log)
summary = root / (prefix + '-summary.md')
summary.write_text(summary.read_text().replace('Paired ratios retain the alternating same-thread comparisons;',
    'Ratios pair separate processes in reversed source-build order, selecting loop-memory-base ON in both binaries;'))
