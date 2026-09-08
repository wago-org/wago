import fs from 'node:fs';
import path from 'node:path';
import {gzipSync} from 'node:zlib';
import {createHash} from 'node:crypto';
import {execFileSync} from 'node:child_process';
const root='/tmp/wago-pr564-regressions-zHTLww',repo='/home/jtenner/Projects/wago';
const out=`${repo}/bench/results/pr564-regression-fixes`;
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
const runs=['recheck','inline','half','global-one','final','compiler-final','arena-census','density-census','bounded-census','confirm-explicit','confirm-signals','remaining-triples','remaining-one','result-layout','result-layout-signals','fixed-work'];
for(const run of runs) {
 const dir=`${root}/${run}`;if(!fs.existsSync(dir))continue;
 const names=fs.readdirSync(dir).sort();
 for(const name of names) {
  if(name.endsWith('.bench')||name==='status.jsonl')copy(`${dir}/${name}`,`${out}/${run}/${name}.gz`,true);
  if(name==='summary.json'||name==='benchstat.txt'||name==='rss-summary.json')copy(`${dir}/${name}`,`${out}/${run}/${name}`);
 }
 archive(dir,names.filter(s=>s.endsWith('.txt')&&s!=='benchstat.txt'||s.endsWith('.status.json')||s.endsWith('.txt.resource')),`${out}/${run}/raw.tar.gz`);
}
const fullNames=fs.readdirSync(`${root}/full`).sort();
for(const name of fullNames) {
 if(name.endsWith('.bench')||name==='status.jsonl'||name==='comparison.json'||name==='comparison.tsv')copy(`${root}/full/${name}`,`${out}/full/${name}.gz`,true);
 if(['summary.json','regression-screen.json','confirmation-screen.json','qualification.json','confirmed-timing.json','confirmed-timing.md','resource-increases.tsv','rss-peaks.json'].includes(name))copy(`${root}/full/${name}`,`${out}/full/${name}`);
 if(name.startsWith('benchstat-'))copy(`${root}/full/${name}`,`${out}/full/${name}.gz`,true);
}
archive(`${root}/full`,fullNames.filter(s=>(s.startsWith('base-')||s.startsWith('final-'))&&(s.endsWith('.txt')||s.endsWith('.txt.resource'))),`${out}/full/raw.tar.gz`);
for(const name of fs.readdirSync(root).sort()) {
 if(name.endsWith('.mjs'))copy(`${root}/${name}`,`${out}/scripts/${name}`);
 if(/^(test-|build-result-layout)/.test(name)&&/\.txt(?:\.|$)/.test(name))copy(`${root}/${name}`,`${out}/tests/${name}.gz`,true);
 if(name.startsWith('size')&&(name.endsWith('.json')||name.endsWith('.txt')))copy(`${root}/${name}`,`${out}/size/${name}`);
}
copy(`${root}/host.jsonl`,`${out}/host.jsonl.gz`,true);
const binaries={};
for(const dir of [root,'/tmp/wago-pr564-HbN430'])for(const name of fs.readdirSync(dir).sort()) {
 if(name.endsWith('.test')&&(dir===root||/^(base-|candidate-)/.test(name))) {
  const p=`${dir}/${name}`;binaries[p]={bytes:fs.statSync(p).size,sha256:sha(p),buildInfo:execFileSync('go',['version','-m',p],{encoding:'utf8'})};
 }
}
const git=(...args)=>execFileSync('git',args,{cwd:repo,encoding:'utf8'}).trim();
const manifest={generated:new Date().toISOString(),baseline:'a07de0973191efab1d32677eff527952c7f9cdd2',priorProduction:'16124d7639983fca0243f77486e32dc5ac74ab57',fixedProduction:'d006d3d105dccd8a9f2ae6e0d2442a99201c9ba0',reportHead:git('rev-parse','HEAD'),productionTree:git('rev-parse','d006d3d10^{tree}'),binaries,go:JSON.parse(execFileSync('go',['env','-json','GOOS','GOARCH','GOVERSION','GOAMD64','CGO_ENABLED','GOEXPERIMENT'],{encoding:'utf8'})),benchstatSha256:sha('/tmp/wago-pr564-HbN430/bin/benchstat'),notes:['Main and prior candidate benchmark binaries are preserved from the initial qualification.','Final benchmark binaries were built from production source identical to d006d3d10 before that commit was created.','Experimental labels half, density and bounded are not final results.','Scripts retain exact original local paths; adapt paths to rerun on another host.','No native ARM64 timing measurements.']};
manifest.releaseBinaries={};
for(const name of ['manager','runtime-standard','runtime-minimal','runtime-minimal-tiny']) {
 const p=`${root}/size-${name}`;
 manifest.releaseBinaries[name]={bytes:fs.statSync(p).size,sha256:sha(p)};
}
fs.writeFileSync(`${out}/manifest.json`,JSON.stringify(manifest,null,2)+'\n');
console.log('Exported benchmark evidence; write and verify SHA256SUMS after the final report edit.');
