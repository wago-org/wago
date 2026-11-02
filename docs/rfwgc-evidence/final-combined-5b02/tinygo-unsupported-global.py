"""Actual TinyGo execution of unchanged unsupported-global follow-up tests."""
import run as q
import json,gzip,re,hashlib
source=q.S/'src/wago/gc_conversion_unsupported_test.go';before=hashlib.sha256(source.read_bytes()).hexdigest();rows=[]
required=['TestUnsupportedConversionPublicGlobalRejectsOpaqueBeforeDirectory','TestUnsupportedConversionGlobalValidationOrdering']
subtests=['ordinary','null','invalid','cross-store','immutable-before-invalid','immutable-before-null','closed-before-invalid','closed-before-null']
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 tags=[] if mode=='ordinary' else ['-tags='+mode]
 name='tinygo-unsupported-global-'+mode
 q.run(name,['tinygo','test','-v','-scheduler=threads','-gc=conservative',*tags,'-run','^TestUnsupportedConversion','./src/wago'])
 text=gzip.open(q.E/name/'raw.log.gz','rt').read();passed=re.findall(r'^\s*--- PASS: ([^ ]+)',text,re.M)
 want=required+['TestUnsupportedConversionGlobalValidationOrdering/'+s for s in subtests]
 assert set(want)<=set(passed),(mode,sorted(set(want)-set(passed)))
 assert '--- SKIP:' not in text and 'test depends on standard Go testing' not in text
 assert hashlib.sha256(source.read_bytes()).hexdigest()==before
 rows.append(dict(mode=mode,required=want,passed=passed,source_file=str(source),source_sha256=before,actual_tinygo=True,tag_adapter=False))
 (q.E/'tinygo-unsupported-global-census.json').write_text(json.dumps(rows,indent=2)+'\n')
