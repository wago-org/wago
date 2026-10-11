"""Pristine-source comparison; select the common loop-memory-base ON state."""
import argparse
import json
import os
from pathlib import Path
import subprocess

root = Path(__file__).resolve().parent
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--candidate', default='/tmp/paired-taken-zero-branch')
parser.add_argument('--prefix', default='taken-zero-branch-cross')
parser.add_argument('--phase', choices=['both', 'exec', 'compile'], default='both')
parser.add_argument('--budget', default='150ms')
parser.add_argument('--rounds', type=int, default=4)
args = parser.parse_args()
names = 'hardware-boolean-anf,audio-adpcm,files-glob-match,compiler-register-allocation'
rows = []
for phase in (['exec', 'compile'] if args.phase == 'both' else [args.phase]):
    for pair in range(args.rounds):
        for candidate in [bool(pair % 2), not bool(pair % 2)]:
            state = 'candidate' if candidate else 'baseline'
            env = dict(os.environ, WAGO_SHARED_SCALAR='0', WAGO_ARM64_EXPERIMENT_TAKEN_ZERO_BRANCH=str(int(candidate)))
            binary = args.candidate if candidate else '/tmp/paired-next-20261011-base'
            raw = subprocess.run([binary, '-option', 'loop-memory-base', '-workloads', names,
                '-phase', phase, '-rounds', '1', '-budget', args.budget, '-code-dir', '/tmp/' + args.prefix + '-' + state],
                env=env, text=True, capture_output=True, check=True).stdout
            (root / f'{args.prefix}-{phase}-{pair}-{state}.txt').write_text(raw)
            for line in raw.splitlines():
                if not line.startswith('{'):
                    continue
                row = json.loads(line)
                if not row['on']:
                    continue
                row['on'], row['round'] = candidate, pair
                rows.append(row)
            (root / (args.prefix + '-build.jsonl')).write_text(''.join(json.dumps(row) + '\n' for row in rows))
proof = []
for name in names.split(','):
    for state, inventory in [('candidate', '/tmp/taken-zero-branch-on-app'), ('baseline', '/tmp/hour-20261011-app')]:
        image = (Path('/tmp/' + args.prefix + '-' + state) / (name + '-1.bin')).read_bytes()
        assert image == (Path(inventory) / (name + '.bin')).read_bytes(), (name, state)
    proof.append(name + ': both process images match exact qualified source inventories.')
(root / (args.prefix + '-native-proof.txt')).write_text('\n'.join(proof) + '\n')
output = root / (args.prefix + '-summary.md')
with output.open('w') as log:
    subprocess.run(['python3', str(root / 'summarize_paired.py'), str(root / (args.prefix + '-build.jsonl'))], check=True, stdout=log)
output.write_text(output.read_text().replace('Paired ratios retain the alternating same-thread comparisons;',
    'Ratios pair separate processes in reversed source-build order, selecting loop-memory-base ON in both binaries;'))
