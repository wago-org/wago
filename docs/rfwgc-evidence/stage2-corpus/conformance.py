#!/usr/bin/env python3
"""Pinned staged-accounting metadata; never relabel historical expected counts as fresh passes."""
import pathlib,json,hashlib,collections,re,subprocess
ROOT=pathlib.Path(__file__).resolve().parents[3];OUT=pathlib.Path(__file__).resolve().parent
spec=pathlib.Path('/home/dev/Projects/wago/tests/conformance/spec-v3')
revision=subprocess.check_output(['git','rev-parse','HEAD'],cwd=spec,text=True).strip()
def sexp_counts(text):
 # Tokenize WAST comments/strings before classifying top-level commands.
 i=0;depth=0;comment=0;string=False;escape=False;heads=collections.Counter();modules=0;want_head=False
 while i<len(text):
  pair=text[i:i+2];c=text[i]
  if comment:
   if pair=='(;':comment+=1;i+=2;continue
   if pair==';)':comment-=1;i+=2;continue
   i+=1;continue
  if string:
   if escape:escape=False
   elif c=='\\':escape=True
   elif c=='"':string=False
   i+=1;continue
  if pair==';;':
   end=text.find('\n',i);i=len(text) if end<0 else end+1;continue
  if pair=='(;':comment=1;i+=2;continue
  if c=='"':string=True;i+=1;continue
  if c=='(':depth+=1;want_head=True;i+=1;continue
  if c==')':depth-=1;want_head=False;i+=1;continue
  if c.isspace():i+=1;continue
  j=i
  while j<len(text) and not text[j].isspace() and text[j] not in '()':j+=1
  atom=text[i:j]
  if want_head:
   if depth==1:heads[atom]+=1
   if atom=='module':modules+=1
   want_head=False
  i=max(j,i+1)
 return {'top_level_commands':dict(heads),'module_forms_including_negative_inputs':modules,'direct_module_commands':heads['module']}
rows=[]
for p in sorted((ROOT/'tests/conformance/baselines').glob('spec-v3-staged-*.json')):
 d=json.loads(p.read_text());files=d.get('files',[{'name':d.get('file'),'counts':d.get('counts',{})}])
 for f in files:
  name=f['name'];source=spec/'test/core'/(name+'.wast');counts=f.get('counts',{})
  rows.append({'snapshot':str(p.relative_to(ROOT)),'snapshot_sha256':hashlib.sha256(p.read_bytes()).hexdigest(),'file':name,'snapshot_counts':counts,'source_available':source.exists(),'source_syntax_counts':sexp_counts(source.read_text()) if source.exists() else None,'source_sha256':hashlib.sha256(source.read_bytes()).hexdigest() if source.exists() else None,'leader_pins':len(f.get('leaders',d.get('leaders',[])))})
# Preserve overlap: several snapshots intentionally cover the same source file.
byfile=collections.defaultdict(list)
for r in rows:byfile[r['file']].append(r)
failures=json.loads((ROOT/'docs/rfwgc-evidence/stage2-suite/summary.json').read_text())['ordinary']['failing_test_lines']
result={'mechanism':'Read-only inventory of checked-in staged accounting snapshots at source SHA; these are expected historical counts, not newly executed module coverage. Current tooling gap carried from stage2-suite exact logs.','source_revision':revision,'snapshots':len(set(r['snapshot'] for r in rows)),'source_case_rows':len(rows),'unique_source_cases':len(byfile),'overlapping_source_cases':{k:len(v) for k,v in byfile.items() if len(v)>1},'rows':rows,'prior_failed_top_level_tests':len([x for x in failures if '/' not in x]),'prior_failed_lines_including_subtests':len(failures),'current_corpus_conformance_modules_executed':0,'toolchain_gap':'Official Release 3 interpreter requires unavailable dune/ocaml/ocamlc/menhir; no installation or expectation rewriting.','source_module_denominator':'Unknown across scripts: counts.modules_passed excludes invalid/malformed/link/trap/feature cases. Leader pins count only pinned leaders; overlapping snapshots are not additive module coverage.'}
(OUT/'conformance-coverage.json').write_text(json.dumps(result,indent=2)+'\n')
print({k:v for k,v in result.items() if k not in ['rows']})
