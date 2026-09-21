from importlib.util import spec_from_file_location,module_from_spec
from pathlib import Path
import json,re,subprocess,os,time
spec=spec_from_file_location('pairs',Path(__file__).with_name('run-wasi-pairs.py'));p=module_from_spec(spec);spec.loader.exec_module(p)
source=Path(__file__).with_name('run-wasi-pairs.py').read_text()
source=source[:source.index("if __name__ == '__main__':")].replace("OUT/'wasi-resources.jsonl'","OUT/'full-followup-resources.jsonl'")
exec(compile(source,str(Path(__file__)), 'exec'),p.__dict__)
flags=json.loads((p.OUT/'full-summary.json').read_text())['timing_outliers_10_percent']
names=[r['name'] for r in flags]+['BenchmarkCommandExec/cjson','BenchmarkCommandExec/tinyxml2','BenchmarkInstantiate/tiny','BenchmarkExec/utf8proc.utf8proc_run']
(p.OUT/'full-followup-selection.json').write_text(json.dumps({'samples':20,'benchtime':'200ms','names':names,'cold_control':'TestMinimalWASICommand in a fresh process, including process/package startup and compile'},indent=2)+'\n')
for sample in range(1,21):
 for label in (['baseline','candidate'] if sample%2 else ['candidate','baseline']):
  for name in names:
   pattern='^'+'$/^'.join(re.escape(part) for part in name.split('/'))+'$'
   p.run(label,'full-followup','suite',['-test.bench',pattern,'-test.benchtime','200ms','-wago.corpus','all'],sample)
  cmd=['taskset','-c','0-15',str(p.ROOT/'.tmp/setup-cleanup-next'/f'wasi-{label}.test'),'-test.run','^TestMinimalWASICommand$','-test.count','1']
  with (p.OUT/f'cold-command-{label}.log').open('a') as f:
   start=time.perf_counter_ns();proc=subprocess.Popen(cmd,cwd=p.ROOT/'bench/suite',env=p.ENV,stdout=f,stderr=subprocess.STDOUT)
   _,status,usage=os.wait4(proc.pid,0);elapsed=time.perf_counter_ns()-start;proc.returncode=os.waitstatus_to_exitcode(status)
  if proc.returncode:raise SystemExit('cold command failed')
  with (p.OUT/f'cold-command-{label}.txt').open('a') as f:f.write(f'BenchmarkColdMinimalCommandProcess 1 {elapsed} ns/op {usage.ru_utime+usage.ru_stime:.9f} CPU-s {usage.ru_maxrss} RSS-KiB {usage.ru_minflt} minor-faults {usage.ru_majflt} major-faults\n')
