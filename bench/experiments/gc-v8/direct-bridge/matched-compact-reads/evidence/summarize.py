import json,pathlib,statistics,re
p=pathlib.Path(__file__).resolve().parent;d=json.loads((p/'results.json').read_text())
lines=['# Bounded compact native-read diagnostic','','Measured source `4fc3bbbc`, runtime `b7b6c3b2`, including Payload single-resolution fix in BOTH variants. Same-source compact helpers versus checked compact native reads; allocation/stores/fill/copy remain helper paths. Explicit 2 MiB Eden and two 8 MiB old semispaces. All12 separate128-warmup real-collection audits and36 bounded diagnostic processes passed. Timed diagnostic protocol is THREE warmup calls plus FIVE timed calls, three rotating fresh-process rounds, not the historical full protocol. Copy diagnostics explicitly permit zero collection only after the separate audited collection run. Exact hashes, checksums and allocation counts still apply.','','| Case | Mode | Median ms | Samples ms | Timed minor/full | Go heap MiB | RSS MiB |','|---|---|---:|---|---|---:|---:|']
for c in dict.fromkeys(r['case']for r in d['rows']):
 for v in ['helpers','reads']:
  rs=[r['result']for r in d['rows']if r['case']==c and r['variant']==v];ts=[statistics.median(r['samples_ns'])/1e6 for r in rs]
  gc=', '.join(sorted(set(str(r['after']['MinorCollections']-r['before']['MinorCollections'])+'/'+str(r['after']['FullCollections']-r['before']['FullCollections'])for r in rs)))
  lines.append(f'| {c} | {v} | {statistics.median(ts):.3f} | '+', '.join(f'{t:.3f}'for t in ts)+f' | {gc} | {statistics.median(r["go_after"]["HeapAlloc"]for r in rs)/1048576:.2f} | {statistics.median(r["proc_VmHWM_kib"]for r in rs)/1024:.2f} |')
lines+=['','## Interpretation','','Struct and tree improve consistently: every native-read sample is below every helper sample. Array allocation regresses consistently: every native-read sample exceeds every helper sample. Fill and both copy cases overlap or have mixed directions. Do not claim a broad gain or infer V8 parity. The stage selectively removes supported struct reads, not the remaining helper-heavy paths; these bounded timings are for intermediate attribution only. Future native allocation/store work needs its own qualification and evidence.','','## Separate Compile+Close footprint','','| Case | Mode | Median us/op | Code bytes | Go B/op | Allocs/op |','|---|---|---:|---:|---:|---:|']
g={}
for r in json.loads((p/'compile.json').read_text())['rows']:
 for l in r['stdout'].splitlines():
  m=re.match(r'BenchmarkGCDirectCompileProbe/(\S+)\s+256\s+(\d+) ns/op\s+(\d+) code-B\s+(\d+) B/op\s+(\d+) allocs/op',l)
  if m:g.setdefault((m[1],r['variant']),[]).append(list(map(int,m.groups()[1:])))
for c in dict.fromkeys(r['case']for r in d['rows']):
 for v in ['helpers','reads']:
  t,code,b,a=[statistics.median(x)for x in zip(*g[c,v])];lines.append(f'| {c} | {v} | {t/1000:.3f} | {code} | {b} | {a} |')
lines+=['','Compile probes are separate, 256 Compile+Close operations per case and three rotating processes. They are not peak-memory measurements. No V8 runs were included. Full target acceptance requires a later full-protocol sufficiently-native pipeline; this diagnostic cannot supply it. Worker CPU activity was excluded, but shared-host frequency/scheduling remain uncontrolled. Exact configurations, counters, hashes and raw outputs are retained.','']
(p/'summary.md').write_text('\n'.join(lines).replace('All12','All 12').replace('separate128','separate 128').replace('and36','and 36'))
