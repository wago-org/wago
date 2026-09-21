#!/usr/bin/env python3
"""One new compatible full baseline, then one full candidate. No profiles."""
from importlib.util import spec_from_file_location, module_from_spec
from pathlib import Path
import hashlib
import subprocess
spec = spec_from_file_location('focused', Path(__file__).with_name('run-focused.py'))
focused = module_from_spec(spec)
spec.loader.exec_module(focused)
files = [focused.ROOT / 'src/wago/imports.go', *sorted((focused.ROOT/'bench/suite').glob('*lifecycle*test.go')), *sorted((focused.ROOT/'bench/suite').glob('*diagnostic*test.go')), focused.ROOT/'.tmp/setup-cleanup/baseline-diagnostic.test', focused.ROOT/'.tmp/setup-cleanup/candidate-diagnostic.test']
with (focused.OUT/'hashes-final.txt').open('w') as out:
 for p in dict.fromkeys(files): out.write(hashlib.sha256(p.read_bytes()).hexdigest()+'  '+str(p.relative_to(focused.ROOT))+'\n')
revision = subprocess.check_output(['git','rev-parse','HEAD'],cwd=focused.ROOT,text=True).strip()
for label in ['baseline','candidate']:
 with (focused.OUT/f'full-{label}.txt').open('a') as out:
  out.write(f'# git {revision}; {label}; source hashes in hashes-final.txt\n')
 focused.run(label,'full','diagnostic',['-test.bench','.','-test.benchtime','1s','-test.timeout','0','-wago.corpus','all'],1)
