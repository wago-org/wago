import fs from 'node:fs';
import path from 'node:path';
import {execFileSync} from 'node:child_process';
import {createHash} from 'node:crypto';
import {median,rankP} from './stats.mjs';
const repo='/home/jtenner/Projects/wago',root=`${repo}/.tmp/pr564-memory`,out=`${repo}/bench/results/pr564-memory`;
fs.mkdirSync(out,{recursive:true});
const csv=(heads,rows)=>heads.join(',')+'\n'+rows.map(r=>heads.map(h=>{const s=r[h]==null?'':String(r[h]);return /[,"\n]/.test(s)?'"'+s.replaceAll('"','""')+'"':s;}).join(',')).join('\n')+'\n';
const rows=[],rss=[],raw=[];
for(const mode of ['explicit','signals'])for(const kind of ['warm','memory','instantiate','rss']) {
 const run=kind==='warm'?`compact-${mode}`:`compact-${kind}-${mode}`,dir=`${root}/${run}`;
 const identity=JSON.parse(fs.readFileSync(`${dir}/identity.json`));
 const comparisons=JSON.parse(fs.readFileSync(`${dir}/comparison.json`));
 for(const r of comparisons.filter(r=>r.label==='compact')) {
  const prior=comparisons.find(p=>p.label==='before'&&p.name===r.name&&p.unit===r.unit);
  rows.push({run,bounds:mode,benchmark:r.name,unit:r.unit,main_median:r.base,after_median:r.candidate,change_percent:r.delta,p:r.p,samples_each:r.baseSamples,pre_fix_median:prior.candidate,after_vs_pre_fix_percent:100*(r.candidate/prior.candidate-1),benchtime:identity.benchtime});
 }
 if(identity.resourceLogs) {
  const status=fs.readFileSync(`${dir}/status.jsonl`,'utf8').trim().split('\n').map(JSON.parse);
  for(const name of new Set(status.map(s=>s.name))) {
   const values=label=>status.filter(s=>s.label===label&&s.name===name).map(s=>+fs.readFileSync(`${dir}/${s.key}.txt.resource`,'utf8').match(/Maximum resident set size \(kbytes\):\s*(\d+)/)[1]);
   const a=values('base'),b=values('compact');
   if(a.length!==6||b.length!==6)throw Error('RSS samples');
   rss.push({run,bounds:mode,benchmark:name,main_median_KiB:median(a),after_median_KiB:median(b),change_percent:100*(median(b)/median(a)-1),p:rankP(a,b),samples_each:6,benchtime:identity.benchtime});
  }
 }
 for(const f of ['identity.json','comparison.json','status.jsonl','base.bench','before.bench','compact.bench']) {
  fs.mkdirSync(`${out}/${run}`,{recursive:true});fs.copyFileSync(`${dir}/${f}`,`${out}/${run}/${f}`);
 }
 raw.push(...fs.readdirSync(dir).filter(f=>/\.txt(?:\.resource)?$/.test(f)).map(f=>`${run}/${f}`));
}
fs.writeFileSync(`${out}/main-vs-after.csv`,csv(['run','bounds','benchmark','unit','main_median','after_median','change_percent','p','samples_each','pre_fix_median','after_vs_pre_fix_percent','benchtime'],rows));
fs.writeFileSync(`${out}/equal-work-rss.csv`,csv(['run','bounds','benchmark','main_median_KiB','after_median_KiB','change_percent','p','samples_each','benchtime'],rss));
execFileSync('tar',['-czf',`${out}/raw-process-logs.tar.gz`,'-C',root,...raw]);
for(const f of ['compact-metadata.json','compact-source.patch','audit-compact-summary.json','compact-native.txt','compact-guard.txt','compact-race.txt','compact-arm64.txt','compact-arm64-signals.txt','compact-owner-test.txt'])fs.copyFileSync(`${root}/${f}`,`${out}/${f}`);
fs.mkdirSync(`${out}/scripts`,{recursive:true});
for(const f of ['focused.mjs','focused-report.mjs','stats.mjs','compact-qualify.mjs','compact-fixed.mjs','export-memory.mjs','audit-compact.mjs'])fs.copyFileSync(`${root}/${f}`,`${out}/scripts/${f}`);
fs.copyFileSync(`${root}/code_audit_test.go`,`${out}/scripts/code_audit_test.go.txt`);
const hash=s=>createHash('sha256').update(s).digest('hex');
const files=fs.readdirSync(out,{recursive:true}).filter(f=>f!=='SHA256SUMS'&&fs.statSync(path.join(out,f)).isFile()).sort();
fs.writeFileSync(`${out}/SHA256SUMS`,files.map(f=>`${hash(fs.readFileSync(path.join(out,f)))}  ${f}`).join('\n')+'\n');
console.log({metrics:rows.length,rss:rss.length,files:files.length,rawLogs:raw.length});
