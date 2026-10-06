#!/usr/bin/env python3
"""Rebase the preserved wide-setup01 Go overlay onto this exact checkout."""
import argparse
import hashlib
import json
from pathlib import Path
import shlex


def main():
    root = Path(__file__).resolve().parents[1]
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out', type=Path, required=True)
    parser.add_argument('--policy', choices=['candidate', 'retained'], default='candidate')
    args = parser.parse_args()
    archive = root / 'docs/performance/dragline-20261003'
    source = archive / 'wide-setup-01'
    metadata = json.loads((source / 'metadata.json').read_text())
    mapping = {}
    for relative, record in metadata['files'].items():
        file = source / record['archive']
        if hashlib.sha256(file.read_bytes()).hexdigest() != record['sha256']:
            raise ValueError('Source snapshot mismatch: ' + relative)
        mapping[str(root / relative)] = str(file)
    physical, effective = hashlib.sha256(), hashlib.sha256()
    paths = {p for p in root.rglob('*.go') if '.git' not in p.parts}
    for path in sorted(paths):
        physical.update(str(path.relative_to(root)).encode() + b'\0' + path.read_bytes())
    for path in sorted(paths | {Path(p) for p in mapping}):
        effective.update(str(path.relative_to(root)).encode() + b'\0')
        effective.update(Path(mapping.get(str(path), str(path))).read_bytes())
    if physical.hexdigest() != '41a6a8c99c7a1546cab3916bc250ced8159a770c259357d417c55b64ee13ec3f':
        raise ValueError('Checkout Go sources differ from the checkpoint; qualification cannot be reused')
    if effective.hexdigest() != metadata['effective_go_source_sha256']:
        raise ValueError('Effective overlay source hash mismatch')
    out = args.out.resolve()
    out.mkdir(parents=True, exist_ok=False)
    (out / 'overlay.json').write_text(json.dumps({'Replace': mapping}, indent=2) + '\n')
    unit = dict(mapping)
    unit[str(root / 'src/core/compiler/backend/dragline/a82a_wide_setup_amd64_test.go')] = str(source / 'unit-test.go.txt')
    (out / 'unit-overlay.json').write_text(json.dumps({'Replace': unit}, indent=2) + '\n')
    env = dict(GOTOOLCHAIN='local', GOWORK='off', GOFLAGS='-mod=readonly', GOMAXPROCS='1', WAGO_BOUNDS='signals', A82A_ROOT=str(root), A82A_SETUP_FIXTURES=str(source), A82A_WIDE_SETUP_POLICY=args.policy)
    for name in ['NESTED_LOOP', 'CALL_TREE', 'DIRECT_WRAPPER']:
        env['A82A_' + name + '_POLICY'] = 'retained'
    for name in ['BYTE_COPY', 'WIDE_EXIT', 'CONVERT_LOOP', 'CONVERT_DYNAMIC', 'VECTOR_REMAINDER', 'VECTOR_ROTATE', 'VECTOR_LITERAL_ALIGN', 'DIVISIBILITY']:
        env['A82A_' + name + '_POLICY'] = 'candidate'
    (out / 'env.sh').write_text('unset GOROOT A82A_FORCE_SCHEDULE A82A_MODULE_SHIFT\n' + ''.join(f'export {k}={shlex.quote(v)}\n' for k, v in env.items()))
    (out / 'source-verification.json').write_text(json.dumps({'physical_go_sha256': physical.hexdigest(), 'effective_go_sha256': effective.hexdigest(), 'policy': args.policy}, indent=2) + '\n')
    print('Prepared exact source overlay:', out / 'overlay.json')
    print('Environment:', out / 'env.sh')
    print('No build, tests, benchmarks, or source promotion performed.')


if __name__ == '__main__':
    main()
