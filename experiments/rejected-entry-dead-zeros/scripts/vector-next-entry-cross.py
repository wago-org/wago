from pathlib import Path
import json,os,subprocess
out=Path('/tmp/wago-vector-next-37174169/experiments/arm64-parity/entry-dead-zeros');out.mkdir(parents=True,exist_ok=True)
for mode in ['signals','explicit']:
 for kind in ['app','core']:
  p=Path(f'/tmp/entry-dead-zeros-qual-{mode}-{kind}.txt')
  assert '\nPASS\n' in p.read_text(),p
changed=[]; proof=[]
for kind in ['app','core']:
 base=Path('/tmp/hour-20261011-'+kind);candidate=Path('/tmp/entry-dead-zeros-qual-signals-'+kind)
 assert len(list(candidate.glob('*.bin')))==(102 if kind=='app' else 46),kind
 for f in sorted(candidate.glob('*.bin')):
  b=base/f.name;assert b.exists(),b
  a,x=b.read_bytes(),f.read_bytes()
  if a!=x:changed.append(dict(kind=kind,file=f.name,before=len(a),after=len(x)))
 proof.append(f'{kind}: compared every signal image to exact37174169 baseline, {len(list(candidate.glob("*.bin")))}images.')
(out/'native-changes.json').write_text(json.dumps(changed,indent=2)+'\n')
(out/'native-proof.txt').write_text('\n'.join(proof)+'\n')
rows=[]
for phase in ['exec','compile']:
 for pair in range(4):
  for candidate in [bool(pair%2),not bool(pair%2)]:
   for kind,names in [('app','audio-adpcm,language-register-vm,files-glob-match'),('core','modular-arithmetic,decimal-parse,complex-roundtrip,compile-match')]:
    env=dict(os.environ,WAGO_SHARED_SCALAR='0',WAGO_BOUNDS='signals',WAGO_ARM64_EXPERIMENT_ENTRY_DEAD_ZEROS=str(int(candidate)))
    binary='/tmp/paired-entry-dead-zeros-qualified' if candidate else '/tmp/paired-next-20261011-base'
    args=[binary,'-option','loop-memory-base','-workloads',names,'-phase',phase,'-rounds','1','-budget','100ms','-code-dir','/tmp/entry-dead-zeros-cross-'+str(int(candidate))+'-'+kind]
    if kind=='core':args+=['-corpus','/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache','-core-exec' if phase=='exec' else '-core-compile']
    raw=subprocess.run(args,env=env,text=True,capture_output=True,check=True).stdout
    (out/f'cross-{kind}-{phase}-{pair}-{int(candidate)}.txt').write_text(raw)
    for l in raw.splitlines():
     if not l.startswith('{'):continue
     row=json.loads(l)
     if not row['on']:continue
     row.update(on=candidate,round=pair);rows.append(row)
    (out/'cross-build.jsonl').write_text(''.join(json.dumps(x)+'\n' for x in rows))
coreids={'modular-arithmetic':'wago__libtommath__modular-arithmetic','decimal-parse':'wago__fastfloat__decimal-parse','complex-roundtrip':'wago__kissfft__complex-roundtrip','compile-match':'wago__pcre2__compile-match'}
for kind in ['app','core']:
 for f in Path('/tmp/entry-dead-zeros-cross-0-'+kind).glob('*-1.bin'):
  name=f.name[:-6];key=coreids[name] if kind=='core' else name
  assert f.read_bytes()==(Path('/tmp/hour-20261011-'+kind)/(key+'.bin')).read_bytes(),('baseline',name)
  candidate=Path('/tmp/entry-dead-zeros-cross-1-'+kind)/f.name
  assert candidate.read_bytes()==(Path('/tmp/entry-dead-zeros-qual-signals-'+kind)/(key+'.bin')).read_bytes(),('candidate',name)
  proof.append(name+': pristine-source and candidate images match exact inventories.')
(out/'cross-native-proof.txt').write_text('\n'.join(proof)+'\n')
