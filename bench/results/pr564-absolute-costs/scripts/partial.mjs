import fs from 'node:fs';
import {parse,median,rankP} from './stats.mjs';
const root='/home/jtenner/Projects/wago/.tmp/pr564-absolute-costs',dir=`${root}/full`;
const status=fs.readFileSync(`${dir}/status.jsonl`,'utf8').trim().split('\n').filter(Boolean).map(JSON.parse).filter(r=>r.code===0);
const top=process.argv[2]||'BenchmarkPluginInstantiate';
for(const mode of ['signals','explicit']) {
 const inputs={};
 for(const label of ['base','final']) {
  const files=status.filter(r=>r.mode===mode&&r.label===label&&r.top===top);
  if(files.length!==6)continue;
  const all=new Map();
  for(const f of files)for(const [name,metrics]of parse(`${dir}/${f.key}.txt`))for(const [unit,values]of Object.entries(metrics)) {
   if(!all.has(name))all.set(name,{});(all.get(name)[unit]??=[]).push(...values);
  }
  inputs[label]=all;
 }
 if(!inputs.base||!inputs.final)continue;
 for(const [name,metrics]of inputs.base)for(const [unit,a]of Object.entries(metrics)) {
  if(!['ns/op','B/op','allocs/op','B/call','allocs/call'].includes(unit))continue;
  const b=inputs.final.get(name)?.[unit];if(!b||a.length!==6||b.length!==6)throw Error('Partial sample mismatch');
  const base=median(a),after=median(b);
  console.log(JSON.stringify({mode,name,unit,base,after,delta:base?100*(after/base-1):after?null:0,p:rankP(a,b)}));
 }
}
