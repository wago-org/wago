import json
import pathlib
import statistics

p = pathlib.Path(__file__).resolve().parent
d = json.loads((p / 'results.json').read_text())
lines = ['# Direct nursery policy sensitivity', '',
    'Source `' + d['provenance']['source_commit'] + '`. One unchanged checked-refstore binary, allocation elision off. Eden varies explicitly; both old semispaces remain 8 MiB each. Three fresh processes per case/policy, each with 3 warmup calls and 5 checked timed calls. All 9 audit processes and 27 timing processes passed.', '',
    '| Workload | Eden KiB | Median ms/call | Samples ms/call | Timed minor/full | Median VmHWM MiB | Median Go heap MiB |',
    '|---|---:|---:|---|---|---:|---:|']
alloc = []
for case in ['struct-allocation-access', 'array-allocation-access', 'tree-survival']:
    for eden in [65536, 524288, 2097152]:
        rs = [r['result'] for r in d['rows'] if r['case'] == case and r['eden_bytes'] == eden]
        times = [r['samples_ns'][0] / 1e6 for r in rs]
        gc = sorted(set(str(r['after']['MinorCollections'] - r['before']['MinorCollections']) + '/' + str(r['after']['FullCollections'] - r['before']['FullCollections']) for r in rs))
        lines.append(f'| {case} | {eden // 1024} | {statistics.median(times):.3f} | ' + ', '.join(f'{v:.3f}' for v in times) + ' | ' + ', '.join(gc) + f' | {statistics.median(r["proc_VmHWM_kib"] / 1024 for r in rs):.2f} | {statistics.median(r["go_after"]["HeapAlloc"] / 1048576 for r in rs):.2f} |')
        alloc.append(f'| {case} | {eden // 1024} | {statistics.median(r["go_after"]["MallocsDelta"] / 5 for r in rs):.1f} | {statistics.median(r["go_after"]["TotalAllocDelta"] / 5 for r in rs):.1f} |')
lines += ['', '## Interpretation', '',
    '- Struct is consistently faster with either larger nursery. Array is consistently faster at 2 MiB than at 64 KiB across these launches; 512 KiB does not establish an array improvement. Tree samples overlap substantially and do not establish a reliable speedup.',
    '- This collector promotes every surviving nursery object; it does not yet have survivor spaces. A larger nursery changes when temporary objects die relative to collection and can avoid promotion, not merely reduce a fixed collection cost.',
    '- The 2 MiB struct run performs no major collection in the short timed window. Its lower resident peak does not mean a lower memory budget: one old semispace can remain untouched. A longer steady-state/forced-major experiment is required before selecting a default.',
    '- Go heap allocated rises from about 24.6 MiB to 27.4 MiB as the nursery and its metadata grow. Process VmHWM includes runtime/Go overhead and only resident pages; it is not reserved arena size.',
    '- The experiment changes heap policy, not code generation. It is not comparable to the historical V8 timing protocol and does not establish V8 parity. Keep 64 KiB as the explicit control in subsequent compiler comparisons; 2 MiB is a useful separately labeled sensitivity configuration, not an adopted default.', '',
    '## Host allocations per timed invocation', '',
    '| Workload | Eden KiB | Median mallocs/call | Median allocated bytes/call |', '|---|---:|---:|---:|'] + alloc + ['',
    'Exact artifact hashes, checksums, expected per-call allocation counts (262208 struct/array; 67551 tree), actual collection counters, binary hash, source version and complete stdout/stderr are retained in raw JSON. Direct copied/promoted-byte and scanned-object counters remain uninstrumented; their zeros do not mean zero work. All timed collection counts in each case/policy were identical across the three processes. Other worker builds/tests were held during this timing window; host scheduling/frequency remain uncontrolled.', '']
(p / 'summary.md').write_text('\n'.join(lines))
print('\n'.join(lines))
