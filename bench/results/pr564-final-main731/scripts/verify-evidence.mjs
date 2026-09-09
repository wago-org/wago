// Run after all timed work. Refuse incomplete or mixed-binary reports.
import fs from 'node:fs';
import path from 'node:path';
import {createHash} from 'node:crypto';
import {parse} from './stats.mjs';
const root='/home/jtenner/Projects/wago/.tmp/pr564-memory/final-closures';
const json=p=>JSON.parse(fs.readFileSync(`${root}/${p}`));
const lines=p=>fs.readFileSync(`${root}/${p}`,'utf8').trim().split('\n').filter(Boolean).map(JSON.parse);
const requireFact=(ok,why)=>{if(!ok)throw Error(why);};
const metadata=json('metadata.json');
requireFact(metadata.head==='b4f2360f517641803249c7ed998f37d8b0ec82a2','wrong source head');
requireFact(metadata.main==='731e95ff2cda7309eaf6d956f1417066bf7f1b69','wrong main');
const digests=Object.fromEntries(Object.entries(metadata.binaries).map(([k,r])=>[k,r.sha256]));
for(const[key,expected]of Object.entries(digests))requireFact(createHash('sha256').update(fs.readFileSync(`${root}/${key}.test`)).digest('hex')===expected,`changed binary ${key}`);
const recorded=json('full/binary-identity.json');
requireFact(Object.keys(recorded).length===4&&Object.entries(recorded).every(([k,v])=>digests[k]===v),'full binary identities');
const fullStatus=lines('full/status.jsonl');
requireFact(fullStatus.length===1020&&new Set(fullStatus.map(r=>r.key)).size===1020&&fullStatus.every(r=>r.code===0),'full process coverage');
const full=json('full/comparison.json');
const metricKeys=new Set(full.map(r=>`${r.mode}/${r.name}/${r.unit}`));
requireFact(metricKeys.size===full.length,'duplicate full metrics');
requireFact(full.length===16144&&full.filter(r=>r.unit==='ns/op').length===3910,'full metric coverage');
requireFact(full.every(r=>!r.status&&r.baseSamples===6&&r.candidateSamples===6),'unpaired full metrics');
for(const mode of ['explicit','signals']){
 const a=parse(`${root}/full/base-${mode}.bench`),b=parse(`${root}/full/final-${mode}.bench`);
 requireFact(a.size===b.size,`benchmark name count ${mode}`);
 for(const[name,units]of a){
  requireFact(b.has(name),`missing ${mode}/${name}`);
  const expected=Object.keys(units).sort(),actual=Object.keys(b.get(name)).sort();
  requireFact(JSON.stringify(expected)===JSON.stringify(actual),`metric unit coverage ${mode}/${name}`);
  for(const unit of expected)requireFact(metricKeys.has(`${mode}/${name}/${unit}`),`metric row coverage ${mode}/${name}/${unit}`);
 }
 for(const suffix of ['confirm','fixed-memory','fixed-instantiate','fixed-rss']){
  const run=`${suffix}-${mode}`,identity=json(`${run}/identity.json`),rows=json(`${run}/comparison.json`),status=lines(`${run}/status.jsonl`),samples=suffix==='confirm'?12:6;
  requireFact(identity.samples===samples&&identity.labels.join(',')==='base,final',`${run} method`);
  for(const[file,digest]of Object.entries(identity.binaries))requireFact(digests[path.basename(file,'.test')]===digest,`${run} binary`);
  requireFact(status.length===identity.names.length*samples*2&&new Set(status.map(r=>r.key)).size===status.length&&status.every(r=>r.code===0),`${run} process coverage`);
  requireFact(rows.length>0&&rows.every(r=>!r.status&&r.baseSamples===samples&&r.candidateSamples===samples),`${run} paired samples`);
 }
}
const selected=json('repeat-selection.json'),repeated=['explicit','signals'].flatMap(mode=>json(`confirm-${mode}/comparison.json`));
for(const r of selected)requireFact(repeated.some(x=>x.mode===r.mode&&x.name===r.name&&x.unit==='ns/op'),`missing selected repeat ${r.mode}/${r.name}`);
const ci=json('ci-b4f236-green.json');
requireFact(ci.headRefOid===metadata.head&&ci.statusCheckRollup.length>0&&ci.statusCheckRollup.every(r=>r.status==='COMPLETED'&&['SUCCESS','SKIPPED','NEUTRAL'].includes(r.conclusion)),'final-code CI');
requireFact(json('qualification-progress.json').stage==='complete','qualification incomplete');
const sizes=json('size.json').rows;
requireFact(sizes.length===8&&sizes.every(r=>Number.isFinite(r.bytes)&&Number.isFinite(r.budget)&&r.bytes<=r.budget),'release size budgets');
const audit=json('audit-compact-summary.json');
requireFact(audit.length===112&&audit.every(r=>r.equal),'AMD64 code audit');
for(const kind of ['minimal','standard']){
 const smoke=json(`${kind}-tiny-smoke.json`);
 requireFact(smoke.head===metadata.head&&smoke.rows.length===80&&smoke.rows.every(r=>r.passed),`${kind} TinyGo startup`);
 requireFact(createHash('sha256').update(fs.readFileSync(smoke.binary)).digest('hex')===smoke.sha256,`${kind} TinyGo identity`);
}
console.log(JSON.stringify({head:metadata.head,main:metadata.main,fullProcesses:fullStatus.length,fullMetrics:full.length,selectedRepeats:selected.length,result:'PASS'},null,2));
