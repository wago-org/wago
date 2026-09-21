from pathlib import Path
import json,re,time
p=Path(__file__).resolve().parent
r={'utc':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'load':Path('/proc/loadavg').read_text().strip(),'memory_pressure':Path('/proc/pressure/memory').read_text(),'temperature':{str(f):f.read_text() for f in Path('/sys/class/thermal').glob('thermal_zone*/temp')}}
for label in ['baseline','candidate']:
 f=p/f'full-{label}.txt'
 if f.exists():
  lines=[l for l in f.read_text().splitlines() if l.startswith('Benchmark') and 'ns/op' in l]
  r[label]={'rows':len(lines),'last':lines[-1].split()[0] if lines else None,'pass':f.read_text().rstrip().endswith('PASS')}
with (p/'full-host-timeline.jsonl').open('a') as f:f.write(json.dumps(r)+'\n')
print(json.dumps(r))
