import fs from 'node:fs';
import {spawn,execFileSync} from 'node:child_process';
const root='/tmp/wago-pr564-regressions-zHTLww',old='/tmp/wago-pr564-HbN430';
const cwd='/home/jtenner/Projects/wago/bench',dir=`${root}/full`;
fs.mkdirSync(dir,{recursive:true});
const bin=(label,mode)=>label==='base'?`${old}/base-${mode}.test`:`${root}/final-${mode}.test`;
const listed=mode=>execFileSync(bin('final',mode),['-test.list=Benchmark'],{cwd,encoding:'utf8'}).split('\n').filter(s=>s.startsWith('Benchmark'));
const done=new Set(fs.existsSync(`${dir}/status.jsonl`)?fs.readFileSync(`${dir}/status.jsonl`,'utf8').trim().split('\n').filter(Boolean).map(JSON.parse).filter(r=>r.code===0).map(r=>r.key):[]);
const samples=6;
for(const mode of ['explicit','signals'])for(const top of listed(mode)) {
 for(let sample=0;sample<samples;sample++)for(const label of sample%2?['final','base']:['base','final']) {
  const key=`${label}-${mode}-${top}-${sample}`;
  if(done.has(key))continue;
  const path=`${dir}/${key}.txt`,fd=fs.openSync(path,'w');
  const args=['-test.run=^$',`-test.bench=^${top}$`,'-test.benchmem','-test.benchtime=100ms','-test.count=1','-test.timeout=10m','-test.v','-wago.bench.isa'];
  const start=Date.now();
  fs.writeFileSync(`${dir}/progress.json`,JSON.stringify({key,start:new Date(start).toISOString(),completed:done.size}));
  const timeArgs=['-v','-o',`${path}.resource`,bin(label,mode),...args];
  const child=spawn(`${old}/time-tool/usr/bin/time`,timeArgs,{cwd,env:{...process.env,GOMAXPROCS:'8',GOGC:'100',WAGO_BOUNDS:mode},stdio:['ignore',fd,fd]});
  const code=await new Promise(r=>child.on('exit',(c,s)=>r(c??s)));fs.closeSync(fd);
  fs.appendFileSync(`${dir}/status.jsonl`,JSON.stringify({key,label,mode,top,sample,code,start:new Date(start).toISOString(),end:new Date().toISOString(),seconds:(Date.now()-start)/1000})+'\n');
  if(code!==0)throw Error(`${key}: ${code}`);
  done.add(key);
 }
 console.log(`${new Date().toISOString()} ${mode}/${top}, ${done.size} completed`);
}
for(const mode of ['explicit','signals'])for(const label of ['base','final']) {
 const files=fs.readdirSync(dir).filter(s=>s.startsWith(`${label}-${mode}-`)&&s.endsWith('.txt')).sort();
 fs.writeFileSync(`${dir}/${label}-${mode}.bench`,files.map(s=>`# ${s}\n`+fs.readFileSync(`${dir}/${s}`,'utf8')).join('\n'));
}
