from importlib.util import spec_from_file_location,module_from_spec
from pathlib import Path
import hashlib
spec=spec_from_file_location('pairs',Path(__file__).with_name('run-wasi-pairs.py'));p=module_from_spec(spec);spec.loader.exec_module(p)
p.run.__globals__['ROOT']=p.ROOT
# The common runner selects the same frozen diagnostic executable for both modes.
source=Path(__file__).with_name('run-wasi-pairs.py').read_text()
source=source[:source.index("if __name__ == '__main__':")].replace("f'wasi-{label}.test'","'worker-owned.test'").replace("OUT/'wasi-resources.jsonl'","OUT/'workers-owned-resources.jsonl'")
exec(compile(source,str(Path(__file__)), 'exec'),p.__dict__)
for sample in range(1,11):
 for api in (['raw','policy','public'] if sample%2 else ['public','policy','raw']):
  stage='full' if api=='public' else '(validate|backend)'
  pattern=f'^BenchmarkWorkerLifecycleDiagnostic$/^(tiny|many_funcs|json-as|lua)$/^callers=(1|4|16)$/^{stage}$/^{api}$/^requested=(0|1|2|4)$'
  p.run(api,'workers-owned','suite',['-test.bench',pattern,'-test.benchtime','100ms','-wago.bench.lifecycle','-wago.corpus','tiny,many_funcs,json-as,lua'],sample)
 for module in ['tiny','many_funcs','json-as','lua']:
  for callers in [1,4,16]:
   for workers in ([1,2,4,0] if sample%2 else [0,4,2,1]):
    p.run(f'{module}-{callers}-{workers}','worker-memory','suite',['-test.run',f'^TestWorkerResources$/^{module}$/^callers={callers}$/^requested={workers}$','-test.v','-wago.bench.lifecycle','-wago.corpus',module],sample)
