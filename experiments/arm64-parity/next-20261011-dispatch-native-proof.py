"""Check the fourth retained option has no unexpected integrated image changes."""
from pathlib import Path
import base64
import json

root = Path(__file__).resolve().parent
images = [r['image'] for r in json.loads((root / 'profile-bounded-branch-index-on/images.json').read_text()) if r.get('image')]
assert len(images) == 1
vm = base64.b64decode(images[0]['code'])
assert len(vm) == 1224
total, changed, new = 0, [], []
for kind, expected in [('core', 46), ('app', 102)]:
    on = Path('/tmp/next-20261011-dispatch-on-' + kind)
    off = Path('/tmp/next-20261011-dispatch-off-' + kind)
    previous = Path('/tmp/next-20261011-select-on-' + kind)
    base = Path('/tmp/hour-20261011-' + kind)
    files = list(on.glob('*.bin'))
    assert len(files) == expected
    assert {p.name for p in files} == {p.name for p in off.glob('*.bin')}
    for p in files:
        original = (base / p.name).read_bytes()
        actual = p.read_bytes()
        assert (off / p.name).read_bytes() == original, p.name
        prior = (previous / p.name).read_bytes()
        wanted = vm if p.name == 'language-register-vm.bin' else prior
        assert actual == wanted, p.name
        total += 1
        if actual != original:
            changed.append(p.name)
        if actual != prior:
            new.append(p.name)
assert total == 148 and len(changed) == 34 and new == ['language-register-vm.bin']
lines = ['All148 combined default-ON SIGNAL images qualified;34 changed,114 pristine37174169-identical.',
         'All four options OFF reproduce every pristine37174169 SIGNAL image.',
         'Relative to1bb0757b, only register VM changes;1224B image exactly matches independently timed/qualified/profiled bounded-index image.',
         'Every other integrated image matches the prior three-option qualified inventory.']
(root / 'next-20261011-dispatch-retain-native-proof.txt').write_text('\n'.join(lines) + '\n')
print('\n'.join(lines))
