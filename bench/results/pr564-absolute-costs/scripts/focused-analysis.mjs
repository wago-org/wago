import fs from 'node:fs';
import {execFileSync} from 'node:child_process';
import {parse as parseMetrics} from './stats.mjs';
const root='/home/jtenner/Projects/wago/.tmp/pr564-absolute-costs';
const median=a=>{a=[...a].sort((x,y)=>x-y);return a.length%2?a[(a.length-1)/2]:(a[a.length/2-1]+a[a.length/2])/2;};
for(const run of process.argv.length>2?process.argv.slice(2):['recheck','inline','half','global-one','final','compiler-final','arena-census','density-census','bounded-census']) {
 const dir=`${root}/${run}`;
 const labels=fs.readdirSync(dir).filter(s=>s.endsWith('.bench')).map(s=>s.slice(0,-6));
 const ordered=['base',...labels.filter(s=>s!=='base'&&s!=='before'),'before'].filter(s=>labels.includes(s));
 const inputs=Object.fromEntries(ordered.map(l=>[l,Object.fromEntries(parseMetrics(`${dir}/${l}.bench`))]));
 const rows=[];
 for(const [name,metrics]of Object.entries(inputs.base))for(const [unit,samples]of Object.entries(metrics)) {
  const row={name,unit,base:median(samples),samples:{}};
  for(const label of ordered) {
   const a=inputs[label][name]?.[unit];if(!a)throw Error(`${run}/${label}/${name}/${unit} missing`);
   const value=median(a);row.samples[label]={n:a.length,median:value,delta:row.base?100*(value/row.base-1):value===0?0:null,values:a};
  }
  rows.push(row);
 }
 fs.writeFileSync(`${dir}/summary.json`,JSON.stringify(rows,null,2));
 const table=execFileSync('/home/jtenner/Projects/wago/.tmp/pr564-absolute-costs/bin/benchstat',ordered.map(l=>`${dir}/${l}.bench`),{encoding:'utf8'});
 fs.writeFileSync(`${dir}/benchstat.txt`,table.split('\n').map(l=>l.trimEnd()).join('\n'));
}
