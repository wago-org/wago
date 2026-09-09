import fs from 'node:fs';
import {parse,median,rankP} from './stats.mjs';
const root='/home/jtenner/Projects/wago/.tmp/pr564-last-pass';
for(const dirName of process.argv.slice(2)) {
 const dir=`${root}/${dirName}`,identity=JSON.parse(fs.readFileSync(`${dir}/identity.json`));
 const inputs=Object.fromEntries(identity.labels.map(label=>[label,parse(`${dir}/${label}.bench`)]));
 const rows=[];
 const names=[...inputs.base.keys()].map(name=>{
  const matches=identity.names.filter(([mode,target])=>name===target||name.startsWith(target+'/'));
  if(matches.length!==1)throw Error(`${dirName}/${name}: ambiguous or unexpected case`);
  return [matches[0][0],name];
 });
 for(const [mode,target]of identity.names)if(!names.some(([m,n])=>m===mode&&(n===target||n.startsWith(target+'/'))))throw Error(`Missing ${mode}/${target}`);
 for(const [mode,name] of names) {
  const metrics=inputs.base.get(name);if(!metrics)throw Error(`Missing ${name}`);
  for(const [unit,a] of Object.entries(metrics))for(const label of identity.labels.filter(l=>l!=='base')) {
   const b=inputs[label].get(name)?.[unit];if(!b||a.length!==identity.samples||b.length!==identity.samples)throw Error(`${dirName}/${name}/${unit}: sample mismatch`);
   const base=median(a),candidate=median(b);
   const row={mode,name,unit,label,base,candidate,delta:base?100*(candidate/base-1):candidate?null:0,p:rankP(a,b),baseSamples:a.length,candidateSamples:b.length,baseValues:a,candidateValues:b};
   if(label==='final'&&inputs.before) {
    const prior=inputs.before.get(name)?.[unit];if(!prior)throw Error('Missing prior');
    row.prior=median(prior);row.priorDelta=row.prior?100*(candidate/row.prior-1):candidate?null:0;row.priorP=rankP(prior,b);
   }
   rows.push(row);
  }
 }
 fs.writeFileSync(`${dir}/comparison.json`,JSON.stringify(rows,null,2)+'\n');
}
