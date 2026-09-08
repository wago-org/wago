import fs from 'node:fs';
import {parse,median,rankP} from './analyze.mjs';
const root='/tmp/wago-pr564-regressions-zHTLww';
for(const run of ['result-layout','result-layout-signals']) {
 const data=parse(`${root}/${run}/layout.bench`),rows=[];
 for(const independent of [false,true])for(const unit of ['ns/op','B/op','allocs/op']) {
  const packed=data.get(`BenchmarkInstanceResultLayout/independent=${independent}/packed=true`)[unit];
  const inline=data.get(`BenchmarkInstanceResultLayout/independent=${independent}/packed=false`)[unit];
  if(packed.length!==6||inline.length!==6)throw Error('Bad diagnostic sample count');
  const a=median(packed),b=median(inline);
  rows.push({independent,unit,packed:a,inline:b,delta:a?100*(b/a-1):b===0?0:null,p:rankP(packed,inline),packedSamples:packed,inlineSamples:inline});
 }
 fs.writeFileSync(`${root}/${run}/summary.json`,JSON.stringify(rows,null,2)+'\n');
 console.log(run,rows.filter(r=>r.unit==='ns/op').map(({packedSamples,inlineSamples,...r})=>r));
}
const rows=fs.readFileSync(`${root}/full/status.jsonl`,'utf8').trim().split('\n').map(JSON.parse).map(r=>({...r,rssKiB:Number(fs.readFileSync(`${root}/full/${r.key}.txt.resource`,'utf8').match(/Maximum resident set size \(kbytes\):\s*(\d+)/)[1])}));
const peaks=[];
for(const mode of ['explicit','signals'])for(const label of ['base','final'])peaks.push(rows.filter(r=>r.mode===mode&&r.label===label).sort((a,b)=>b.rssKiB-a.rssKiB)[0]);
fs.writeFileSync(`${root}/full/rss-peaks.json`,JSON.stringify(peaks,null,2)+'\n');console.log('RSS peaks',peaks);
if(fs.existsSync(`${root}/fixed-work/status.jsonl`)) {
 const samples={};
 for(const label of ['base','final'])samples[label]=fs.readdirSync(`${root}/fixed-work`).filter(s=>s.startsWith(label+'-')&&s.endsWith('.resource')).map(s=>Number(fs.readFileSync(`${root}/fixed-work/${s}`,'utf8').match(/Maximum resident set size \(kbytes\):\s*(\d+)/)[1]));
 const base=median(samples.base),final=median(samples.final);
 const summary={unit:'KiB maximum process RSS',base,final,delta:100*(final/base-1),p:rankP(samples.base,samples.final),samples,notes:['One iteration per compact-compile case, six alternating pairs, explicit build.','Times in this census are not speed measurements.']};
 fs.writeFileSync(`${root}/fixed-work/rss-summary.json`,JSON.stringify(summary,null,2)+'\n');console.log(summary);
}
