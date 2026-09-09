import fs from 'node:fs';
import {parse,median,rankP} from './stats.mjs';
const root='/home/jtenner/Projects/wago/.tmp/pr564-absolute-costs';
const jsonl=p=>fs.existsSync(p)?fs.readFileSync(p,'utf8').trim().split('\n').filter(Boolean).map(JSON.parse):[];
for(const name of ['largest','full','confirm-signals','confirm-explicit']) {
 const records=jsonl(`${root}/${name}/status.jsonl`);
 if(!records.length)continue;
 const last=records.at(-1);
 console.log({run:name,completed:records.filter(r=>r.code===0).length,failed:records.filter(r=>r.code!==0).length,last:last.key,ended:last.end});
}
if(fs.existsSync(`${root}/full/progress.json`))console.log(JSON.parse(fs.readFileSync(`${root}/full/progress.json`)));
if(process.argv.includes('--largest')&&fs.existsSync(`${root}/largest/summary.json`)) {
 for(const r of JSON.parse(fs.readFileSync(`${root}/largest/summary.json`)).filter(r=>r.unit==='ns/op'||r.name.startsWith('BenchmarkPluginInstantiate')))console.log({name:r.name,unit:r.unit,main:r.base,after:r.samples.final.median,delta:r.samples.final.delta,p:rankP(r.samples.base.values,r.samples.final.values),prior:r.samples.before.median});
}
