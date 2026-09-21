from importlib.util import spec_from_file_location,module_from_spec
from pathlib import Path
spec=spec_from_file_location('pairs',Path(__file__).with_name('run-wasi-pairs.py'));p=module_from_spec(spec);spec.loader.exec_module(p)
source=Path(__file__).with_name('run-wasi-pairs.py').read_text()
source=source[:source.index("if __name__ == '__main__':")].replace("f'wasi-{label}.test'","'memory-policy.test'").replace("OUT/'wasi-resources.jsonl'","OUT/'memory-policy-released-resources.jsonl'")
exec(compile(source,str(Path(__file__)), 'exec'),p.__dict__)
for sample in range(1,11):
 for pages in [6,16]:
  for data in [0,4096,65536,pages*65536]:
   engines=['wago-guarded','wazero-default','wazero-max-capacity']
   for engine in (engines if sample%2 else engines[::-1]):
    p.run(f'{pages}-{data}-{engine}','memory-policy-released','suite',['-test.run',f'^TestMemoryPolicyReleasedResources$/^pages={pages}$/^data={data}$/^{engine}$','-test.v','-wago.bench.lifecycle'],sample)
