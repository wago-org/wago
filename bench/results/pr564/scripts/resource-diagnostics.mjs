import fs from 'node:fs';
import {spawn} from 'node:child_process';
const out='/tmp/wago-pr564-HbN430',dir=`${out}/resource-diagnostics`;
fs.mkdirSync(dir,{recursive:true});
const rows=[];
for(const mod of ['isa_call','isa_var','isa_bulk_mem','json-as'])for(const label of ['base','candidate']) {
  const file=`${dir}/${label}-${mod}.txt`,fd=fs.openSync(file,'w');
  const args=['-test.run=^$',`-test.bench=^BenchmarkCompileCompact$/^${mod}$`,'-test.benchtime=1x','-test.count=1','-test.benchmem','-test.v','-wago.bench.isa'];
  const child=spawn(`${out}/${label}-explicit.test`,args,{cwd:'/home/jtenner/Projects/wago/bench',env:{...process.env,GOMAXPROCS:'8',GOGC:'100',WAGO_EXPLAIN:'1',WAGO_BOUNDS:'explicit'},stdio:['ignore',fd,fd]});
  const code=await new Promise(resolve=>child.on('exit',(c,s)=>resolve(c??s)));fs.closeSync(fd);
  const lines=fs.readFileSync(file,'utf8').split('\n');
  const stats=lines.filter(s=>/^compile-(?:resources|node-scratch|control-scratch):|^compile:|^native-mapping:/.test(s));
  rows.push({label,mod,code,args,stats});if(code!==0)process.exitCode=1;
}
fs.writeFileSync(`${out}/resource-diagnostics.json`,JSON.stringify({note:'WAGO_EXPLAIN=1 instrumented, single iteration; structural counters only. Its timing and B/op are NOT used in the normal benchmark comparison.',rows},null,2)+'\n');
console.log(JSON.stringify(rows,null,2));
