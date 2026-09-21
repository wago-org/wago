from importlib.util import spec_from_file_location,module_from_spec
from pathlib import Path
import hashlib,json,subprocess
spec=spec_from_file_location('pairs',Path(__file__).with_name('run-wasi-pairs.py'));p=module_from_spec(spec);spec.loader.exec_module(p)
source=Path(__file__).with_name('run-wasi-pairs.py').read_text()
source=source[:source.index("if __name__ == '__main__':")].replace("OUT/'wasi-resources.jsonl'","OUT/'full-resources.jsonl'")
exec(compile(source,str(Path(__file__)), 'exec'),p.__dict__)
files=list((p.ROOT/'bench/suite').glob('*.go'))+[p.ROOT/'src/wago/imports.go',p.ROOT/'.tmp/setup-cleanup-next/wasi/internal/core/core.go',p.ROOT/'.tmp/setup-cleanup-next/wasi-base/internal/core/core.go',p.ROOT/'.tmp/setup-cleanup-next/wasi-baseline.test',p.ROOT/'.tmp/setup-cleanup-next/wasi-candidate.test']
(p.OUT/'full-source-hashes.json').write_text(json.dumps({str(f.relative_to(p.ROOT)):hashlib.sha256(f.read_bytes()).hexdigest() for f in files},indent=2)+'\n')
for label in ['baseline','candidate']:
 p.run(label,'full','suite',['-test.bench','.','-test.benchtime','1s','-test.timeout','0','-wago.corpus','all'],1)
