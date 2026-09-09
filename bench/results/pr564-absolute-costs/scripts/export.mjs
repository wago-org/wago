import fs from 'node:fs';
import path from 'node:path';
import {gzipSync} from 'node:zlib';
import {createHash} from 'node:crypto';
import {execFileSync} from 'node:child_process';
const repo='/home/jtenner/Projects/wago',root=`${repo}/.tmp/pr564-absolute-costs`,out=`${repo}/bench/results/pr564-absolute-costs`;
const sha=p=>createHash('sha256').update(fs.readFileSync(p)).digest('hex');
function copy(src,dst,gzip=false) {
 fs.mkdirSync(path.dirname(dst),{recursive:true});
 const data=fs.readFileSync(src);fs.writeFileSync(dst,gzip?gzipSync(data,{level:9}):data);
}
function archive(dir,names,dst) {
 if(!names.length)return;
 fs.mkdirSync(path.dirname(dst),{recursive:true});
 execFileSync('tar',['-czf',dst,'-C',dir,'--',...names]);
}
const qualification=JSON.parse(fs.readFileSync(`${root}/full/qualification.json`));
if(qualification.unpaired.length||qualification.badSampleCounts.length||qualification.processSummary.some(r=>r.failed))throw Error('Full qualification has gaps');
for(const run of fs.readdirSync(root).filter(s=>fs.existsSync(`${root}/${s}/identity.json`))) {
 const dir=`${root}/${run}`,names=fs.readdirSync(dir).sort();
 for(const name of names) {
  if(name.endsWith('.bench')||name==='status.jsonl')copy(`${dir}/${name}`,`${out}/${run}/${name}.gz`,true);
  if(['summary.json','comparison.json','benchstat.txt','identity.json','rss-summary.json'].includes(name))copy(`${dir}/${name}`,`${out}/${run}/${name}`);
 }
 archive(dir,names.filter(s=>s.endsWith('.txt')&&s!=='benchstat.txt'||s.includes('.interrupted-')||s.endsWith('.resource')),`${out}/${run}/raw.tar.gz`);
}
const fullNames=fs.readdirSync(`${root}/full`).sort();
for(const name of fullNames) {
 if(name.endsWith('.bench')||['status.jsonl','comparison.json','comparison.tsv','recovery.jsonl'].includes(name)||name.startsWith('benchstat-'))copy(`${root}/full/${name}`,`${out}/full/${name}.gz`,true);
 if(['summary.json','regression-screen.json','confirmation-screen.json','qualification.json','confirmed-timing.json','resource-increases.tsv','binary-identity.json'].includes(name))copy(`${root}/full/${name}`,`${out}/full/${name}`);
}
archive(`${root}/full`,fullNames.filter(s=>(s.startsWith('base-')||s.startsWith('final-'))&&(s.endsWith('.txt')||s.endsWith('.resource')||s.includes('.interrupted-'))),`${out}/full/raw.tar.gz`);
for(const name of fs.readdirSync(`${root}/rendered`).sort())copy(`${root}/rendered/${name}`,`${out}/${name}`);
for(const name of fs.readdirSync(root).sort()) {
 if(name.endsWith('.mjs')||['code_audit_test.go','before-overlay.json','audit-base.json','audit-final.json','analysis-fixture.bench'].includes(name))copy(`${root}/${name}`,`${out}/scripts/${name}`);
 if(name.startsWith('test-'))copy(`${root}/${name}`,`${out}/tests/${name}.gz`,true);
 if(name.startsWith('audit-')&&(name.endsWith('.txt')||name.endsWith('.status.json')||name==='audit-summary.json'||name==='audit-all-summary.json'))copy(`${root}/${name}`,`${out}/code-audit/${name}`);
 if(name.startsWith('micro-')||name.startsWith('profile-'))copy(`${root}/${name}`,`${out}/profiles/${name}`);
 if(name.endsWith('.cpu')||name.endsWith('.mem'))copy(`${root}/${name}`,`${out}/profiles/${name}.gz`,true);
 if(['metadata.json','listed-cases.json','size.json','perf-access.txt','resource-confirmation-screen.json','timing-diagnostic-screen.json','pipeline-complete.json','verification.json'].includes(name))copy(`${root}/${name}`,`${out}/${name}`);
}
if(fs.existsSync(`${root}/size`))for(const name of fs.readdirSync(`${root}/size`).filter(s=>s.endsWith('.txt')||s.endsWith('.json')))copy(`${root}/size/${name}`,`${out}/size/${name}`);
copy(`${root}/host.jsonl`,`${out}/host.jsonl.gz`,true);
copy(`${root}/run.txt`,`${out}/run.txt.gz`,true);
const metadata=JSON.parse(fs.readFileSync(`${root}/metadata.json`));
const binaries={};
for(const label of ['base','before','final'])for(const mode of ['explicit','signals']) {
 const p=`${root}/${label}-${mode}.test`;binaries[`${label}-${mode}`]={bytes:fs.statSync(p).size,sha256:sha(p),buildInfo:metadata.binaries[`${label}-${mode}`]};
}
const fixtureFiles=execFileSync('rg',['--files','bench/corpus','tests/fixtures','tests/corpora','-g','*.wasm','-g','*.json'],{cwd:repo,encoding:'utf8'}).trim().split('\n').sort();
const fixtures=Object.fromEntries(fixtureFiles.map(p=>[p,sha(`${repo}/${p}`)]));
const manifest={generated:new Date().toISOString(),baseline:metadata.main,priorProduction:metadata.prior,fixedProduction:metadata.after,binaries,benchstatSha256:sha(`${root}/bin/benchstat`),timeSha256:sha(`${root}/time-tool/usr/bin/time`),fixtures,notes:['Before is freshly measured pinned main; no pre-reboot samples are included.','The prior label is d006d3d10 and is only a diagnostic comparison, never the main baseline.','All timed benchmark binaries exclude the diagnostic code-audit overlay.','The benchmark harness differs from main only by an unused opt-in optimization-ablation benchmark.','Scripts retain exact local paths; adapt paths to rerun elsewhere.','No native ARM64 timing measurements.']};
fs.writeFileSync(`${out}/manifest.json`,JSON.stringify(manifest,null,2)+'\n');
console.log('Exported evidence. Finish README, then generate and verify SHA256SUMS.');
