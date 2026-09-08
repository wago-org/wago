import fs from 'node:fs';
import {execFileSync} from 'node:child_process';
const root='/tmp/wago-pr564-regressions-zHTLww';
const median=a=>{a=[...a].sort((x,y)=>x-y);return a.length%2?a[(a.length-1)/2]:(a[a.length/2-1]+a[a.length/2])/2;};
function parse(file) {
 const rows={};
 for(const line of fs.readFileSync(file,'utf8').split('\n')) {
  const p=line.trim().split(/\s+/);if(!p[0].startsWith('Benchmark')||!/^\d+$/.test(p[1]))continue;
  const name=p[0].replace(/-\d+$/,'');
  for(let i=2;i<p.length-1;i+=2)if(Number.isFinite(+p[i]))((rows[name]??={})[p[i+1]]??=[]).push(+p[i]);
 }
 return rows;
}
for(const run of process.argv.length>2?process.argv.slice(2):['recheck','inline','half','global-one','final','compiler-final','arena-census','density-census','bounded-census']) {
 const dir=`${root}/${run}`;
 const labels=fs.readdirSync(dir).filter(s=>s.endsWith('.bench')).map(s=>s.slice(0,-6));
 const ordered=['base',...labels.filter(s=>s!=='base'&&s!=='before'),'before'].filter(s=>labels.includes(s));
 const inputs=Object.fromEntries(ordered.map(l=>[l,parse(`${dir}/${l}.bench`)]));
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
 const table=execFileSync('/tmp/wago-pr564-HbN430/bin/benchstat',ordered.map(l=>`${dir}/${l}.bench`),{encoding:'utf8'});
 fs.writeFileSync(`${dir}/benchstat.txt`,table.split('\n').map(l=>l.trimEnd()).join('\n'));
}
