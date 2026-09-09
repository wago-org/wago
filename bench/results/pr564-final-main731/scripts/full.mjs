import fs from 'node:fs';
import {spawn,execFileSync} from 'node:child_process';
import {createHash} from 'node:crypto';
const root='/home/jtenner/Projects/wago/.tmp/pr564-memory/final-closures',old='/home/jtenner/Projects/wago/.tmp/pr564-memory/final-closures';
const cwd='/home/jtenner/Projects/wago/bench',dir=`${root}/full`;
fs.mkdirSync(dir,{recursive:true});
const bin=(label,mode)=>label==='base'?`${old}/base-${mode}.test`:`${root}/final-${mode}.test`;
const priority=['BenchmarkPluginInstantiate','BenchmarkPluginExec','BenchmarkExec','BenchmarkExecParallel','BenchmarkCompileFull','BenchmarkCompileCompact'];
const listed=mode=>execFileSync(bin('final',mode),['-test.list=Benchmark'],{cwd,encoding:'utf8'}).split('\n').filter(s=>s.startsWith('Benchmark')).sort((a,b)=>(priority.includes(a)?priority.indexOf(a):99)-(priority.includes(b)?priority.indexOf(b):99));
const fingerprints={};
for(const mode of ['explicit','signals'])for(const label of ['base','final'])fingerprints[`${label}-${mode}`]=createHash('sha256').update(fs.readFileSync(bin(label,mode))).digest('hex');
const identityPath=`${dir}/binary-identity.json`;
if(fs.existsSync(identityPath)&&JSON.stringify(JSON.parse(fs.readFileSync(identityPath)))!==JSON.stringify(fingerprints))throw Error('Benchmark binaries changed; use a new result directory');
fs.writeFileSync(identityPath,JSON.stringify(fingerprints));
const boot=fs.readFileSync('/proc/sys/kernel/random/boot_id','utf8').trim();
const done=new Set(fs.existsSync(`${dir}/status.jsonl`)?fs.readFileSync(`${dir}/status.jsonl`,'utf8').trim().split('\n').filter(Boolean).map(JSON.parse).filter(r=>r.code===0).map(r=>r.key):[]);
const samples=6;
for(const mode of ['signals','explicit'])for(const top of listed(mode)) {
 for(let sample=0;sample<samples;sample++)for(const label of sample%2?['final','base']:['base','final']) {
  const key=`${label}-${mode}-${top}-${sample}`;
  if(done.has(key))continue;
  while(fs.existsSync(`${root}/pause.json`)) {
   fs.writeFileSync(`${dir}/progress.json`,JSON.stringify({key,paused:true,completed:done.size}));
   await new Promise(resolve=>setTimeout(resolve,2000));
  }
  const path=`${dir}/${key}.txt`;
  if(fs.existsSync(path)) {
   const suffix=`.interrupted-${Date.now()}`;
   fs.renameSync(path,path+suffix);
   if(fs.existsSync(`${path}.resource`))fs.renameSync(`${path}.resource`,`${path}.resource${suffix}`);
   fs.appendFileSync(`${dir}/recovery.jsonl`,JSON.stringify({key,boot,preserved:path+suffix})+'\n');
  }
  const fd=fs.openSync(path,'w');
  const args=['-test.run=^$',`-test.bench=^${top}$`,'-test.benchmem','-test.benchtime=100ms','-test.count=1','-test.timeout=10m','-test.v','-wago.bench.isa'];
  const start=Date.now();
  fs.writeFileSync(`${dir}/progress.json`,JSON.stringify({key,start:new Date(start).toISOString(),completed:done.size}));
  const timeArgs=['-v','-o',`${path}.resource`,bin(label,mode),...args];
  const child=spawn(`${old}/time-tool/usr/bin/time`,timeArgs,{cwd,env:{...process.env,GOMAXPROCS:'8',GOGC:'100',WAGO_BOUNDS:mode},stdio:['ignore',fd,fd]});
  const code=await new Promise(r=>child.on('exit',(c,s)=>r(c??s)));fs.fsyncSync(fd);fs.closeSync(fd);
  const statusFD=fs.openSync(`${dir}/status.jsonl`,'a');
  fs.writeSync(statusFD,JSON.stringify({key,label,mode,top,sample,code,boot,start:new Date(start).toISOString(),end:new Date().toISOString(),seconds:(Date.now()-start)/1000})+'\n');
  fs.fsyncSync(statusFD);fs.closeSync(statusFD);
  if(code!==0)throw Error(`${key}: ${code}`);
  done.add(key);
 }
 console.log(`${new Date().toISOString()} ${mode}/${top}, ${done.size} completed`);
}
for(const mode of ['explicit','signals'])for(const label of ['base','final']) {
 const files=fs.readdirSync(dir).filter(s=>s.startsWith(`${label}-${mode}-`)&&s.endsWith('.txt')).sort();
 fs.writeFileSync(`${dir}/${label}-${mode}.bench`,files.map(s=>`# ${s}\n`+fs.readFileSync(`${dir}/${s}`,'utf8')).join('\n'));
}
