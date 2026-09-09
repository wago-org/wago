import fs from 'node:fs';
import {parse,median,rankP} from './stats.mjs';
const root='/home/jtenner/Projects/wago/.tmp/pr564-absolute-costs';
for(const mode of ['explicit','signals']) {
 const dir=`${root}/confirm-${mode}`;
 if(!fs.existsSync(`${dir}/status.jsonl`))continue;
 const identity=JSON.parse(fs.readFileSync(`${dir}/identity.json`));
 const records=fs.readFileSync(`${dir}/status.jsonl`,'utf8').trim().split('\n').filter(Boolean).map(JSON.parse);
 const warnings=[];
 let complete=0;
 for(const [bounds,name]of identity.names) {
  const byLabel=Object.fromEntries(['base','final'].map(label=>[label,records.filter(r=>r.mode===bounds&&r.name===name&&r.label===label&&r.code===0)]));
  if(Object.values(byLabel).some(r=>r.length!==identity.samples))continue;
  complete++;
  const values=Object.fromEntries(Object.entries(byLabel).map(([label,rs])=>[label,rs.flatMap(r=>parse(`${dir}/${r.key}.txt`).get(name)?.['ns/op']??[])]));
  if(Object.values(values).some(r=>r.length!==identity.samples))throw Error(`Missing timing ${name}`);
  const base=median(values.base),after=median(values.final),p=rankP(values.base,values.final),delta=100*(after/base-1);
  if(delta>0&&p!==null&&p<.05)warnings.push({name,base,after,delta,p});
 }
 console.log(JSON.stringify({mode,complete,total:identity.names.length,failed:records.filter(r=>r.code!==0).length,warnings},null,2));
}
