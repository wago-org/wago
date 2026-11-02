#!/usr/bin/env python3
"""Verify retained receipt integrity and exact final test census, without execution."""
from pathlib import Path
import hashlib, json, re, gzip
root = Path(__file__).resolve().parent
for line in (root / 'SHA256SUMS').read_text().splitlines():
    digest, name = line.split('  ', 1)
    assert hashlib.sha256((root / name).read_bytes()).hexdigest() == digest, name
receipt = json.loads((root / 'receipt.json').read_text())
with gzip.open(root / 'source-manifest.json.gz', 'rt') as f:
    manifest = json.load(f)
assert len(manifest) == receipt['source_blob_count'] == 17790
census = json.loads((root / 'census.json').read_text())
for mode in ('ordinary', 'wagodebug', 'wago_regalloccheck'):
    for lane, count in (('focused', 24), ('diagnostics', 8)):
        name = f'review-{mode}-{lane}.log'
        text = (root / 'logs' / name).read_text()
        actual = {key: re.findall(r'^\s*--- ' + key + r': (\S+)', text, re.M)
                  for key in ('PASS', 'FAIL', 'SKIP')}
        assert actual == census[name]
        assert len(actual['PASS']) == count and not actual['FAIL'] and not actual['SKIP']
        assert text.rstrip().endswith('PASS')
negative = (root / 'logs/negative.log').read_text()
assert 'direct Go import local-state join differs at local 0' in negative
assert negative.rstrip().endswith('FAIL')
print('PASS: checksums, manifest count, six exact final censuses, negative control')
