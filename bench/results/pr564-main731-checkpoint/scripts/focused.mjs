import fs from 'node:fs';
import {spawn} from 'node:child_process';
import {createHash} from 'node:crypto';
const root='/home/jtenner/Projects/wago/.tmp/pr564-memory/final731';
const old='/home/jtenner/Projects/wago/.tmp/pr564-memory/final731';
const run=process.argv[2]||'recheck';
const labels=process.env.LABELS?.split(',')||['base','before'];
const samples=Number(process.env.SAMPLES||6);
const names=process.env.CASES?JSON.parse(process.env.CASES):[
 ['explicit','BenchmarkExecParallel/process/swar-pack-parse.parse4'],
 ['explicit','BenchmarkExecParallel/independent/fib_iter.fib'],
 ['explicit','BenchmarkExecParallel/process/xjb-mulhi.mulhi'],
 ['explicit','BenchmarkExecGlobalGet_wago'],
 ['explicit','BenchmarkExecParallel/independent/isa_simd_i64x2.shl'],
 ['signals','BenchmarkInstantiate/zstd'],
];
const dir=`${root}/${run}`; fs.mkdirSync(dir,{recursive:true});
const escaped=s=>s.replace(/[.*+?^${}()|[\]\\]/g,'\\$&');
const binaries={};
for(const mode of new Set(names.map(r=>r[0])))for(const label of labels) {
 const file=`${root}/${label==='base'?'base':label==='before'?'before':label}-${mode}.test`;
 binaries[file]=createHash('sha256').update(fs.readFileSync(file)).digest('hex');
}
const identity={names,labels,samples,benchtime:process.env.BENCHTIME||'300ms',procs:process.env.PROCS||'8',binaries};
if(process.env.RESOURCE_LOGS==='1')identity.resourceLogs=true;
const identityFile=`${dir}/identity.json`;
if(fs.existsSync(identityFile)&&JSON.stringify(JSON.parse(fs.readFileSync(identityFile)))!==JSON.stringify(identity))throw Error('Focused run identity changed');
fs.writeFileSync(identityFile,JSON.stringify(identity,null,2));
const boot=fs.readFileSync('/proc/sys/kernel/random/boot_id','utf8').trim();
const done=new Set(fs.existsSync(`${dir}/status.jsonl`)?fs.readFileSync(`${dir}/status.jsonl`,'utf8').trim().split('\n').filter(Boolean).map(JSON.parse).filter(r=>r.code===0).map(r=>r.key):[]);
for(let i=0;i<names.length;i++) {
 const [mode,name]=names[i];
 for(let sample=0;sample<samples;sample++)for(const label of sample%2?[...labels].reverse():labels) {
  const key=`${label}-${i}-${sample}`,path=`${dir}/${key}.txt`;
  if(done.has(key))continue;
  if(fs.existsSync(path)) {
   const suffix=`.interrupted-${Date.now()}`;
   fs.renameSync(path,path+suffix);
   if(fs.existsSync(`${path}.resource`))fs.renameSync(`${path}.resource`,`${path}.resource${suffix}`);
  }
  const bin=label==='base'?`${old}/base-${mode}.test`:label==='before'?`${root}/before-${mode}.test`:`${root}/${label}-${mode}.test`;
  const args=['-test.run=^$',`-test.bench=${name.split('/').map(s=>`^${escaped(s)}$`).join('/')}`,'-test.benchmem',`-test.benchtime=${process.env.BENCHTIME||'300ms'}`,'-test.count=1','-test.timeout=10m','-wago.bench.isa'];
  const fd=fs.openSync(path,'w'),start=Date.now();
  const child=spawn(identity.resourceLogs?`${root}/time-tool/usr/bin/time`:bin,identity.resourceLogs?['-v','-o',`${path}.resource`,bin,...args]:args,{cwd:'/home/jtenner/Projects/wago/bench',env:{...process.env,GOMAXPROCS:process.env.PROCS||'8',GOGC:'100',WAGO_BOUNDS:mode},stdio:['ignore',fd,fd]});
  const code=await new Promise(r=>child.on('exit',(c,s)=>r(c??s))); fs.fsyncSync(fd);fs.closeSync(fd);
  const statusFD=fs.openSync(`${dir}/status.jsonl`,'a');
  fs.writeSync(statusFD,JSON.stringify({key,mode,name,label,sample,bin,args,code,boot,start:new Date(start).toISOString(),end:new Date().toISOString(),seconds:(Date.now()-start)/1000})+'\n');
  fs.fsyncSync(statusFD);fs.closeSync(statusFD);
  if(code!==0)throw Error(`${key}: ${code}`);
  done.add(key);
  fs.writeFileSync(`${path}.ok`,'');
 }
 console.log(`${new Date().toISOString()} completed ${name}`);
}
for(const label of labels) {
 const files=fs.readdirSync(dir).filter(s=>s.startsWith(label+'-')&&s.endsWith('.txt')).sort();
 fs.writeFileSync(`${dir}/${label}.bench`,files.map(s=>fs.readFileSync(`${dir}/${s}`)).join('\n'));
}
