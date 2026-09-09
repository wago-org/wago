// Report only. Never launch benchmarks or alter source code.
import fs from 'node:fs';
import {median,rankP} from './stats.mjs';
const root='/home/jtenner/Projects/wago/.tmp/pr564-memory/final-closures';
const out=`${root}/rendered`;
const read=p=>JSON.parse(fs.readFileSync(`${root}/${p}`));
const full=read('full/comparison.json');
const engine=r=>/Wazero|_wazero/.test(r.name)?'wazero':'wago';
const csv=(rows,keys)=>keys.join(',')+'\n'+rows.map(r=>keys.map(k=>{
 const s=r[k]==null?'':String(r[k]);return /[,"\n]/.test(s)?'"'+s.replaceAll('"','""')+'"':s;
}).join(',')).join('\n')+'\n';
const all=[];
function add(rows,stage,benchtime) {
 const batches=new Set(rows.filter(r=>r.unit==='calls/batch').map(r=>`${r.mode}/${r.name}`));
 for(const r of rows) {
  if(!(r.candidate>r.base)||r.unit==='calls/batch')continue;
  if(benchtime==='1x'&&r.unit==='ns/op')continue;
  if(r.name.startsWith('BenchmarkPluginExec/')&&['B/op','allocs/op'].includes(r.unit))continue;
  const rawBatch=batches.has(`${r.mode}/${r.name}`)&&['B/op','allocs/op'].includes(r.unit);
  const kind=r.unit==='ns/op'?'time':rawBatch?'raw batch counter':r.unit==='code-B'?'code size':'memory';
  all.push({stage,bounds:r.mode,benchmark:r.name,engine:engine(r),kind,unit:r.unit,
   main_median:r.base,branch_median:r.candidate,absolute_increase:r.candidate-r.base,
   increase_percent:r.delta,p:r.p,unadjusted_p_below_005:r.p!==null&&r.p<.05,
   samples_each:r.baseSamples,benchtime,
   note:rawBatch?'Not a per-call regression; batch size can differ. See B/call and allocs/call.':
    r.unit==='B/call'||r.unit==='allocs/call'?'Derived per sample before taking medians.':
    r.name.startsWith('BenchmarkExecParallel/')?'Throughput time, not one-call latency.':
    benchtime==='1x'?'Equal-work memory check; timing omitted.':'Measured increase; p-values are not adjusted for multiple comparisons.'});
 }
}
add(full,'full screen','100ms');
for(const mode of ['explicit','signals']) {
 add(read(`confirm-${mode}/comparison.json`),'longer repeat','500ms');
 for(const suffix of ['memory','instantiate','rss']) {
  const run=`fixed-${suffix}-${mode}`,identity=read(`${run}/identity.json`);
  add(read(`${run}/comparison.json`),`fixed ${suffix}`,identity.benchtime);
  const status=fs.readFileSync(`${root}/${run}/status.jsonl`,'utf8').trim().split('\n').map(JSON.parse);
  for(const [bounds,name] of identity.names) {
   if(bounds!==mode||typeof name!=='string')throw Error(`Unexpected fixed-work identity ${run}`);
   const values=label=>status.filter(r=>r.label===label&&r.name===name).map(r=>+fs.readFileSync(`${root}/${run}/${r.key}.txt.resource`,'utf8').match(/Maximum resident set size \(kbytes\):\s*(\d+)/)[1]);
   const a=values('base'),b=values('final');
   if(a.length!==6||b.length!==6||[...a,...b].some(x=>!Number.isFinite(x)))throw Error(`Missing RSS samples ${run}/${name}`);
   const base=median(a),candidate=median(b);
   if(candidate>base)all.push({stage:`fixed ${suffix}`,bounds:mode,benchmark:name,engine:'wago',kind:'process peak RSS',unit:'KiB',main_median:base,branch_median:candidate,absolute_increase:candidate-base,increase_percent:100*(candidate/base-1),p:rankP(a,b),unadjusted_p_below_005:rankP(a,b)<.05,samples_each:a.length,benchtime:identity.benchtime,note:'Equal-work process peak includes setup, Go heap, native mappings, and corpus; not retained compiler memory.'});
  }
 }
}
const stages=['full screen','longer repeat','fixed memory','fixed instantiate','fixed rss'];
const kinds=['time','memory','code size','process peak RSS','raw batch counter'];
all.sort((a,b)=>stages.indexOf(a.stage)-stages.indexOf(b.stage)||kinds.indexOf(a.kind)-kinds.indexOf(b.kind)||a.unit.localeCompare(b.unit)||b.absolute_increase-a.absolute_increase||a.benchmark.localeCompare(b.benchmark));
const keys=Object.keys(all[0]);
const write=(name,rows)=>fs.writeFileSync(`${out}/${name}.csv`,csv(rows,keys));
write('all-increased-metrics-main-vs-branch',all);
const costs=all.filter(r=>r.engine==='wago'&&r.kind!=='raw batch counter');
write('all-wago-losses-main-vs-branch',costs);
write('wazero-control-increases-main-vs-branch',all.filter(r=>r.engine==='wazero'));
write('raw-batch-counter-increases-not-per-call-losses',all.filter(r=>r.kind==='raw batch counter'));
write('all-slower-wago-benchmarks-main-vs-branch',costs.filter(r=>r.kind==='time'));
const fmt=n=>n==null?'new nonzero':String(Number(n.toPrecision(12)));
let md='# All measured Wago losses against main\n\nBefore: measured main `731e95ff2cda7309eaf6d956f1417066bf7f1b69`. After: this branch `b4f2360f517641803249c7ed998f37d8b0ec82a2`.\n\nEvery positive time, per-operation/per-call memory, code-size, or equal-work peak RSS difference is included, even when p >= 0.05. Full-screen and later samples stay separate: a better repeat does not erase a screen loss. Bounds are separate benchmark configurations. P-values are unadjusted and do not prove cause; **yes** flags p < 0.05. Parallel execution measures throughput, not one-call latency. Fixed 1x timing is not a timing claim and is omitted. Wazero controls and raw per-batch counter increases have separate CSV files. Increased calls/batch is a calibration change, not a loss.\n\n';
const summary=[];
for(const stage of [...new Set(costs.map(r=>r.stage))])for(const kind of [...new Set(costs.filter(r=>r.stage===stage).map(r=>r.kind))]) {
 const rows=costs.filter(r=>r.stage===stage&&r.kind===kind);
 summary.push({stage,kind,increased_rows:rows.length,p_below_005:rows.filter(r=>r.unadjusted_p_below_005).length});
 md+=`## ${stage}: ${kind}\n\n${rows.length} increased rows; ${rows.filter(r=>r.unadjusted_p_below_005).length} with unadjusted p < 0.05. Rows sorted by unit, then absolute increase.\n\n| Bounds | Benchmark | Unit | Main | Branch | Increase | Increase % | p | p < .05 | Samples each |\n|---|---|---|---:|---:|---:|---:|---:|---|---:|\n`;
 md+=rows.map(r=>`| ${r.bounds} | ${r.benchmark} | ${r.unit} | ${fmt(r.main_median)} | ${fmt(r.branch_median)} | ${fmt(r.absolute_increase)} | ${fmt(r.increase_percent)} | ${fmt(r.p)} | ${r.unadjusted_p_below_005?'yes':'no'} | ${r.samples_each} |`).join('\n')+'\n\n';
}
fs.writeFileSync(`${out}/all-wago-losses-main-vs-branch.md`,md);
fs.writeFileSync(`${out}/loss-summary.json`,JSON.stringify({main:read('metadata.json').main,branch:read('metadata.json').head,summary,wagoCostRows:costs.length,allIncreasedRows:all.length},null,2)+'\n');
// Independent coverage assertions: all positive measured time rows survive.
const timeInputs=[['full screen',full],...['explicit','signals'].map(m=>['longer repeat',read(`confirm-${m}/comparison.json`)])];
for(const[stage,rows]of timeInputs) {
 const expected=rows.filter(r=>engine(r)==='wago'&&r.unit==='ns/op'&&r.candidate>r.base);
 const actual=costs.filter(r=>r.stage===stage&&r.kind==='time');
 for(const r of expected)if(!actual.some(x=>x.bounds===r.mode&&x.benchmark===r.name&&x.main_median===r.base&&x.branch_median===r.candidate))throw Error(`Missing time loss ${stage}/${r.mode}/${r.name}`);
}
console.log(JSON.stringify(summary,null,2));
