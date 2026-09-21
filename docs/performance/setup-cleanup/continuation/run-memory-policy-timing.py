from importlib.util import spec_from_file_location,module_from_spec
from pathlib import Path
spec=spec_from_file_location('pairs',Path(__file__).with_name('run-wasi-pairs.py'));p=module_from_spec(spec);spec.loader.exec_module(p)
source=Path(__file__).with_name('run-wasi-pairs.py').read_text()
source=source[:source.index("if __name__ == '__main__':")].replace("f'wasi-{label}.test'","'memory-policy.test'").replace("OUT/'wasi-resources.jsonl'","OUT/'memory-policy-resources.jsonl'")
exec(compile(source,str(Path(__file__)), 'exec'),p.__dict__)
for sample in range(1,11):
 for engine in (['wago-guarded','wazero-default','wazero-max-capacity'] if sample%2 else ['wazero-max-capacity','wazero-default','wago-guarded']):
  p.run(engine,'memory-policy-timing','suite',['-test.bench',f'^BenchmarkMemoryPolicyLifecycleDiagnostic$/././^{engine}$','-test.benchtime','200ms','-wago.bench.lifecycle'],sample)
