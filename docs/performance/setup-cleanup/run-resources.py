#!/usr/bin/env python3
from importlib.util import spec_from_file_location, module_from_spec
from pathlib import Path
spec = spec_from_file_location('focused', Path(__file__).with_name('run-focused.py'))
focused = module_from_spec(spec)
spec.loader.exec_module(focused)
for sample in range(1,11):
 for label in (['baseline','candidate'] if sample%2 else ['candidate','baseline']):
  focused.run(label,'resources','diagnostic',['-test.run','^Test(CommandLifecycleResources|LifecycleResources)$','-test.v','-wago.corpus','tiny,utf8proc,pcre2,xxhash,cjson,tinyxml2','-wago.bench.lifecycle'],sample)
