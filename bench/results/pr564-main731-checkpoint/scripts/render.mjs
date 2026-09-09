import fs from 'node:fs';
const root='/home/jtenner/Projects/wago/.tmp/pr564-memory/final731',out=`${root}/rendered`;
fs.mkdirSync(out,{recursive:true});
const rows=JSON.parse(fs.readFileSync(`${root}/full/comparison.json`));
const listed=JSON.parse(fs.readFileSync(`${root}/listed-cases.json`));
const fullOnly=process.argv.includes('--full-only');
const confirmedOnly=process.argv.includes('--confirmed-only');
const focus=fullOnly?[]:['signals','explicit'].flatMap(mode=>JSON.parse(fs.readFileSync(`${root}/confirm-${mode}/comparison.json`)));
const csv=(names,records)=>names.join(',')+'\n'+records.map(r=>names.map(n=>{
 const s=r[n]===null||r[n]===undefined?'':String(r[n]);return /[,"\n]/.test(s)?'"'+s.replaceAll('"','""')+'"':s;
}).join(',')).join('\n')+'\n';
const number=n=>n===null||n===undefined?'—':String(Number(n.toPrecision(12)));
const pct=n=>n===null||n===undefined?'new nonzero':`${n>0?'+':''}${n.toFixed(2)}%`;
const pvalue=n=>n===null||n===undefined?'—':n<.001?'<0.001':n.toFixed(3);
const engine=r=>/Wazero|_wazero/.test(r.name)?'wazero':'wago';
const batched=new Set(rows.filter(r=>r.unit==='calls/batch').map(r=>`${r.mode}/${r.name}`));
const note=r=>r.name.startsWith('BenchmarkPluginExec/')&&['B/op','allocs/op'].includes(r.unit)?'unmeasured: allocation timer stopped':r.unit==='B/call'||r.unit==='allocs/call'?'derived per sample from calls/batch':batched.has(`${r.mode}/${r.name}`)&&['B/op','allocs/op'].includes(r.unit)?'raw per batch; use per-call metric':'reported';
const records=rows.map(r=>({bounds:r.mode,benchmark:r.name,engine:engine(r),unit:r.unit,main_median:r.base,after_median:r.candidate,change_percent:r.delta,p:r.p,main_samples:r.baseSamples,after_samples:r.candidateSamples,measurement:note(r),status:r.status??'paired'}));
const headers=Object.keys(records[0]);
fs.writeFileSync(`${out}/all-metrics-main-vs-after.csv`,csv(headers,records));
fs.writeFileSync(`${out}/all-timings-main-vs-after.csv`,csv(headers,records.filter(r=>r.unit==='ns/op')));
const table=(title,data,description)=>`# ${title}\n\n${description}\n\n| Bounds | Benchmark | Main ns/op | After ns/op | Change | p | Samples each |\n|---|---|---:|---:|---:|---:|---:|\n`+data.map(r=>`| ${r.mode} | ${r.name} | ${number(r.base)} | ${number(r.candidate)} | ${pct(r.delta)} | ${pvalue(r.p)} | ${r.baseSamples} |`).join('\n')+'\n';
const method='Before is freshly measured main `731e95ff2cda7309eaf6d956f1417066bf7f1b69`. After is `1d04b458fa512265e2b93711f5c355435a29f33e`. Negative changes are faster. All values are medians; p values are unadjusted two-sided rank tests, not proof of equivalence.';
for(const mode of ['explicit','signals']) {
 const data=rows.filter(r=>r.mode===mode&&r.unit==='ns/op').sort((a,b)=>a.name.localeCompare(b.name));
 let text=`# Full ${mode} timings: main versus after\n\n${method}\n\nSix alternating fresh-process pairs; 100 ms requested time. Includes Wazero control rows. ExecParallel is throughput time, not call latency. Each reported PluginExec sample times one fixed workload; Go may invoke the benchmark again during calibration, but benchtime does not lengthen that workload.\n`;
 for(const stage of new Set(data.map(r=>r.name.split('/')[0]))) {
  text+=`\n## ${stage}\n\n| Benchmark | Main ns/op | After ns/op | Change | p |\n|---|---:|---:|---:|---:|\n`;
  text+=data.filter(r=>r.name.split('/')[0]===stage).map(r=>`| ${r.name} | ${number(r.base)} | ${number(r.candidate)} | ${pct(r.delta)} | ${pvalue(r.p)} |`).join('\n')+'\n';
 }
 fs.writeFileSync(`${out}/timings-${mode}-main-vs-after.md`,text);
}
const matched=(source,selector=()=>true)=>listed.map(old=>{
 const hits=source.filter(r=>r.mode===old.mode&&r.name===old.name&&r.unit==='ns/op'&&selector(r));
 if(hits.length!==1)throw Error(`Missing or duplicate listed ${old.mode}/${old.name}`);return hits[0];
});
fs.writeFileSync(`${out}/listed-full.md`,table('Your 23 cases: full-suite samples',matched(rows),`${method}\n\nSix pairs, 100 ms requested time. Bounds labels recover the two otherwise duplicate benchmark names from the earlier report.`));
if(!fullOnly)fs.writeFileSync(`${out}/listed-focused.md`,table('Your 23 cases: longer repeat',matched(focus,r=>r.label==='final'),`${method}\n\nTwelve fresh alternating pairs, 500 ms requested time. These new samples are separate from the full-suite screen. All original listed cases were selected for repetition, regardless of their screen result.`));
const focusedRecords=focus.map(r=>({bounds:r.mode,benchmark:r.name,unit:r.unit,main_median:r.base,after_median:r.candidate,change_percent:r.delta,p:r.p,main_samples:r.baseSamples,after_samples:r.candidateSamples,measurement:note(r)}));
if(!fullOnly)fs.writeFileSync(`${out}/focused-main-vs-after.csv`,csv(Object.keys(focusedRecords[0]),focusedRecords));
const regressions=focus.filter(r=>r.unit==='ns/op'&&r.delta>0&&r.p!==null&&r.p<.05);
if(!fullOnly)fs.writeFileSync(`${out}/confirmed-timing-regressions.md`,table('Timing increases in the longer repeat',regressions,`${method}\n\nPositive changes with p < 0.05. These are unadjusted warnings for further diagnosis, not automatically proven causes.`));
if(!fullOnly&&!confirmedOnly) {
 const selected=JSON.parse(fs.readFileSync(`${root}/timing-diagnostic-screen.json`));
 const diagnostics=[...new Set(selected.map(r=>r.mode))].flatMap(mode=>JSON.parse(fs.readFileSync(`${root}/timing-final-${mode}/comparison.json`)));
 for(const r of selected)if(!diagnostics.some(d=>d.mode===r.mode&&d.name===r.name&&d.unit==='ns/op'&&d.label==='final'))throw Error('Missing timing diagnostic');
 const finalTimings=diagnostics.filter(r=>r.label==='final'&&r.unit==='ns/op');
 const remaining=finalTimings.filter(r=>r.delta>0&&r.p!==null&&r.p<.05);
 const diagnosticRecords=diagnostics.filter(r=>r.label==='final').map(r=>({bounds:r.mode,benchmark:r.name,unit:r.unit,main_median:r.base,after_median:r.candidate,change_percent:r.delta,p:r.p,main_samples:r.baseSamples,after_samples:r.candidateSamples,prior_median:r.prior,after_vs_prior_percent:r.priorDelta,p_vs_prior:r.priorP,measurement:note(r)}));
 fs.writeFileSync(`${out}/timing-diagnostics-main-vs-after.csv`,csv(['bounds','benchmark','unit','main_median','after_median','change_percent','p','main_samples','after_samples','prior_median','after_vs_prior_percent','p_vs_prior','measurement'],diagnosticRecords));
 fs.writeFileSync(`${out}/timing-diagnostics.md`,table('Final timing diagnostics',finalTimings,`${method}\n\nTwenty-four fresh alternating main/prior/after triples; one second requested time. Selection: positive p < 0.05 in the twelve-pair repeat, or a user-listed case still above +5%, or any increase in a user-listed case with main time at least 1 ms. Prior is diagnostic only; it does not replace main. Earlier warning samples remain in confirmed-timing-regressions.md.`));
 fs.writeFileSync(`${out}/remaining-timing-warnings.md`,table('Timing warnings in final diagnostics',remaining,`${method}\n\n${remaining.length} positive changes have p < 0.05 in the final diagnostics. This threshold is unadjusted and is not a proof of cause or equivalence. The earlier twelve-pair warnings remain in confirmed-timing-regressions.md; all final samples remain in timing-diagnostics.md.`));
}
const applications=new Set(['json-as','blake-as','utf-as','json-as-simd','blake-as-simd','utf-as-simd','coremark','blake3','qoi','lz4','zlib','zstd','wasm3','lua','sqlite3','ruby','esbuild']);
const important=rows.filter(r=>r.unit==='ns/op'&&engine(r)==='wago'&&applications.has(r.name.split('/')[1]?.split('.')[0])).sort((a,b)=>b.base-a.base);
fs.writeFileSync(`${out}/application-timings-by-main-time.md`,table('Application timings, largest main time first',important,`${method}\n\nFull-suite samples. Compile, instantiate, and execute measure different operations; their times must not be added.`));
console.log(JSON.stringify({fullOnly,metrics:records.length,timings:records.filter(r=>r.unit==='ns/op').length,listed:listed.length,focused:fullOnly?null:focusedRecords.length,regressions:fullOnly?null:regressions.length}));
if(!fullOnly&&!confirmedOnly)await import('./render-resources.mjs');
