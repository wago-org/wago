import fs from 'node:fs';
const out='/tmp/wago-pr564-HbN430';
const all=JSON.parse(fs.readFileSync(`${out}/comparison.json`));
const confirmed=fs.existsSync(`${out}/confirmation-summary.json`)?JSON.parse(fs.readFileSync(`${out}/confirmation-summary.json`)):[];
const key=r=>`${r.mode}/${r.name}/${r.unit}`;
const followups=new Map(confirmed.map(r=>[key(r),r]));
const batched=new Set(all.filter(r=>r.unit==='calls/batch').map(r=>`${r.mode}/${r.name}`));
const rows=[];
for(const initial of all) {
  if(initial.status||/Wazero|_wazero/.test(initial.name))continue;
  if(!['ns/op','B/op','allocs/op','B/call','allocs/call','code-B'].includes(initial.unit))continue;
  if(initial.name.startsWith('BenchmarkPluginExec/')&&initial.unit!=='ns/op')continue;
  if(['B/op','allocs/op'].includes(initial.unit)&&batched.has(`${initial.mode}/${initial.name}`))continue;
  const followup=followups.get(key(initial));
  const result=followup??initial;
  // Include all observed increases, not only a percent or significance cutoff.
  if(!(result.candidate>result.base)&&!(initial.candidate>initial.base))continue;
  let status;
  if(result.unit==='ns/op') {
    status=followup?(result.delta>0&&result.p<0.05?'repeat-significant-slowdown':'not-confirmed-on-repeat'):'first-pass-only';
  } else status=result.candidate<=result.base?'resource-increase-not-repeated':result.unit==='code-B'?'code-size-increase':'resource-increase-observed';
  rows.push({...result,status,initialDelta:initial.delta,initialP:initial.p,confirmation:!!followup});
}
rows.sort((a,b)=>a.mode.localeCompare(b.mode)||a.unit.localeCompare(b.unit)||(b.delta??Infinity)-(a.delta??Infinity)||a.name.localeCompare(b.name));
fs.writeFileSync(`${out}/regressions.json`,JSON.stringify(rows,null,2)+'\n');
const columns=['status','mode','name','unit','base','candidate','delta','p','baseSamples','candidateSamples','initialDelta','initialP','confirmation'];
fs.writeFileSync(`${out}/regressions.tsv`,columns.join('\t')+'\n'+rows.map(r=>columns.map(k=>r[k]??'').join('\t')).join('\n')+'\n');
console.log(JSON.stringify({observedRows:rows.length,confirmedTiming:rows.filter(r=>r.status==='repeat-significant-slowdown'),codeGrowth:rows.filter(r=>r.status==='code-size-increase')},null,2));
