import fs from 'node:fs';
import path from 'node:path';
import {execFileSync} from 'node:child_process';
import {createHash} from 'node:crypto';
import {median,rankP} from './stats.mjs';
const repo='/home/jtenner/Projects/wago',root=`${repo}/.tmp/pr564-memory/final731`,out=`${repo}/bench/results/pr564-worker-closures`;
fs.mkdirSync(out,{recursive:true});
const copy=(s,d=s)=>{fs.mkdirSync(path.dirname(`${out}/${d}`),{recursive:true});fs.copyFileSync(`${root}/${s}`,`${out}/${d}`);};
const csv=(heads,rows)=>heads.join(',')+'\n'+rows.map(r=>heads.map(h=>{const s=r[h]==null?'':String(r[h]);return /[,"\n]/.test(s)?'"'+s.replaceAll('"','""')+'"':s;}).join(',')).join('\n')+'\n';
const rows=[],rss=[],raw=[];
for(const variant of ['hintwork','hintscratch'])for(const mode of ['explicit','signals'])for(const kind of ['cold','warm',...(variant==='hintscratch'?['esbuild-fixed']:[])]) {
 const run=`${variant}-${kind}-${mode}`,dir=`${root}/${run}`;
 const id=JSON.parse(fs.readFileSync(`${dir}/identity.json`)),comp=JSON.parse(fs.readFileSync(`${dir}/comparison.json`));
 const status=fs.readFileSync(`${dir}/status.jsonl`,'utf8').trim().split('\n').map(JSON.parse);
 if(status.some(r=>r.code!==0)||status.length!==id.samples*id.labels.length*id.names.length)throw Error(`Incomplete ${run}`);
 for(const r of comp.filter(r=>r.label===variant)) {
  const prior=comp.find(p=>p.label==='final'&&p.mode===r.mode&&p.name===r.name&&p.unit===r.unit);
  if(!prior||r.baseSamples!==id.samples||r.candidateSamples!==id.samples)throw Error('Samples');
  rows.push({variant,run,bounds:mode,benchmark:r.name,unit:r.unit,main_median:r.base,prior_pr_median:prior.candidate,after_median:r.candidate,change_vs_main_percent:r.delta,p_vs_main:r.p,change_vs_prior_percent:100*(r.candidate/prior.candidate-1),p_vs_prior:rankP(prior.candidateValues,r.candidateValues),samples_each:id.samples,benchtime:id.benchtime,note:kind==='cold'?'memory check, not a timing claim':variant==='hintwork'?'rejected serial-gate experiment':'retained-span and closure reuse'});
 }
 if(id.resourceLogs) {
  const values=label=>status.filter(r=>r.label===label).map(r=>+fs.readFileSync(`${dir}/${r.key}.txt.resource`,'utf8').match(/Maximum resident set size \(kbytes\):\s*(\d+)/)[1]);
  const a=values('base'),b=values('final'),c=values(variant);
  rss.push({run,bounds:mode,main_KiB:median(a),prior_pr_KiB:median(b),after_KiB:median(c),change_vs_main_percent:100*(median(c)/median(a)-1),p_vs_main:rankP(a,c),change_vs_prior_percent:100*(median(c)/median(b)-1),p_vs_prior:rankP(b,c),samples_each:id.samples,benchtime:id.benchtime});
 }
 for(const f of ['identity.json','comparison.json','status.jsonl',...id.labels.map(l=>`${l}.bench`)])copy(`${run}/${f}`);
 raw.push(...fs.readdirSync(dir).filter(f=>/\.txt(?:\.resource)?$/.test(f)).map(f=>`${run}/${f}`));
}
fs.writeFileSync(`${out}/all-measurements.csv`,csv(Object.keys(rows[0]),rows));
fs.writeFileSync(`${out}/equal-work-esbuild-rss.csv`,csv(Object.keys(rss[0]),rss));
execFileSync('tar',['-czf',`${out}/raw-process-logs.tar.gz`,'-C',root,...raw]);
for(const f of ['hint-scratch-source.patch','hint-work-source.patch','hint-cold-profile.mem','hint-cold-profile.txt','hint-scratch-profile.mem','hint-scratch-profile.txt','hint-work-red.txt','hint-work-green.txt','hint-scratch-red.txt','hint-scratch-green.txt','closures-native.txt','closures-native-pinned.txt','closures-native-pinned-v2.txt','closures-guard.txt','closures-race.txt','closures-arm64.txt','closures-arm64-guard.txt','closures-bench-tests.txt'])copy(f);
for(const f of ['stats.mjs','focused.mjs','focused-report.mjs','hint-work-run.mjs','hint-scratch-run.mjs','hint-scratch-esbuild.mjs','export-closures.mjs'])copy(f,`scripts/${f}`);
fs.writeFileSync(`${out}/metadata.json`,JSON.stringify({main:'731e95ff2cda7309eaf6d956f1417066bf7f1b69',priorPR:'1d04b458fa512265e2b93711f5c355435a29f33e',candidate:'priorPR plus hint-scratch-source.patch',rejected:'priorPR plus hint-work-source.patch',go:execFileSync('go',['version'],{encoding:'utf8'}).trim(),procs:8,GOGC:100,binaryHashes:'Each run identity.json contains every frozen binary hash.',builds:'go test -c [-tags wago_guardpage] -o OUTPUT . in each checkout bench directory.'},null,2)+'\n');
const files=fs.readdirSync(out,{recursive:true}).filter(f=>f!=='SHA256SUMS'&&fs.statSync(path.join(out,f)).isFile()).sort();
fs.writeFileSync(`${out}/SHA256SUMS`,files.map(f=>`${createHash('sha256').update(fs.readFileSync(path.join(out,f))).digest('hex')}  ${f}`).join('\n')+'\n');
console.log({files:files.length,metrics:rows.length,rss:rss.length});
