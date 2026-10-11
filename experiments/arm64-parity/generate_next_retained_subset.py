"""Merge focused retained-image captures, retaining older unchanged images."""
import ast
from decimal import Decimal
import hashlib
import json
from pathlib import Path
from statistics import median

from generate_hour_remaining_subset import execution_pairs, historical_times
from generate_next_remaining_subset import add_capture

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent


def merge(rows, hashes, filename, names, rounds, protocol):
    raw = (HERE / filename).read_bytes()
    pairs = execution_pairs(raw)
    assert set(pairs) == set(names), (filename, set(pairs) ^ set(names))
    hashes.append((filename, hashlib.sha256(raw).hexdigest()))
    for name, corpus in names.items():
        sample = pairs[name]
        assert set(sample) == set(range(rounds))
        assert all(set(p) == {False, True} for p in sample.values())
        values = [sample[i][True] for i in range(rounds)]
        rows[corpus] = median(values), min(values), max(values), protocol


def merge_source_capture(rows, hashes, filename, names, pairs, protocol):
    """Select candidate/default samples from reversed source process pairs."""
    raw = (HERE / filename).read_bytes()
    samples = {}
    for line in raw.decode().splitlines():
        if not line.startswith('{'):
            continue
        row = json.loads(line, parse_float=Decimal)
        if row['phase'] != 'exec':
            continue
        value = Decimal(str(row['us']))
        assert row['workload'] in names, (filename, row['workload'])
        assert row['source'] in {'baseline', 'candidate'}
        assert type(row['pair']) is int and row['pair'] >= 0
        assert type(row['round']) is int and row['round'] == 0
        assert type(row['on']) is bool and value.is_finite() and value > 0
        key = row['workload'], row['pair'], row['source'], row['on']
        assert key not in samples, (filename, key)
        samples[key] = value
    expected = {(name, pair, source, on) for name in names for pair in range(pairs)
                for source in ['baseline', 'candidate'] for on in [False, True]}
    assert set(samples) == expected, (filename, set(samples) ^ expected)
    hashes.append((filename, hashlib.sha256(raw).hexdigest()))
    for name, corpus in names.items():
        # ON is the common loop-memory-base policy in both source compilers;
        # the candidate's CSET lineage option is enabled by its process default.
        values = [samples[name, pair, 'candidate', True] for pair in range(pairs)]
        rows[corpus] = median(values), min(values), max(values), protocol


def main():
    proof = (HERE / 'next-20261011-select-retain-native-proof.txt').read_text()
    assert '33 change,115 baseline-identical' in proof
    assert 'exactly7 same-width/same-register CBZ retargets' in proof
    rows, hashes = {}, []
    old = 'audio-adpcm video-dct files-glob-match compiler-register-allocation vision-components graphics-reed-solomon image-median document-edit-distance triangle-raster video-optical-flow compression-lzw geo-polygon-clipping stencil-wave-equation ml-knn language-register-vm vision-dilation'.split()
    new = 'search-aho-corasick stats-linear-regression ml-inference hardware-prime-implicants serialization-protobuf audio-fir grid-pathfinding ml-kmeans stats-bootstrap files-path-normalize hardware-boolean-anf ml-convolution'.split()
    add_capture(rows, hashes, 'hour-20261011-focused-exec.jsonl', {n: 'applications/' + n for n in old}, 8, '37174169 unchanged image; 8×150ms')
    add_capture(rows, hashes, 'next-20261011-gap-screen-exec.jsonl', {n: 'applications/' + n for n in new}, 6, '37174169 unchanged image; 6×125ms')
    add_capture(rows, hashes, 'hour-20261011-focused-core-exec.jsonl', {
        'modular-arithmetic': 'wago/libtommath/modular-arithmetic',
        'decimal-parse': 'wago/fastfloat/decimal-parse',
        'complex-roundtrip': 'wago/kissfft/complex-roundtrip',
    }, 6, '37174169 unchanged image; 6×150ms')

    select_names = {}
    inventory = HERE / 'integer-select-local-read/evidence/native-inventory.txt'
    for line in inventory.read_text().splitlines()[1:]:
        kind, name, _, _ = ast.literal_eval(line)
        corpus = name.replace('__', '/') if kind == 'core' else 'applications/' + name
        key = corpus if corpus in ['wago/coremark/2k-performance', 'wago/zlib/inflate', 'wago/zstd/decompress'] else corpus.rsplit('/', 1)[-1]
        assert key not in select_names
        select_names[key] = corpus
    assert len(select_names) == 32
    merge(rows, hashes, 'integer-select-local-read/affected-subset/all.jsonl', select_names, 4,
          'isolated select; integrated image identical; 4×100ms')
    cross_names = 'geo-point-in-polygon hardware-boolean-anf ml-kmeans geo-polygon-clipping compiler-register-allocation files-glob-match audio-adpcm'.split()
    cross = HERE / 'next-20261011-select-cross-native-proof.txt'
    assert cross.exists() and len(cross.read_text().splitlines()) == len(cross_names)
    merge(rows, hashes, 'next-20261011-select-cross-build.jsonl', {n: 'applications/' + n for n in cross_names}, 4,
          'three retained defaults; 4×150ms processes')

    dispatch_proof = (HERE / 'next-20261011-dispatch-retain-native-proof.txt').read_text()
    assert '34 changed,114 pristine37174169-identical' in dispatch_proof
    dispatch_names = 'language-register-vm compiler-register-allocation files-glob-match hardware-boolean-anf'.split()
    assert len((HERE / 'next-20261011-dispatch-cross-native-proof.txt').read_text().splitlines()) == 4
    merge(rows, hashes, 'next-20261011-dispatch-cross-build.jsonl', {n: 'applications/' + n for n in dispatch_names}, 4,
          'four retained defaults; 4×150ms processes')

    cset_inventory_name = 'cset-lineage/inventory/changes.json'
    cset_inventory_raw = (HERE / cset_inventory_name).read_bytes()
    cset_inventory = json.loads(cset_inventory_raw)
    assert set(cset_inventory) == {'app', 'core'}
    assert len(cset_inventory['app']) == 12 and len(cset_inventory['core']) == 2
    assert sum(r['changed_words'] for records in cset_inventory.values() for r in records) == 72
    cset_names = {}
    for kind, records in cset_inventory.items():
        for record in records:
            assert record['changed_words'] > 0 and record['changed_words'] % 2 == 0
            assert record['size'] > 0 and record['size'] % 4 == 0
            corpus = record['name'].replace('__', '/') if kind == 'core' else 'applications/' + record['name']
            key = corpus.rsplit('/', 1)[-1]
            assert key not in cset_names
            cset_names[key] = corpus
    assert len(cset_names) == 14
    cset_proof_name = 'cset-lineage/retained/native-proof.txt'
    cset_proof_raw = (HERE / cset_proof_name).read_bytes()
    cset_proof = cset_proof_raw.decode()
    assert 'all retained defaultON affected images exact frozen first/timed candidate' in cset_proof
    assert 'all rollbackOFF exact currentfour baseline' in cset_proof
    assert 'no new changes outside the14 qualified CSET-lineage images' in cset_proof
    subset_plan_name = 'cset-lineage/cset-lineage-subset.py'
    subset_plan = (HERE / subset_plan_name).read_bytes()
    assert "'-rounds','4','-budget','100ms'" in subset_plan.decode()
    cross_plan_name = 'cset-lineage/cset-lineage-cross-cheap.py'
    cross_plan = (HERE / cross_plan_name).read_bytes()
    assert 'for pair in range(6)' in cross_plan.decode()
    assert "'-option','loop-memory-base'" in cross_plan.decode()
    assert "'-rounds','1','-budget','150ms'" in cross_plan.decode()
    hashes += [(name, hashlib.sha256(raw).hexdigest()) for name, raw in [
        (cset_inventory_name, cset_inventory_raw), (cset_proof_name, cset_proof_raw),
        (subset_plan_name, subset_plan), (cross_plan_name, cross_plan)]]
    merge(rows, hashes, 'cset-lineage/subset/all.jsonl', cset_names, 4,
          'isolated CSET; final image identical; 4×100ms')
    source_names = {n: cset_names[n] for n in [
        'geo-polyline-simplify', 'particle-physics', 'hardware-prime-implicants',
        'ml-kmeans', 'modular-arithmetic', 'parse-structure']}
    source_names['audio-adpcm'] = 'applications/audio-adpcm'
    merge_source_capture(rows, hashes, 'cset-lineage/cross-cheap/all.jsonl', source_names, 6,
                         'CSET source; final image identical; 6×150ms processes')

    historical = historical_times(ROOT / 'steady-exec-wago-vs-w2c2.md')
    ranked = []
    for corpus, (value, low, high, protocol) in rows.items():
        assert corpus in historical, corpus
        comparator = historical[corpus]
        ranked.append((value / comparator, corpus, value, low, high, comparator, protocol))
    lines = ['# Retained ARM64 priority subset, 2026-10-11', '',
        f'Apple M4 Max, arm64. {len(ranked)} focused workloads ranked worst-to-best against historical w2c2. These captures cover the compiler with the five retained changes through `cset-lineage`, based on `7257c1c0`. No full timing sweep was run. Times are median microseconds per exact invocation after warmup; every raw observation remains available.', '',
        'Fourteen native-affected rows use the qualified CSET-lineage four-pair 100ms repeat. Its final six reversed-process 150ms source pairs override six overlapping cases and also refresh unchanged ADPCM. The source candidates contain the three earlier changes plus CSET lineage; qualification proves their measured images match the final images with all five retained changes, including preservation of the existing register-VM dispatch. Earlier three/four-change source pairs, isolated-select repeats and older captures remain only for images unchanged by CSET lineage. Dates/protocols differ, and ranges expose outliers. This is a priority list, not a simultaneously measured parity claim.', '',
        'w2c2 was not rerun: its microseconds come from the [complete historical device ranking](../../steady-exec-wago-vs-w2c2.md), source `main@700f5565`, captured 2026-10-09. Positive gaps mean Wago is slower. Workload contracts differ; compare each row only to its matching corpus.', '',
        '| Rank | Corpus workload | Retained Wago µs | Observed range µs | Historical w2c2 µs | Ratio | Gap | Wago capture |',
        '|---:|---|---:|---:|---:|---:|---:|---|']
    for rank, (ratio, corpus, value, low, high, comparator, protocol) in enumerate(sorted(ranked, reverse=True), 1):
        lines.append(f'| {rank} | `{corpus}` | {value:.6f} | {low:.6f}–{high:.6f} | {comparator:.6f} | {ratio:.3f}× | {(ratio-1)*100:+.1f}% | {protocol} |')
    lines += ['', 'Subsequent exploratory changes are excluded. OS-thread locking and QoS do not establish physical-core affinity. See [retained changes and compile costs](next-20261011-results.md), [CSET final native proof](cset-lineage/retained/native-proof.txt), and [latest source comparison](cset-lineage/cross-cheap/summary.md).', '', 'Source SHA256:', '']
    lines += [f'- `{name}`: `{digest}`' for name, digest in hashes]
    output = HERE / 'next-20261011-retained-subset.md'
    output.write_text('\n'.join(lines) + '\n')
    print(f'Wrote {output}: {len(ranked)} ranked workloads')


if __name__ == '__main__':
    main()
