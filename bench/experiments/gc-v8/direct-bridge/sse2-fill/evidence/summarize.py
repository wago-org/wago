import json,pathlib,statistics,re
p=pathlib.Path(__file__).resolve().parent
lines=['# SSE2 fill matched experiment','','Source `aaf25f6`, runtime `861bbe1`. Same-source best ABI4 loop/copy baseline versus added SSE2 fill, with V8 as end-to-end reference. Primary Eden is 2 MiB; 512 KiB is a separate control. Both old semispaces remain 8 MiB. All 12 audits and 36 timing launches passed. Timed protocol stays 100 warmups, 9 batches of 32 calls, three rotating rounds. Audit-only warmups are 128 at 2 MiB. No CPU affinity changes were made mid-experiment.','']
for mode in ['primary','control']:
 d=json.loads((p/mode/'results.json').read_text());lines+=['## '+mode,'','| Case | Engine | Median ms | Process medians ms | Ratio to V8 | Timed minor/full | Peak RSS MiB |','|---|---|---:|---|---:|---|---:|']
 for c in ['array-fill','array-allocation-access']:
  groups={v:[r['result'] for r in d['rows'] if r['case']==c and r['variant']==v] for v in ['baseline','candidate','v8']}
  v8=statistics.median(statistics.median(r['samples_ns']) for r in groups['v8'])
  for v,rs in groups.items():
   ts=[statistics.median(r['samples_ns'])/1e6 for r in rs];med=statistics.median(ts)
   gc='unobserved' if v=='v8' else ', '.join(sorted(set(str(r['after']['MinorCollections']-r['before']['MinorCollections'])+'/'+str(r['after']['FullCollections']-r['before']['FullCollections']) for r in rs)))
   rss=statistics.median((r['memory']['VmHWM_kib'] if v=='v8' else r['proc_VmHWM_kib'])/1024 for r in rs)
   lines.append(f'| {c} | {v} | {med:.4f} | '+', '.join(f'{t:.4f}' for t in ts)+f' | {med*1e6/v8:.3f}x | {gc} | {rss:.2f} |')
 lines+=['']
lines+=['## Interpretation','','At 2 MiB, fill is lower in all three paired rounds, with a 3.8% median reduction; the array-allocation control overlaps and changes only +0.4% in median. At 512 KiB, fill improves 1.6% in median but rounds are mixed, while the control is noisy. This is modest evidence on a shared host, not a strong universal gain. Candidate fill remains 1.237x V8 at the primary policy and misses the <=1.15x gate. Do not promote a default based solely on this small effect. A later explicitly affinity-controlled repeat could reduce migration noise, but cannot eliminate shared-host variability.','','## Separate compile footprint','','| Case | Variant | Median us/op | Native bytes | Go B/op | Allocs/op |','|---|---|---:|---:|---:|---:|']
g={}
for r in json.loads((p/'compile.json').read_text())['rows']:
 for l in r['stdout'].splitlines():
  m=re.match(r'BenchmarkGCDirectCompileProbe/(\S+)\s+256\s+(\d+) ns/op\s+(\d+) code-B\s+(\d+) B/op\s+(\d+) allocs/op',l)
  if m:g.setdefault((m[1],r['variant']),[]).append(list(map(int,m.groups()[1:])))
for c in ['array-fill','array-allocation-access']:
 for v in ['baseline','candidate']:
  t,code,b,a=[statistics.median(x) for x in zip(*g[c,v])];lines.append(f'| {c} | {v} | {t/1000:.3f} | {code} | {b} | {a} |')
lines+=['','Fill adds 64 native code bytes (2479 to 2543) without added Go compile allocations/bytes; the array control is unchanged. Compile timings are reported, not treated as evidence of a compiler speedup.','','V8 uses --no-liftoff, Node 24.22.1 / V8 13.6.233.17-node.58. Exact artifact hashes, checksums, object allocation counts and actual Wago collections were verified. V8 internal allocation/elision and collection counters remain unobserved; memory budgets differ. This is an opt-in end-to-end subset comparison, not isolated collector efficiency or an all-six-workload target result. Profiling and worker builds were excluded from the window; host frequency/scheduling remain uncontrolled.','']
(p/'summary.md').write_text('\n'.join(lines))
