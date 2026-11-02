"""Check canonical baseline compilation before its execution; no RFW unsafe route."""
import run as q
import pathlib,json
b=json.loads((q.E/'baseline.json').read_text());q.M['pin']=b['pin']
assert b['catalog_equal']
raise SystemExit(q.test('baseline-canonical-checked-preflight',['./suite','-args','-wago.corpus=all'],'wago_regalloccheck',['-failfast','-run','^TestCorpus$/[^/]+$/(Decode|Validate|Compile|CompileFull)$'],pathlib.Path(b['source'])/'bench'))
