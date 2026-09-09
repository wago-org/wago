import fs from 'node:fs';
import {spawn} from 'node:child_process';
import {median,rankP} from './stats.mjs';
const root='/home/jtenner/Projects/wago/.tmp/pr564-absolute-costs';
async function run(file,args=[],env={}) {
 const child=spawn(process.execPath,[`${root}/${file}`,...args],{env:{...process.env,...env},stdio:'inherit'});
 const code=await new Promise(r=>child.on('exit',(c,s)=>r(c??s)));
 if(code!==0)throw Error(`${file}: ${code}`);
}
for(const mode of ['signals','explicit']) {
 const runName=`compiler-fixed-${mode}`,dir=`${root}/${runName}`;
 await run('focused.mjs',[runName],{CASES:JSON.stringify([[mode,'BenchmarkCompileCompact']]),LABELS:'base,before,final',SAMPLES:'6',BENCHTIME:'1x',RESOURCE_LOGS:'1'});
 await run('focused-analysis.mjs',[runName]);
 await run('focused-report.mjs',[runName]);
 const status=fs.readFileSync(`${dir}/status.jsonl`,'utf8').trim().split('\n').map(JSON.parse);
 const rss={};
 for(const label of ['base','before','final'])rss[label]=status.filter(r=>r.label===label&&r.code===0).map(r=>{
  const text=fs.readFileSync(`${dir}/${r.key}.txt.resource`,'utf8');
  const match=text.match(/Maximum resident set size \(kbytes\):\s*(\d+)/);if(!match)throw Error('Missing RSS');return +match[1];
 });
 const summary={mode,unit:'KiB maximum whole-process RSS',operation:'one compact compile per corpus module; ISA included',samples:rss,main:median(rss.base),prior:median(rss.before),after:median(rss.final),delta:100*(median(rss.final)/median(rss.base)-1),p:rankP(rss.base,rss.final)};
 fs.writeFileSync(`${dir}/rss-summary.json`,JSON.stringify(summary,null,2)+'\n');
 console.log(summary);
}
