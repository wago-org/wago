import fs from 'node:fs';
import {spawn} from 'node:child_process';
const root='/home/jtenner/Projects/wago/.tmp/pr564-absolute-costs';
async function run(file,args=[],env={}) {
 const child=spawn(process.execPath,[`${root}/${file}`,...args],{env:{...process.env,...env},stdio:'inherit'});
 const code=await new Promise(r=>child.on('exit',(c,s)=>r(c??s)));
 if(code!==0)throw Error(`${file}: ${code}`);
}
for(const mode of ['explicit','signals'])for(const label of ['base','final'])if(!fs.existsSync(`${root}/full/${label}-${mode}.bench`))throw Error('Full suite incomplete');
await run('analyze.mjs');
await run('listed-cases.mjs');
const rows=JSON.parse(fs.readFileSync(`${root}/full/comparison.json`));
const listed=JSON.parse(fs.readFileSync(`${root}/listed-cases.json`));
const wago=r=>!/Wazero|_wazero/.test(r.name);
const batched=new Set(rows.filter(r=>r.unit==='calls/batch').map(r=>`${r.mode}/${r.name}`));
const timing=rows.filter(r=>wago(r)&&r.unit==='ns/op'&&r.delta>0&&(r.p!==null&&r.p<.05||r.delta>5));
const resources=rows.filter(r=>{
 if(!wago(r)||r.name.startsWith('BenchmarkPluginExec/')||!(r.candidate>r.base&&(r.delta>1||r.base===0)))return false;
 return batched.has(`${r.mode}/${r.name}`)?['B/call','allocs/call'].includes(r.unit):['B/op','allocs/op'].includes(r.unit);
});
fs.writeFileSync(`${root}/full/confirmation-screen.json`,JSON.stringify({timing,resources,listed},null,2));
for(const mode of ['explicit','signals']) {
 const names=new Set([...timing,...resources,...listed,...rows.filter(r=>/^BenchmarkPlugin(?:Instantiate|Exec)\//.test(r.name))].filter(r=>r.mode===mode).map(r=>r.name));
 const time=name=>rows.find(r=>r.mode===mode&&r.name===name&&r.unit==='ns/op')?.base??0;
 const cases=[...names].sort((a,b)=>time(b)-time(a)).map(name=>[mode,name]);
 console.log(`Confirming ${cases.length} ${mode} cases`);
 await run('focused.mjs',[`confirm-${mode}`],{CASES:JSON.stringify(cases),LABELS:'base,final',SAMPLES:'12',BENCHTIME:'500ms'});
 await run('focused-analysis.mjs',[`confirm-${mode}`]);
 await run('focused-report.mjs',[`confirm-${mode}`]);
}
await run('focused-report.mjs',['largest']);
await run('qualification.mjs');
// Equal operation counts separate true heap savings from Go's calibration and
// the first-instance preparation cost amortized across each timed sample.
for(const mode of ['signals','explicit']) {
 const cases=['ruby','esbuild','sqlite3','wasm3','lua'].map(name=>[mode,`BenchmarkPluginInstantiate/${name}`]);
 const dir=`instantiate-fixed-${mode}`;
 await run('focused.mjs',[dir],{CASES:JSON.stringify(cases),LABELS:'base,before,final',SAMPLES:'6',BENCHTIME:'128x'});
 await run('focused-analysis.mjs',[dir]);
 await run('focused-report.mjs',[dir]);
}
await run('resource-prior.mjs');
await run('fixed-work.mjs');
await run('audit-all.mjs');
// Release builds start only after every queued timed comparison has drained.
await run('size.mjs');
await run('render.mjs');
fs.writeFileSync(`${root}/pipeline-complete.json`,JSON.stringify({completed:new Date().toISOString(),note:'Review all warnings before final qualification.'},null,2)+'\n');
