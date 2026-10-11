"""Verify final images against the independently qualified default-ON source."""
from pathlib import Path

HERE = Path(__file__).resolve().parent
lines = []
for bounds in ('signal', 'explicit'):
    total = 0
    for kind, count in (('core', 46), ('app', 102)):
        actual = Path(f'/tmp/final-pr-{bounds}-{kind}')
        expected = HERE / f'cset-lineage/retained/{bounds}-1-{kind}'
        names = {p.name for p in expected.glob('*.bin')}
        assert len(names) == count, (bounds, kind, len(names))
        assert names == {p.name for p in actual.glob('*.bin')}
        for name in names:
            assert (actual / name).read_bytes() == (expected / name).read_bytes(), (bounds, kind, name)
        total += len(names)
        lines.append(f'{bounds} {kind}: all {count} final images exactly match qualified retained default-ON source')
    assert total == 148
changed = unchanged = 0
for kind in ('core', 'app'):
    actual = Path(f'/tmp/final-pr-signal-{kind}')
    baseline = Path(f'/tmp/hour-20261011-{kind}')
    assert {p.name for p in actual.glob('*.bin')} == {p.name for p in baseline.glob('*.bin')}
    for path in actual.glob('*.bin'):
        if path.read_bytes() == (baseline / path.name).read_bytes():
            unchanged += 1
        else:
            changed += 1
lines.append(f'Five post-37174169 defaults: {changed} changed, {unchanged} pristine37174169-identical signal images')
output = HERE / 'final-pr-native-proof.txt'
output.write_text('\n'.join(lines) + '\n')
print(output.read_text(), end='')
