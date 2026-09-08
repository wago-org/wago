import fs from 'node:fs';
import {spawn} from 'node:child_process';
const root='/tmp/wago-pr564-regressions-zHTLww',old='/tmp/wago-pr564-HbN430',dir=`${root}/fixed-work`;
fs.mkdirSync(dir,{recursive:true});
const labels=['base','final'];
for(let sample=0;sample<6;sample++)for(const label of sample%2?[...labels].reverse():labels) {
 const key=`${label}-${sample}`,path=`${dir}/${key}.txt`,fd=fs.openSync(path,'w');
 const bin=label==='base'?`${old}/base-explicit.test`:`${root}/final-explicit.test`;
 const args=['-v','-o',`${path}.resource`,bin,'-test.run=^$','-test.bench=^BenchmarkCompileCompact$','-test.benchmem','-test.benchtime=1x','-test.count=1','-wago.bench.isa'];
 const memory=fs.readFileSync('/proc/meminfo','utf8'),start=Date.now();
 const child=spawn(`${old}/time-tool/usr/bin/time`,args,{cwd:'/home/jtenner/Projects/wago/bench',env:{...process.env,GOMAXPROCS:'8',GOGC:'100',WAGO_BOUNDS:'explicit'},stdio:['ignore',fd,fd]});
 const code=await new Promise(r=>child.on('exit',(c,s)=>r(c??s)));fs.closeSync(fd);
 fs.appendFileSync(`${dir}/status.jsonl`,JSON.stringify({key,label,sample,args,code,start:new Date(start).toISOString(),seconds:(Date.now()-start)/1000,memory})+'\n');
 if(code!==0)throw Error(`${key}: ${code}`);
 console.log(`${key} passed`);
}
for(const label of labels) {
 const files=fs.readdirSync(dir).filter(s=>s.startsWith(label+'-')&&s.endsWith('.txt')).sort();
 fs.writeFileSync(`${dir}/${label}.bench`,files.map(s=>fs.readFileSync(`${dir}/${s}`)).join('\n'));
}
