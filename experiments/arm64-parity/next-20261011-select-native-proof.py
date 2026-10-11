"""Verify the integrated retained images and isolated select proof."""
from pathlib import Path
import struct

root = Path(__file__).resolve().parent
total, changed, interaction = 0, [], []
for kind, expected in [('core', 46), ('app', 102)]:
    on = Path('/tmp/next-20261011-select-on-' + kind)
    off = Path('/tmp/next-20261011-select-off-' + kind)
    base = Path('/tmp/hour-20261011-' + kind)
    previous = Path('/tmp/next-20261011-on-' + kind)
    isolated = Path('/tmp/integer-select-retained-signal-1-' + kind)
    files = list(on.glob('*.bin'))
    assert len(files) == expected
    assert {p.name for p in files} == {p.name for p in off.glob('*.bin')}
    for p in files:
        original = (base / p.name).read_bytes()
        actual = p.read_bytes()
        assert (off / p.name).read_bytes() == original, p.name
        select = (isolated / p.name).read_bytes()
        wanted = select if select != original else (previous / p.name).read_bytes()
        if p.name == 'hardware-boolean-anf.bin':
            assert len(actual) == len(select) == 1888
            differences = []
            for pc in range(0, len(actual), 4):
                a, b = struct.unpack_from('<I', select, pc)[0], struct.unpack_from('<I', actual, pc)[0]
                if a == b:
                    continue
                assert a & 0x7e000000 == b & 0x7e000000 == 0x34000000
                assert (a ^ b) & ~(0x7ffff << 5) == 0
                differences.append(pc)
            assert len(differences) == 7, differences
            interaction.append('Boolean ANF: isolated select image1888B plus exactly7 same-width/same-register CBZ retargets at ' + ', '.join(hex(pc) for pc in differences))
        else:
            assert actual == wanted, p.name
        total += 1
        if actual != original:
            changed.append((kind, p.name))
assert total == 148 and len(changed) == 33, (total, changed)
lines = [f'{total} combined default-ON SIGNAL images qualified; {len(changed)} change,115 baseline-identical.',
         'All three options OFF reproduce every pristine37174169 SIGNAL image.',
         'All other combined images exactly match isolated select or prior qualified two-option images.',
         *interaction]
(root / 'next-20261011-select-retain-native-proof.txt').write_text('\n'.join(lines) + '\n')
print('\n'.join(lines))
