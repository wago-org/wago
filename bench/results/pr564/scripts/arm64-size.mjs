import fs from 'node:fs';
import {spawn} from 'node:child_process';
const out='/tmp/wago-pr564-HbN430',dir=`${out}/arm64-size`;
fs.mkdirSync(dir,{recursive:true});
const rows=new Map();
for(const label of ['base','candidate']) {
  const log=`${dir}/${label}.txt`;
  if(!process.argv.includes('--parse')) {
  const fd=fs.openSync(log,'w');
  const args=[`${out}/${label}-arm64-bench.test`,'-test.run=^$','-test.bench=^BenchmarkCompileFull$','-test.benchtime=1x','-test.count=1','-test.benchmem','-test.v','-test.timeout=10m','-wago.bench.isa'];
  console.log(`${new Date().toISOString()} ARM64 ${label} code-size census; emulated times are not performance results`);
  const child=spawn(`${out}/qemu/usr/bin/qemu-aarch64`,args,{cwd:'/home/jtenner/Projects/wago/bench',env:{...process.env,GOMAXPROCS:'8',GOGC:'100',WAGO_BOUNDS:'explicit'},stdio:['ignore',fd,fd]});
  const code=await new Promise(resolve=>child.on('exit',(c,s)=>resolve(c??s)));fs.closeSync(fd);
  fs.writeFileSync(`${dir}/${label}.status.json`,JSON.stringify({label,code,args,interpretation:'code sizes only; QEMU elapsed times are excluded'},null,2)+'\n');
  if(code!==0){console.log({label,code});process.exit(1);}
  }
  for(const line of fs.readFileSync(log,'utf8').split('\n')) {
    const m=line.match(/^(BenchmarkCompileFull\/\S+)-\d+\s+\d+\s+.*?\s(\d+(?:\.\d+)?) code-B/);
    if(!m)continue;
    if(!rows.has(m[1]))rows.set(m[1],{});
    rows.get(m[1])[label]=Number(m[2]);
  }
}
const result=[...rows].map(([name,r])=>({name,...r,delta:r.base!==undefined&&r.candidate!==undefined?r.candidate-r.base:null,percent:r.base>0?100*(r.candidate/r.base-1):r.base===r.candidate?0:null}));
fs.writeFileSync(`${out}/arm64-code-size.tsv`,'benchmark\tbase_bytes\tcandidate_bytes\tdelta_bytes\tdelta_percent\n'+result.map(r=>[r.name,r.base,r.candidate,r.delta,r.percent].map(v=>v??'').join('\t')).join('\n')+'\n');
console.log(JSON.stringify({rows:result.length,growth:result.filter(r=>r.delta>0),missing:result.filter(r=>r.delta===null)},null,2));
