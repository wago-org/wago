from pathlib import Path
import re,statistics
p=Path(__file__).parent
print("| BLAKE3 input bytes | Phase | Off median µs | On median µs | Median paired delta |")
print("|---|---|---:|---:|---:|")
for length in (1024,16384,102400):
 samples={}
 for rnd in range(6):
  for on in (0,1):
   raw=(p/f'regional-memory-read-blake-{length}-{rnd}-{on}.txt').read_text()
   assert '\nPASS\n' in raw, 'incomplete vector run'
   for phase in ('Compile','Exec'):
    hits=re.findall(r'BenchmarkCachedContract/'+phase+r'-\d+\s+\d+\s+([\d.]+) ns/op',raw)
    assert len(hits)==1, (length,rnd,on,phase,hits)
    samples[rnd,on,phase]=float(hits[0])/1000
 for phase in ('Compile','Exec'):
  off=[samples[r,0,phase] for r in range(6)]
  on=[samples[r,1,phase] for r in range(6)]
  delta=statistics.median([(b/a-1)*100 for a,b in zip(off,on)])
  print(f'| {length} | {phase.lower()} | {statistics.median(off):.4f} | {statistics.median(on):.4f} | {delta:+.2f}% |')
print('\nSix alternating process pairs per length, exact byte oracle before and after execution. Separate-process harness results are exploratory; no physical-core affinity is established. Negative means faster. Raw observations are retained.')
