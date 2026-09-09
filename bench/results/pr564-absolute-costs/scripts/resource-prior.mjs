import fs from 'node:fs';
import {spawn} from 'node:child_process';
const root='/home/jtenner/Projects/wago/.tmp/pr564-absolute-costs';
async function run(file,args=[],env={}) {
 const child=spawn(process.execPath,[`${root}/${file}`,...args],{env:{...process.env,...env},stdio:'inherit'});
 const code=await new Promise(r=>child.on('exit',(c,s)=>r(c??s)));
 if(code!==0)throw Error(`${file}: ${code}`);
}
// This script is reached only after both confirmation runs and equal-count
// instantiation checks. Keep extra timing diagnostics serial with those jobs.
await run('timing-diagnostic.mjs');
const warnings=[];
for(const mode of ['signals','explicit']) {
 const rows=JSON.parse(fs.readFileSync(`${root}/confirm-${mode}/comparison.json`));
 const resource=rows.filter(r=>!r.name.startsWith('BenchmarkPluginExec/')&&['B/op','allocs/op','B/call','allocs/call'].includes(r.unit)&&r.candidate>r.base&&(r.delta>1||r.base===0));
 warnings.push(...resource);
 const compiler=resource.filter(r=>/^Benchmark(?:Compile|Validate|Decode)/.test(r.name));
 for(const [suffix,time,samples]of [['warm','500ms','12'],['fixed','1x','6']]) {
  const selected=suffix==='warm'?compiler.filter(r=>r.unit==='B/op'&&r.delta>1&&r.p!==null&&r.p<.05):compiler;
  const cases=[...new Set(selected.map(r=>r.name))].map(name=>[mode,name]);
  if(!cases.length)continue;
  const dir=`resource-${mode}-${suffix}`;
  await run('focused.mjs',[dir],{CASES:JSON.stringify(cases),LABELS:'base,before,final',SAMPLES:samples,BENCHTIME:time});
  await run('focused-analysis.mjs',[dir]);
  await run('focused-report.mjs',[dir]);
 }
}
fs.writeFileSync(`${root}/resource-confirmation-screen.json`,JSON.stringify(warnings,null,2)+'\n');
