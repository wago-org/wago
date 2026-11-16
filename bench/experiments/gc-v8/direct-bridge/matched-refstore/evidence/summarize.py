import json,pathlib,statistics
p=pathlib.Path(__file__).resolve().parent;d=json.loads((p/'results.json').read_text());lines=['# Matched ABI3 reference-array store experiment','','Source '+d['provenance']['source_commit']+'. Same compiler/collector source and explicit64KiB Eden /8MiB per old semispace; only native allocator versus native refstore tag changes. Allocation elision off. Three fresh processes each,3warmup+5checked timed calls.','','| Workload | Variant | median ms | samples ms | timed minor/full | proc VmHWM MiB | Go heap MiB |','|---|---|---:|---|---|---:|---:|'];med={}
for case in ['struct-allocation-access','array-allocation-access','tree-survival']:
 for variant in ['native_alloc','native_refstore']:
  rows=[r for r in d['rows']if r['case']==case and r['variant']==variant]
  if any(r['returncode']or'result'not in r for r in rows):lines.append(f'|{case}|{variant}|FAILED|||||');continue
  rs=[r['result']for r in rows];times=[r['samples_ns'][0]/1e6 for r in rs];med[case,variant]=statistics.median(times)
  gc=[str(r['after']['MinorCollections']-r['before']['MinorCollections'])+'/'+str(r['after']['FullCollections']-r['before']['FullCollections'])for r in rs]
  lines.append(f'|{case}|{variant}|{statistics.median(times):.3f}|'+', '.join(f'{v:.3f}'for v in times)+'|'+', '.join(gc)+f'|{statistics.median(r["proc_VmHWM_kib"]/1024 for r in rs):.2f}|{statistics.median(r["go_after"]["HeapAlloc"]/1048576 for r in rs):.2f}|')
lines+=['','Exploratory native-store versus native-allocation median deltas:']
for case in ['struct-allocation-access','array-allocation-access','tree-survival']:
 if (case,'native_alloc')in med and(case,'native_refstore')in med:lines.append(f'- {case}: {(med[case,"native_refstore"]/med[case,"native_alloc"]-1)*100:+.1f}% elapsed time.')
lines+=['', 'Noise warning: array and tree samples show large opposing outliers; the array median delta is inconclusive and the tree control supplies no reliable speed claim. Struct samples were consistently lower in all three launches.', '', 'All successful launches require exact artifact SHA256, numeric checksums, expected per-call allocation counts, and actual collection. Full stdout/stderr, configurations, counters, binary hashes and /proc memory data are retained in rawJSON.','', 'This short isolated experimental comparison does not establish V8 parity or a final runtime performance guarantee. Heap policies are fixed; any nursery-size experiment must be reported separately. Direct copied/promoted-byte and scan counters remain uninstrumented. Process memory includes Go and runtime overhead.']
(p/'summary.md').write_text('\n'.join(lines)+'\n');print('\n'.join(lines))
