import fs from 'node:fs';
import {parse,median,rankP} from './analyze.mjs';
const root='/tmp/wago-pr564-regressions-zHTLww',rows=[];
for(const mode of ['explicit','signals']) {
 const dir=`${root}/confirm-${mode}`;if(!fs.existsSync(`${dir}/status.jsonl`))continue;
 const status=fs.readFileSync(`${dir}/status.jsonl`,'utf8').trim().split('\n').map(JSON.parse);
 for(const name of new Set(status.map(r=>r.name))) {
  const group=status.filter(r=>r.name===name&&r.code===0);if(group.length!==24)continue;
  const samples={base:[],final:[]};
  for(const r of group) {
   const values=parse(`${dir}/${r.key}.txt`).get(name)?.['ns/op'];
   if(values?.length!==1)throw Error(`Unexpected sample ${r.key}`);
   samples[r.label].push(values[0]);
  }
  if(samples.base.length!==12||samples.final.length!==12)throw Error(`Bad sample count ${name}`);
  const base=median(samples.base),final=median(samples.final);
  rows.push({mode,name,base,final,delta:100*(final/base-1),p:rankP(samples.base,samples.final)});
 }
}
console.log(JSON.stringify({completed:rows.length,positive:rows.filter(r=>r.delta>0),confirmed:rows.filter(r=>r.delta>0&&r.p<.05)},null,2));
