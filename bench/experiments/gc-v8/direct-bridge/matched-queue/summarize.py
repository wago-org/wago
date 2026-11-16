import pathlib,json,statistics
p=pathlib.Path(__file__).resolve().parent;d=json.loads((p/'results.json').read_text())
lines=['# Matched direct GC queue/allocation experiment','','Three alternating fresh processes per case/variant; 3 warmup calls then 5 checked calls. Go1.27.1, GOMAXPROCS=1, exact artifact digests and binary/source hashes in raw JSON. All variants use direct offsets,64KiB Eden,8MiB per old semispace (two old semispaces). Allocation elision disabled.','','| Workload | Variant | median ms | three samples ms | timed minor/full | proc VmHWM median MiB | Go heap median MiB |','|---|---|---:|---|---|---:|---:|']
med={}
for case in ['struct-allocation-access','array-allocation-access','tree-survival']:
 for variant in ['reads_before_queue','reads_after_queue','alloc_after_queue']:
  rows=[r for r in d['rows']if r['case']==case and r['variant']==variant]
  if any(r['returncode']or'result'not in r for r in rows):lines.append(f'|{case}|{variant}|FAILED|||||');continue
  rs=[r['result']for r in rows];times=[r['samples_ns'][0]/1e6 for r in rs];med[case,variant]=statistics.median(times)
  gc=[str(r['after']['MinorCollections']-r['before']['MinorCollections'])+'/'+str(r['after']['FullCollections']-r['before']['FullCollections'])for r in rs]
  rss=[r.get('proc_VmHWM_kib',0)/1024 for r in rs]
  lines.append(f'|{case}|{variant}|{statistics.median(times):.3f}|'+', '.join(f'{v:.3f}'for v in times)+'|'+', '.join(gc)+f'|{statistics.median(rss):.2f}|{statistics.median(r["go_after"]["HeapAlloc"]/1048576 for r in rs):.2f}|')
lines+=['','## Exploratory deltas','']
for case in ['struct-allocation-access','array-allocation-access','tree-survival']:
 if all((case,v)in med for v in ['reads_before_queue','reads_after_queue','alloc_after_queue']):
  q=(med[case,'reads_after_queue']/med[case,'reads_before_queue']-1)*100;a=(med[case,'alloc_after_queue']/med[case,'reads_after_queue']-1)*100
  lines.append(f'- {case}: queue {q:+.1f}%; native allocation after queue {a:+.1f}% (negative means lower elapsed time).')
lines+=['','This is a short exploratory collector/codegen comparison, not a V8 target test. Queue comparison uses the same native-read compiler; allocator comparison holds queue implementation fixed. Exact allocation count assertions cover every timed invocation. Struct is the scalar allocation treatment; array/tree are controls where scalar-struct allocation may not apply. Do not infer significance from three samples alone.','', 'Linux /proc VmHWM is used instead of inherited ru_maxrss; hashing streams small chunks. Process memory includes Go/compiler/runtime overhead, not just guest storage. Direct scan/copied/promoted-byte stats remain uninstrumented.']
(p/'summary.md').write_text('\n'.join(lines)+'\n');print('\n'.join(lines))
