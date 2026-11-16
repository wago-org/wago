import json
import pathlib
import statistics

p = pathlib.Path(__file__).resolve().parent
d = json.loads((p / 'results.json').read_text())
lines = ['# Matched trusted scalar-read experiment', '',
    'Source `' + d['provenance']['source_commit'] + '`. Same source and collector, checked reference-store versus trusted scalar reads (policy 1). This does not enable trusted stores. Eden policies are explicit: 512 KiB primary, 64 KiB retained control; both old semispaces remain 8 MiB each. Allocation elision is off. Three fresh processes per case/policy/variant, with 3 warmup and 5 checked timed calls each.', '',
    '| Workload | Eden KiB | Variant | Median ms/call | Samples ms/call | Timed minor/full | Median VmHWM MiB | Median Go heap MiB |',
    '|---|---:|---|---:|---|---|---:|---:|']
medians = {}
alloc = []
for case in ['struct-allocation-access', 'array-allocation-access', 'tree-survival']:
    for eden in [65536, 524288]:
        for variant in ['checked_refstore', 'trusted_read']:
            rows = [r for r in d['rows'] if r['case'] == case and r['eden_bytes'] == eden and r['variant'] == variant]
            assert len(rows) == 3 and all(r['returncode'] == 0 and 'result' in r for r in rows)
            rs = [r['result'] for r in rows]
            times = [r['samples_ns'][0] / 1e6 for r in rs]
            medians[case, eden, variant] = statistics.median(times)
            gc = sorted(set(str(r['after']['MinorCollections'] - r['before']['MinorCollections']) + '/' + str(r['after']['FullCollections'] - r['before']['FullCollections']) for r in rs))
            lines.append(f'| {case} | {eden // 1024} | {variant} | {statistics.median(times):.3f} | ' + ', '.join(f'{v:.3f}' for v in times) + ' | ' + ', '.join(gc) + f' | {statistics.median(r["proc_VmHWM_kib"] / 1024 for r in rs):.2f} | {statistics.median(r["go_after"]["HeapAlloc"] / 1048576 for r in rs):.2f} |')
            alloc.append(f'| {case} | {eden // 1024} | {variant} | {statistics.median(r["go_after"]["MallocsDelta"] / 5 for r in rs):.1f} | {statistics.median(r["go_after"]["TotalAllocDelta"] / 5 for r in rs):.1f} |')
lines += ['', '## Median elapsed-time differences', '']
for case in ['struct-allocation-access', 'array-allocation-access', 'tree-survival']:
    for eden in [65536, 524288]:
        change = (medians[case, eden, 'trusted_read'] / medians[case, eden, 'checked_refstore'] - 1) * 100
        lines.append(f'- {case}, {eden // 1024} KiB: {change:+.1f}%.')
lines += ['', '## Interpretation', '', 'No broad improvement is established. The 64 KiB struct samples favor trusted reads in each paired round, but the primary 512 KiB struct samples do not. Array samples have large opposing noise; tree provides no reliable gain and has large trusted outliers at 512 KiB. Keep the mode experimental rather than selecting it based on median-only wins. Collection counts and host allocation traffic are identical within each matched case/policy.', '', 'These exploratory medians must be read together with all samples, not as confidence intervals or V8 comparisons. This short protocol does not establish steady-state performance, peak pauses or V8 parity. Host scheduling and frequency remain uncontrolled. All successful probes verify exact artifact hashes, checksums, expected per-call allocation counts and actual collection. Raw JSON retains stdout/stderr, binary hashes, configurations and complete counters. Direct copied/promoted-byte and scanned-object counters are not instrumented.', '', '## Host allocations per timed invocation', '', '| Workload | Eden KiB | Variant | Median mallocs/call | Median allocated bytes/call |', '|---|---:|---|---:|---:|'] + alloc + ['']
(p / 'summary.md').write_text('\n'.join(lines))
print('\n'.join(lines))
