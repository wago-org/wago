import fs from 'node:fs';
import path from 'node:path';
import {execFileSync} from 'node:child_process';
import {createHash} from 'node:crypto';
import {median,rankP} from './stats.mjs';
const repo='/home/jtenner/Projects/wago',root=`${repo}/.tmp/pr564-last-pass`,out=`${repo}/bench/results/pr564-last-pass`;
const read=p=>JSON.parse(fs.readFileSync(`${root}/${p}`));
const plan=read('plan.json');
const statuses=read('confirm-status.json');
if(statuses.length!==4||statuses.some(r=>r.code!==0))throw Error('Incomplete confirmation');
fs.mkdirSync(out,{recursive:true});
const copy=(from,to=from)=>{fs.mkdirSync(path.dirname(`${out}/${to}`),{recursive:true});fs.copyFileSync(`${root}/${from}`,`${out}/${to}`);};
const csv=(rows,keys=Object.keys(rows[0]))=>keys.join(',')+'\n'+rows.map(r=>keys.map(k=>{const s=r[k]==null?'':String(r[k]);return /[,"\n]/.test(s)?'"'+s.replaceAll('"','""')+'"':s;}).join(',')).join('\n')+'\n';
const metrics=[],rss=[],raw=[];
let processes=0;
for(const p of [...plan.plan,{name:'join-screen',benchtime:'1x',samples:3}]){
 const dir=`${root}/${p.name}`,identity=read(`${p.name}/identity.json`),rows=read(`${p.name}/comparison.json`),runs=fs.readFileSync(`${dir}/status.jsonl`,'utf8').trim().split('\n').map(JSON.parse);
 if(runs.length!==identity.names.length*p.samples*3||runs.some(r=>r.code!==0)||new Set(runs.map(r=>r.key)).size!==runs.length)throw Error(`Process coverage ${p.name}`);
 if(rows.some(r=>r.baseSamples!==p.samples||r.candidateSamples!==p.samples||r.status))throw Error('Unpaired data');
 if(p.name!=='join-screen')processes+=runs.length;
 for(const r of rows)metrics.push({run:p.name,bounds:r.mode,benchmark:r.name,version:r.label,unit:r.unit,main_median:r.base,after_median:r.candidate,absolute_change:r.candidate-r.base,change_percent:r.delta,p:r.p,prior_PR_median:r.prior,change_vs_prior_percent:r.priorDelta,p_vs_prior:r.priorP,samples_each:r.baseSamples,note:p.benchtime==='1x'?'One timed operation; memory check, not a timing claim.':'250 ms requested timing; batched allocation counters need per-call interpretation.'});
 for(const[mode,name]of identity.names){
  const values=label=>runs.filter(r=>r.label===label&&r.name===name).map(r=>+fs.readFileSync(`${dir}/${r.key}.txt.resource`,'utf8').match(/Maximum resident set size \(kbytes\):\s*(\d+)/)[1]);
  for(const label of identity.labels.filter(l=>l!=='base')){
   const a=values('base'),b=values(label);if(a.length!==p.samples||b.length!==p.samples)throw Error('RSS samples');
   rss.push({run:p.name,bounds:mode,benchmark:name,version:label,main_KiB:median(a),after_KiB:median(b),change_percent:100*(median(b)/median(a)-1),p:rankP(a,b),samples_each:p.samples,note:p.benchtime==='1x'?'Equal-work whole-process peak, includes setup and other decoded corpus modules.':'Adaptive iteration counts: raw process evidence, not equal-work RSS.'});
  }
 }
 for(const f of ['identity.json','comparison.json','status.jsonl',...identity.labels.map(l=>`${l}.bench`)])copy(`${p.name}/${f}`);
 raw.push(...fs.readdirSync(dir).filter(f=>/\.txt(?:\.resource)?$/.test(f)).map(f=>`${p.name}/${f}`));
}
if(processes!==468)throw Error('Wrong process count');
fs.writeFileSync(`${out}/all-measurements.csv`,csv(metrics));
fs.writeFileSync(`${out}/all-process-rss.csv`,csv(rss));
const final=metrics.filter(r=>r.version==='final');
fs.writeFileSync(`${out}/remaining-increases.csv`,csv(final.filter(r=>r.after_median>r.main_median&&r.unit!=='calls/batch'&&!(r.run.startsWith('cold')&&r.unit==='ns/op')),Object.keys(metrics[0])));
const summary={processes,benchmarkSeconds:statuses.reduce((s,r)=>s+r.seconds,0),metricRows:metrics.length,cold:final.filter(r=>r.run.startsWith('cold')&&['B/op','allocs/op'].includes(r.unit)),timings:final.filter(r=>r.run.startsWith('timed')&&r.unit==='ns/op'),rss:rss.filter(r=>r.run.startsWith('cold')&&r.version==='final')};
fs.writeFileSync(`${root}/summary.json`,JSON.stringify(summary,null,2)+'\n');
for(const f of ['plan.json','confirm-status.json','checks.json','code-audit.json','summary.json','source.patch'])copy(f);
for(const f of ['focused.mjs','focused-report.mjs','stats.mjs','confirm.mjs','checks.mjs','export.mjs'])copy(f,`scripts/${f}`);
for(const c of read('checks.json'))copy(`${c.name}.txt`,`checks/${c.name}.txt`);
for(const arch of ['amd64','arm64'])fs.copyFileSync(`${repo}/src/core/compiler/backend/railshot/${arch}/memory_scratch_test.go`,`${out}/scripts/memory_scratch_${arch}_test.go.txt`);
for(const f of ['b4-esbuild-p4.mem','main-esbuild-p4.mem'])copy(f,`profiles/${f}`);
execFileSync('tar',['-czf',`${out}/raw-process-logs.tar.gz`,'-C',root,...raw]);
const files=fs.readdirSync(out,{recursive:true}).filter(f=>f!=='SHA256SUMS'&&fs.statSync(`${out}/${f}`).isFile()).sort();
fs.writeFileSync(`${out}/SHA256SUMS`,files.map(f=>`${createHash('sha256').update(fs.readFileSync(`${out}/${f}`)).digest('hex')}  ${f}`).join('\n')+'\n');
console.log(JSON.stringify({processes,benchmarkSeconds:summary.benchmarkSeconds,metricRows:metrics.length,remainingTimingIncreases:summary.timings.filter(r=>r.after_median>r.main_median),cold:summary.cold,rss:summary.rss},null,2));
