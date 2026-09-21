#!/usr/bin/env python3
"""Twenty preselected paired rounds; no profiling, with fixed process settings."""
import json
import os
from pathlib import Path
import subprocess
import time

ROOT = Path(__file__).resolve().parents[4]
OUT = ROOT / 'docs/performance/setup-cleanup/continuation'
ENV = dict(os.environ, GOMAXPROCS='16', GOGC='100', GOMEMLIMIT='off', GODEBUG='', WAGO_BOUNDS='signals')
CASES = [
 ('wasi-focused', 'suite', ['-test.bench', '^Benchmark(CommandExec|Instantiate|Exec|ExecCallOverhead_wago)$/^(cjson|tinyxml2|utf8proc|pcre2|tiny)(\\.|$)', '-test.benchtime', '200ms', '-wago.corpus', 'cjson,tinyxml2,utf8proc,pcre2,tiny']),
 ('wasi-phases', 'suite', ['-test.bench', '^BenchmarkCommandLifecycleDiagnostic$', '-test.benchtime', '1000x', '-wago.corpus', 'cjson,tinyxml2', '-wago.bench.lifecycle']),
 ('wasi-host-owned', 'suite', ['-test.bench', '^BenchmarkWASI(HostCall|OwnedLifecycle)Diagnostic$', '-test.benchtime', '200ms', '-wago.corpus', 'cjson,tinyxml2', '-wago.bench.lifecycle']),
 *[(f'wasi-resources-{m}', 'suite', ['-test.run', f'^TestWASIResources$/^{m}$', '-test.v', '-wago.corpus', 'cjson,tinyxml2', '-wago.bench.lifecycle']) for m in ['minimal-wasi','cjson','tinyxml2']],
]

def environment():
 result = {}
 for p in ['/proc/loadavg','/proc/pressure/memory','/proc/pressure/cpu','/proc/meminfo']:
  result[p] = Path(p).read_text()
 result['temperature'] = {str(p): p.read_text() for p in Path('/sys/class/thermal').glob('thermal_zone*/temp')}
 return result


def run(label, kind, binary, args, sample):
 cmd = ['taskset', '-c', '0-15', str(ROOT / '.tmp/setup-cleanup-next' / f'wasi-{label}.test'), '-test.run', '^$', '-test.count', '1', '-test.benchmem', *args]
 before = environment()
 started = time.monotonic()
 rss_peak = virtual_peak = mappings_peak = 0
 with (OUT / f'{kind}-{label}.txt').open('a') as f:
  f.write(f'# round {sample}\n'); f.flush()
  p = subprocess.Popen(cmd, cwd=ROOT/'bench/suite', env=ENV, stdout=f, stderr=subprocess.STDOUT)
  while True:
   pid, status, usage = os.wait4(p.pid, os.WNOHANG)
   if pid:
    p.returncode = os.waitstatus_to_exitcode(status)
    break
   try:
    status_text = Path(f'/proc/{p.pid}/status').read_text()
    fields = {l.split(':')[0]:l.split(':')[1].strip() for l in status_text.splitlines() if ':' in l}
    rss_peak = max(rss_peak, int(fields.get('VmRSS', '0 kB').split()[0]))
    virtual_peak = max(virtual_peak, int(fields.get('VmSize', '0 kB').split()[0]))
    mappings_peak = max(mappings_peak, len(Path(f'/proc/{p.pid}/maps').read_text().splitlines()))
   except (OSError, ValueError):
    pass
   time.sleep(.02)
 record = dict(environment_before=before,environment_after=environment(),label=label,kind=kind,sample=sample,command=cmd,seconds=time.monotonic()-started,exit=p.returncode,peak_rss_kib=usage.ru_maxrss,sampled_peak_rss_kib=rss_peak,peak_virtual_kib=virtual_peak,peak_mapping_count=mappings_peak,minor_faults=usage.ru_minflt,major_faults=usage.ru_majflt,user_seconds=usage.ru_utime,system_seconds=usage.ru_stime)
 with (OUT/'wasi-resources.jsonl').open('a') as f: f.write(json.dumps(record)+'\n')
 if p.returncode: raise SystemExit(f'failed: {cmd}')
 print(f'round {sample} {label} {kind}: {record["seconds"]:.1f}s',flush=True)

if __name__ == '__main__':
 for sample in range(1,21):
  for label in (['baseline','candidate'] if sample%2 else ['candidate','baseline']):
   for kind,binary,args in CASES: run(label,kind,binary,args,sample)
