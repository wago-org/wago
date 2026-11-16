import json
import pathlib
import statistics
p = pathlib.Path(__file__).resolve().parent
d = json.loads((p / 'results.json').read_text())
cases = ['struct-allocation-access', 'array-allocation-access', 'array-fill', 'array-copy-checksum', 'array-loop-copy-checksum', 'tree-survival']
lines = ['# Full-protocol experimental direct pipeline versus V8', '', 'Source `' + d['provenance']['source_commit'] + '`. Wago uses explicit ' + str(d['provenance']['eden_bytes'] // 1024) + ' KiB Eden and two 8 MiB old semispaces, with allocation elision disabled. One candidate combines checked native fill with trusted numeric-array access, reference-struct access/allocation and native numeric-array allocation. Each fresh process performs 100 warmup invocations and 9 timed batches of 32 invocations. Three process rounds rotate workload and engine order. V8 uses `--no-liftoff`; exact versions and binary/artifact hashes are in raw JSON.', '', '| Workload | Engine | Median ms/call | Process medians ms | Ratio to V8 | <=1.15x gate | Timed minor/full | Median peak RSS MiB |', '|---|---|---:|---|---:|---|---|---:|']
for case in cases:
    groups = {}
    for engine in ['candidate', 'v8']:
        rows = [r for r in d['rows'] if r['case'] == case and r['variant'] == engine]
        assert len(rows) == 3 and all(r['returncode'] == 0 and 'result' in r for r in rows)
        groups[engine] = [r['result'] for r in rows]
    v8 = statistics.median(statistics.median(r['samples_ns']) for r in groups['v8'])
    for engine, rs in groups.items():
        times = [statistics.median(r['samples_ns']) / 1e6 for r in rs]
        median = statistics.median(times)
        ratio = median * 1e6 / v8
        if engine == 'v8':
            gc = 'not instrumented'
            rss = statistics.median(r['memory']['VmHWM_kib'] / 1024 for r in rs)
            gate = 'reference'
        else:
            gc = ', '.join(str(r['after']['MinorCollections']-r['before']['MinorCollections']) + '/' + str(r['after']['FullCollections']-r['before']['FullCollections']) for r in rs)
            rss = statistics.median(r['proc_VmHWM_kib'] / 1024 for r in rs)
            gate = 'PASS' if ratio <= 1.15 else 'MISS'
        lines.append(f'| {case} | {engine} | {median:.3f} | ' + ', '.join(f'{t:.3f}' for t in times) + f' | {ratio:.3f}x | {gate} | {gc} | {rss:.2f} |')
lines += ['', '## Audit protocol', '', 'The timed protocol remains 100 warmups and 9 x 32 calls for both policies. The primary audit uses 100 warmups. The 2 MiB sensitivity audit uses 128 warmups because 100 copy-kernel calls allocate only 1,843,200 bytes, below the 2 MiB nursery; the original audit stopped solely on the no-collection guard, with its raw record preserved. No runtime/checksum failure occurred. The same candidate binary SHA is used for both policies. The audit-only driver adjustment happened after building ba95cd3 and does not change compiled source.', '', '## Outcome', '', 'The per-workload table above is the acceptance gate; any miss means the corpus target is not achieved. The measured V8 version is 13.6.233.17-node.58 under Node 24.22.1. Actual collection counts are reported without assuming major collections occur at either nursery policy.', '', '## Scope and caveats', '', 'This is an end-to-end experimental engine comparison, not an isolated collector-efficiency proof or the default Wago runtime. Retained exported roots constrain the latest iteration but do not prove identical internal allocation behavior in V8. Every per-workload target miss is retained; no aggregate substitutes for the <=1.15x gate. Numerical checksums, exact artifacts and expected Wago allocation counts are verified for all six cases.', '', 'Compilation is excluded. Process RSS includes distinct engine/host overhead and only resident pages, not an equalized memory budget. V8 collection counts and copied/promoted bytes are not instrumented. Wago direct copied/promoted-byte and scan counters are also uninstrumented. Do not infer equivalent collection schedules, allocation-elision behavior in V8, peak pause guarantees or universal engine superiority from this corpus. Go host GC is invoked outside timing after warmup; V8 uses its ordinary heap policy without a forced GC. Worker builds/tests were held during the measurement window; host scheduling/frequency remain uncontrolled.', '']
(p / 'summary.md').write_text('\n'.join(lines))
print('\n'.join(lines))
