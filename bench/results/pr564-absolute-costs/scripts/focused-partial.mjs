import fs from 'node:fs';
import {parse,median,rankP} from './stats.mjs';
const root='/home/jtenner/Projects/wago/.tmp/pr564-absolute-costs',dir=`${root}/${process.argv[2]}`;
const identity=JSON.parse(fs.readFileSync(`${dir}/identity.json`));
const status=fs.readFileSync(`${dir}/status.jsonl`,'utf8').trim().split('\n').filter(Boolean).map(JSON.parse).filter(r=>r.code===0);
for(const [mode,name] of identity.names.filter(([m,n])=>process.argv.slice(3).some(s=>n.includes(s)))) {
 const inputs={};
 for(const label of identity.labels) {
  const samples=status.filter(r=>r.name===name&&r.label===label&&r.mode===mode);
  if(samples.length!==identity.samples)continue;
  const metrics={};
  for(const sample of samples)for(const [unit,values]of Object.entries(parse(`${dir}/${sample.key}.txt`).get(name)??{}))(metrics[unit]??=[]).push(...values);
  inputs[label]=metrics;
 }
 if(!inputs.base||!inputs.final)continue;
 for(const unit of ['ns/op','B/op','allocs/op','B/call','allocs/call']) {
  const a=inputs.base[unit],b=inputs.final[unit];if(!a||!b)continue;
  console.log(JSON.stringify({mode,name,unit,main:median(a),after:median(b),delta:median(a)?100*(median(b)/median(a)-1):median(b)?null:0,p:rankP(a,b),n:a.length}));
 }
}
