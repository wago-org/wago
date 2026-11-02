import pathlib,json,gzip,re,hashlib,collections
E=pathlib.Path(__file__).resolve().parent;M=json.loads((E/'source.json').read_text());spec=[]
for p in sorted(E.glob('spec-v*/raw.log.gz')):
 lines=[];total=[]
 for l in gzip.open(p,'rt'):
  try:x=json.loads(l)
  except ValueError:continue
  text=x.get('Output','')
  if 'modules(pass=' in text:lines.append(text.strip())
  if 'TOTAL[' in text:total.append(text.strip())
 spec.append(dict(suite=p.parent.name,per_file=lines,total=total))
(E/'spec-accounting.json').write_text(json.dumps(spec,indent=2)+'\n')
cat=json.loads((E/'catalog.json').read_text()) if (E/'catalog.json').exists() else json.loads((pathlib.Path(M['source'])/'corpus/catalog.json').read_text())
rows=[]
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 p=E/('catalog-'+mode)/'result.json';r=json.loads(p.read_text()) if p.exists() else {};ts={e.get('Test'):e['Action'] for e in r.get('events',[]) if 'Test' in e}
 for e in cat['benchmarks']:rows.append(dict(id=e['id'],mode=mode,source=M['pin'],status=ts.get('TestRFWStage3CorpusCompile/'+e['id'],'NOT_RUN')))
(E/'catalog-per-id.json').write_text(json.dumps(rows,indent=2)+'\n')
fixtures=[dict(file=str(p.relative_to(E)),sha256=hashlib.sha256(p.read_bytes()).hexdigest(),kind={'graph':'actual WasmGC chain','scalar':'scalar control','noGC':'no-GC control'}[p.stem]) for p in sorted(E.glob('fixtures-*/*.wasm'))]
(E/'generated-graph-identities.json').write_text(json.dumps(fixtures,indent=2)+'\n')
refs=[]
p=E/'corpus-per-id.json'
if p.exists():
 for r in json.loads(p.read_text()):
  ref=r.get('reference')
  if ref and ref['status']=='skip':refs.append(dict(id=r['id'],mode=r['mode'],revision=r['revision'],reference=ref))
(E/'reference-only-skips.json').write_text(json.dumps(refs,indent=2)+'\n')
print('catalog',dict(collections.Counter(r['status'] for r in rows)),'spec commands',len(spec),'graph binaries',len(fixtures),'reference-only skip rows',len(refs))
