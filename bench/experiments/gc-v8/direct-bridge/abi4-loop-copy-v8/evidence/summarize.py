import json,pathlib,statistics,re
p=pathlib.Path(__file__).resolve().parent
cases=['struct-allocation-access','array-allocation-access','array-fill','array-copy-checksum','array-loop-copy-checksum','tree-survival']
lines=['# ABI4 loop/copy evidence','','Measured source `978d456`, runtime `6de74d7`. Same-source baseline and candidate both use ABI4; candidate additionally enables loop and checked-copy capabilities. Allocation elision is disabled. Both old semispaces remain 8 MiB. Full gates use 100 warmup calls, 9 batches of 32 calls and three rotating process rounds. The 2 MiB audit alone uses 128 warmups. Short A/B uses 100 warmups and five timed calls per process. All 36 audit processes and 108 execution timing processes passed. Separate compile measurements used three rotating processes per binary, 256 Compile+Close iterations per case.','']
for mode in ['matched','primary','sensitivity']:
 d=json.loads((p/mode/'results.json').read_text());lines += ['## '+mode,'', '| Case | Engine | Median ms | Process medians ms | Candidate/reference | Timed minor/full | Peak RSS MiB |','|---|---|---:|---|---:|---|---:|']
 for case in cases:
  groups={v:[r['result'] for r in d['rows'] if r['case']==case and r['variant']==v] for v in (['baseline','candidate'] if mode=='matched' else ['candidate','v8'])}
  ref=statistics.median(statistics.median(r['samples_ns']) for r in groups['baseline' if mode=='matched' else 'v8'])
  for v,rs in groups.items():
   ts=[statistics.median(r['samples_ns'])/1e6 for r in rs];m=statistics.median(ts)
   gc='unobserved' if v=='v8' else ', '.join(sorted(set(str(r['after']['MinorCollections']-r['before']['MinorCollections'])+'/'+str(r['after']['FullCollections']-r['before']['FullCollections']) for r in rs)))
   rss=statistics.median((r['memory']['VmHWM_kib'] if v=='v8' else r['proc_VmHWM_kib'])/1024 for r in rs)
   ratio=f'{m*1e6/ref:.3f}x' if v=='candidate' else 'reference'
   lines.append(f'| {case} | {v} | {m:.4f} | '+', '.join(f'{t:.4f}' for t in ts)+f' | {ratio} | {gc} | {rss:.2f} |')
 lines+=['']
lines+=['## Interpretation','','Every full-protocol workload still misses the 1.15x V8 threshold at both policies. The target is not achieved. The same-source A/B demonstrates consistent bulk-copy improvement, with every candidate sample below every baseline sample. Scalar-copy and other controls overlap; fill has a large candidate outlier. Struct is slower in every paired short round, despite unchanged emitted code size and allocation footprint; preserve that control rather than claiming universal gain. No aggregate hides target misses.','','## Separate compile footprint','','| Case | Variant | Median us/op | Code bytes | Go bytes/op | Go allocations/op |','|---|---|---:|---:|---:|---:|']
groups={}
for r in json.loads((p/'compile.json').read_text())['rows']:
 for l in r['stdout'].splitlines():
  m=re.match(r'BenchmarkGCDirectCompileProbe/(\S+)\s+256\s+(\d+) ns/op\s+(\d+) code-B\s+(\d+) B/op\s+(\d+) allocs/op',l)
  if m:groups.setdefault((m[1],r['variant']),[]).append(list(map(int,m.groups()[1:])))
for c in cases:
 for v in ['baseline','candidate']:
  ns,code,byt,alloc=[statistics.median(x) for x in zip(*groups[c,v])];lines.append(f'| {c} | {v} | {ns/1000:.3f} | {code} | {byt} | {alloc} |')
lines+=['','Copy artifacts add 896 native code bytes (+23.8%), 5185 Go bytes/compile (+7.2%) and 7 allocations/compile. Other artifacts have unchanged code size/Go allocations. Compile timing varies in controls, so do not infer a general compiler-speed improvement. This measures Compile+Close, not peak compiler RSS.','','## Limits','','V8 13.6.233.17-node.58 / Node 24.22.1 uses --no-liftoff. All exact artifact hashes, checksums and Wago allocation counters were verified. V8 internal allocation/elision and collection behavior are unobserved; retained roots do not prove equal optimizer allocation behavior. This is an end-to-end opt-in engine comparison, not isolated collector efficiency. Resident memory is not an equalized heap budget. Direct copied/promoted/scan counters are uninstrumented. Compilation is excluded from execution timings; Go host GC runs outside timing after warmup. CPU windows excluded worker builds/tests, but host scheduling/frequency remain uncontrolled. Raw samples, versions, counters and binary hashes are retained.','']
(p/'summary.md').write_text('\n'.join(lines))
