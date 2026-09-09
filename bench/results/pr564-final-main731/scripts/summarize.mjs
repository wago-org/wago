import fs from 'node:fs';
const root='/home/jtenner/Projects/wago/.tmp/pr564-memory/final-closures';
const read=p=>JSON.parse(fs.readFileSync(`${root}/${p}`));
const full=read('full/comparison.json');
const confirmed=['explicit','signals'].flatMap(mode=>read(`confirm-${mode}/comparison.json`));
const fixed=['explicit','signals'].flatMap(mode=>['memory','instantiate','rss'].flatMap(s=>read(`fixed-${s}-${mode}/comparison.json`).map(r=>({...r,run:s}))));
const describe=r=>({bounds:r.mode,benchmark:r.name,unit:r.unit,main:r.base,after:r.candidate,absoluteChange:r.candidate-r.base,changePercent:r.delta,p:r.p,samplesEach:r.baseSamples,...(r.run?{run:r.run}:{})});
const timingWarnings=rows=>rows.filter(r=>r.unit==='ns/op'&&!/Wazero|_wazero/.test(r.name)&&r.delta>0&&r.p!==null&&r.p<.05).map(describe).sort((a,b)=>b.absoluteChange-a.absoluteChange);
const memory=rows=>Object.fromEntries(['B/op','allocs/op'].map(unit=>[unit,rows.filter(r=>r.unit===unit&&!/Wazero|_wazero|Exec/.test(r.name)&&r.candidate>r.base).map(describe).sort((a,b)=>b.absoluteChange-a.absoluteChange)]));
const listed=read('listed-cases.json').map(r=>{
 const hits=confirmed.filter(x=>x.mode===r.mode&&x.name===r.name&&x.unit==='ns/op');
 if(hits.length!==1)throw Error(`listed coverage ${r.mode}/${r.name}`);return describe(hits[0]);
});
const summary={
 main:read('metadata.json').main,head:read('metadata.json').head,
 fullMetrics:full.length,fullTimings:full.filter(r=>r.unit==='ns/op').length,
 stages:read('full/summary.json').filter(r=>r.corpus==='default'&&r.unit==='ns/op'&&['CompileFull','CompileCompact','Instantiate','Exec','ExecParallel','PluginInstantiate'].includes(r.stage)),
 fullTimingWarnings:timingWarnings(full),confirmedTimingWarnings:timingWarnings(confirmed),
 repeatCases:read('repeat-selection.json').length,confirmedMetrics:confirmed.length,listed,
 rubyFixed:fixed.filter(r=>r.run==='instantiate'&&r.name==='BenchmarkPluginInstantiate/ruby').map(describe),
 coldJSON8:fixed.filter(r=>r.run==='memory'&&r.name==='BenchmarkCompileWorkers/json-as/p8'&&r.unit!=='ns/op').map(describe),
 confirmedMemoryWarnings:memory(confirmed.filter(r=>r.p!==null&&r.p<.05)),
 allFixedMemoryIncreases:memory(fixed),releaseSizes:read('size.json').rows.map(r=>({label:r.label,name:r.name,bytes:r.bytes,budget:r.budget})),
};
fs.writeFileSync(`${root}/summary-inspection.json`,JSON.stringify(summary,null,2)+'\n');
console.log(JSON.stringify({fullMetrics:summary.fullMetrics,fullTimingWarnings:summary.fullTimingWarnings.length,repeatCases:summary.repeatCases,confirmedTimingWarnings:summary.confirmedTimingWarnings.length},null,2));
