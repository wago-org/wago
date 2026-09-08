import fs from 'node:fs';
import {spawn} from 'node:child_process';
const root='/tmp/wago-pr564-regressions-zHTLww';
const old='/tmp/wago-pr564-HbN430';
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
for(let i=0;i<names.length;i++) {
 const [mode,name]=names[i];
 for(let sample=0;sample<samples;sample++)for(const label of sample%2?[...labels].reverse():labels) {
  const key=`${label}-${i}-${sample}`,path=`${dir}/${key}.txt`;
  if(fs.existsSync(`${path}.ok`))continue;
  const bin=label==='base'?`${old}/base-${mode}.test`:label==='before'?`${old}/candidate-${mode}.test`:`${root}/${label}-${mode}.test`;
  const args=['-test.run=^$',`-test.bench=${name.split('/').map(s=>`^${escaped(s)}$`).join('/')}`,'-test.benchmem',`-test.benchtime=${process.env.BENCHTIME||'300ms'}`,'-test.count=1','-test.timeout=10m','-wago.bench.isa'];
  const fd=fs.openSync(path,'w'),start=Date.now();
  const child=spawn(bin,args,{cwd:'/home/jtenner/Projects/wago/bench',env:{...process.env,GOMAXPROCS:process.env.PROCS||'8',GOGC:'100',WAGO_BOUNDS:mode},stdio:['ignore',fd,fd]});
  const code=await new Promise(r=>child.on('exit',(c,s)=>r(c??s))); fs.closeSync(fd);
  fs.appendFileSync(`${dir}/status.jsonl`,JSON.stringify({key,mode,name,label,sample,bin,args,code,start:new Date(start).toISOString(),end:new Date().toISOString(),seconds:(Date.now()-start)/1000})+'\n');
  if(code!==0)throw Error(`${key}: ${code}`);
  fs.writeFileSync(`${path}.ok`,'');
 }
 console.log(`${new Date().toISOString()} completed ${name}`);
}
for(const label of labels) {
 const files=fs.readdirSync(dir).filter(s=>s.startsWith(label+'-')&&s.endsWith('.txt')).sort();
 fs.writeFileSync(`${dir}/${label}.bench`,files.map(s=>fs.readFileSync(`${dir}/${s}`)).join('\n'));
}
