import fs from 'node:fs';
import path from 'node:path';
import {execFileSync} from 'node:child_process';
import {createHash} from 'node:crypto';
import {median,rankP} from './stats.mjs';
const repo='/home/jtenner/Projects/wago',root=`${repo}/.tmp/pr564-memory/final731`,parent=`${repo}/.tmp/pr564-memory`,out=`${repo}/bench/results/pr564-main731-checkpoint`;
fs.mkdirSync(out,{recursive:true});
const copy=(source,target=source)=>{fs.mkdirSync(path.dirname(`${out}/${target}`),{recursive:true});fs.copyFileSync(`${root}/${source}`,`${out}/${target}`);};
const csv=(heads,rows)=>heads.join(',')+'\n'+rows.map(r=>heads.map(h=>{const s=r[h]==null?'':String(r[h]);return /[,"\n]/.test(s)?'"'+s.replaceAll('"','""')+'"':s;}).join(',')).join('\n')+'\n';
const status=fs.readFileSync(`${root}/full/status.jsonl`,'utf8').trim().split('\n').map(JSON.parse);
if(status.length!==1020||status.some(r=>r.code!==0))throw Error('Incomplete full run');
const metrics=JSON.parse(fs.readFileSync(`${root}/full/comparison.json`));
if(metrics.some(r=>r.status||r.baseSamples!==6||r.candidateSamples!==6))throw Error('Unpaired full metrics');
for(const f of fs.readdirSync(`${root}/rendered`))copy(`rendered/${f}`,f);
for(const f of ['metadata.json','size.json','audit-compact-summary.json','analysis-checks.txt','host.jsonl'])copy(f);
for(const f of ['status.jsonl','binary-identity.json','comparison.json','comparison.tsv','summary.json','regression-screen.json','base-explicit.bench','final-explicit.bench','base-signals.bench','final-signals.bench'])copy(`full/${f}`);
const rss=[],memory=[],raw=fs.readdirSync(`${root}/full`).filter(f=>/\.txt(?:\.resource)?$/.test(f)).map(f=>`full/${f}`);
for(const mode of ['explicit','signals'])for(const suffix of ['memory','instantiate','rss']) {
 const run=`fixed-${suffix}-${mode}`,dir=`${root}/${run}`;
 const identity=JSON.parse(fs.readFileSync(`${dir}/identity.json`));
 const comp=JSON.parse(fs.readFileSync(`${dir}/comparison.json`));
 for(const r of comp)memory.push({run,bounds:mode,benchmark:r.name,unit:r.unit,main_median:r.base,after_median:r.candidate,change_percent:r.delta,p:r.p,samples_each:r.baseSamples,benchtime:identity.benchtime,note:identity.benchtime==='1x'?'memory check, not a timing claim':'equal work'});
 const runs=fs.readFileSync(`${dir}/status.jsonl`,'utf8').trim().split('\n').map(JSON.parse);
 for(const name of new Set(runs.map(r=>r.name))) {
  const values=label=>runs.filter(r=>r.label===label&&r.name===name).map(r=>+fs.readFileSync(`${dir}/${r.key}.txt.resource`,'utf8').match(/Maximum resident set size \(kbytes\):\s*(\d+)/)[1]);
  const a=values('base'),b=values('final');if(a.length!==6||b.length!==6)throw Error('RSS samples');
  rss.push({run,bounds:mode,benchmark:name,main_median_KiB:median(a),after_median_KiB:median(b),change_percent:100*(median(b)/median(a)-1),p:rankP(a,b),samples_each:6,benchtime:identity.benchtime});
 }
 for(const f of ['identity.json','comparison.json','status.jsonl','base.bench','final.bench'])copy(`${run}/${f}`);
 raw.push(...fs.readdirSync(dir).filter(f=>/\.txt(?:\.resource)?$/.test(f)).map(f=>`${run}/${f}`));
}
// This frozen 1d04 checkpoint ended before longer timing confirmation.
// Carry all warnings into the b4f236 final-head confirmation instead.
fs.writeFileSync(`${out}/fixed-work-memory-main-vs-after.csv`,csv(['run','bounds','benchmark','unit','main_median','after_median','change_percent','p','samples_each','benchtime','note'],memory));
fs.writeFileSync(`${out}/equal-work-rss-main-vs-after.csv`,csv(['run','bounds','benchmark','main_median_KiB','after_median_KiB','change_percent','p','samples_each','benchtime'],rss));
const peaks=status.map(r=>({bounds:r.mode,label:r.label,group:r.top,sample:r.sample,peak_rss_KiB:+fs.readFileSync(`${root}/full/${r.key}.txt.resource`,'utf8').match(/Maximum resident set size \(kbytes\):\s*(\d+)/)[1]}));
fs.writeFileSync(`${out}/full-process-rss.csv`,csv(['bounds','label','group','sample','peak_rss_KiB'],peaks));
const skipped=[];
for(const r of status.filter(r=>r.sample===0)) {
 const lines=fs.readFileSync(`${root}/full/${r.key}.txt`,'utf8').split('\n');
 for(let i=0;i<lines.length;i++) {
  const m=lines[i].match(/--- SKIP: (Benchmark\S+)/);if(!m)continue;
  skipped.push({bounds:r.mode,label:r.label,group:r.top,benchmark:m[1],context:lines.slice(Math.max(0,i-3),i+1).join('\n'),raw_log:`full/${r.key}.txt`});
 }
}
fs.writeFileSync(`${out}/skipped-benchmarks.csv`,csv(['bounds','label','group','benchmark','context','raw_log'],skipped));
execFileSync('tar',['-czf',`${out}/raw-process-logs.tar.gz`,'-C',root,...raw]);
for(const f of ['full.mjs','build.mjs','analyze.mjs','render.mjs','stats.mjs','focused.mjs','focused-report.mjs','fixed.mjs','repeat.mjs','size.mjs','audit.mjs','host.mjs','export-checkpoint.mjs','extra-checks.mjs','analysis-checks.mjs','analysis-fixture.bench','listed-cases.json'])copy(f,`scripts/${f}`);
fs.copyFileSync(`${parent}/code_audit_test.go`,`${out}/scripts/code_audit_test.go.txt`);
for(const f of ['integrated-full.txt','integrated-full-pinned.txt','integrated-guard.txt','integrated-race.txt','integrated-backend.txt','integrated-bench-tests.txt','integrated-arm64.txt','integrated-arm64-v2.txt','integrated-arm64-v3.txt','integrated-arm64-signals-v3.txt','integrated-self-clean-env.txt','integrated-cli-commands.txt','main-wine-bootstrap.txt','final-windows-arm64-731-build.txt','final731-tiny-smoke.json','ci-1d04-green.json']) {
 fs.mkdirSync(`${out}/checks`,{recursive:true});fs.copyFileSync(`${parent}/${f}`,`${out}/checks/${f}`);
}
for(const f of fs.readdirSync(root).filter(f=>f.startsWith('cli-')&&f.endsWith('.txt')))copy(f,`checks/${f}`);
for(const f of ['extra-checks.json','standard-tiny-smoke.json','standard-tiny-build.txt','standard-tiny-strip.txt','main-wine-installer.txt','final-wine-installer.txt'])copy(f,`checks/${f}`);
for(const f of fs.readdirSync(`${root}/size`).filter(f=>f.endsWith('.smoke.json')||f.endsWith('.smoke.txt')))copy(`size/${f}`);
const hash=s=>createHash('sha256').update(s).digest('hex');
const files=fs.readdirSync(out,{recursive:true}).filter(f=>f!=='SHA256SUMS'&&fs.statSync(path.join(out,f)).isFile()).sort();
fs.writeFileSync(`${out}/SHA256SUMS`,files.map(f=>`${hash(fs.readFileSync(path.join(out,f)))}  ${f}`).join('\n')+'\n');
console.log({files:files.length,metrics:metrics.length,timings:metrics.filter(r=>r.unit==='ns/op').length,memoryMetrics:memory.length,rss:rss.length,processes:status.length});
