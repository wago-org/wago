import run as q
import pathlib,json,hashlib
# New adapter only; existing archived test source remains byte-identical.
original=q.S/'src/wago/design_repair_unsupported_test.go'
adapter=q.S/'src/wago/final_combined_tinygo_rejection_test.go'
data='//go:build tinygo || wago_target_tinygo\n'+original.read_text().split('\n',1)[1]
adapter.write_text(data)
(q.E/'tinygo-rejection-overlay.json').write_text(json.dumps(dict(original=str(original),original_sha256=hashlib.sha256(original.read_bytes()).hexdigest(),adapter=str(adapter),adapter_sha256=hashlib.sha256(adapter.read_bytes()).hexdigest(),change='Only build constraint; bodies preserved'),indent=2))
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 tags=[] if mode=='ordinary' else ['-tags='+mode]
 if q.run('tinygo-repair-'+mode,['tinygo','test','-scheduler=threads','-gc=conservative',*tags,'-v','-run','^Test(DesignRepair|GCExternConversion)','./src/wago']):raise SystemExit(1)
