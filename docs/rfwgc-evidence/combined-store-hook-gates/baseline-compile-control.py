#!/usr/bin/env python3
"""Compile-only control after a checked compiler failure; no guest invocation."""
import run as q
import pathlib,tarfile,json,hashlib
q.env.pop('GOWORK',None)
s=pathlib.Path(q.M['scratch'])/'compile-baseline';s.mkdir()
with tarfile.open(pathlib.Path(q.M['scratch'])/'baseline.tar') as t:t.extractall(s,filter='data')
rel='src/wago/rfwgc_stage3_measurement_bench_test.go';data=(q.S/rel).read_bytes();(s/rel).write_bytes(data)
pin='a3c08365f531eb3c9003e3acf3e0a822c12c31f4'
(q.E/'baseline-compile-adapter.json').write_text(json.dumps({'pin':pin,'source':str(s),'test_overlay':rel,'overlay_source':q.M['pin'],'overlay_sha256':hashlib.sha256(data).hexdigest(),'no_guest_execution':True},indent=2)+'\n')
q.M['pin']=pin
raise SystemExit(q.test('baseline-checked-clang-cpp',['./src/wago'],'wago_regalloccheck',['-failfast','-run','^TestRFWStage3CorpusCompile$/^clang-cpp-kernels$'],s,{'RFW_CATALOG':str(s/'corpus/catalog.json')}))
