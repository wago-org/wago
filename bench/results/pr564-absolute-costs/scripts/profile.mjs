import fs from 'node:fs';
import {spawn} from 'node:child_process';
const root='/home/jtenner/Projects/wago/.tmp/pr564-absolute-costs';
for(const [name,mode,pattern,time,count] of [
 ['ruby-instantiate','signals','^BenchmarkPluginInstantiate$/^ruby$','5s','1'],
 ['coremark','explicit','^BenchmarkExec$/^coremark\\.coremark_run$','5s','1'],
 ['wasm3','signals','^BenchmarkPluginExec$/^wasm3$','1x','40'],
]) {
 const fd=fs.openSync(`${root}/profile-${name}.txt`,'w'),start=Date.now();
 const bin=`${root}/before-${mode}.test`;
 const args=['-test.run=^$',`-test.bench=${pattern}`,`-test.benchtime=${time}`,`-test.count=${count}`,'-test.benchmem',`-test.cpuprofile=${root}/${name}.cpu`,`-test.memprofile=${root}/${name}.mem`];
 const child=spawn(bin,args,{cwd:'/home/jtenner/Projects/wago/bench',env:{...process.env,GOMAXPROCS:'8',GOGC:'100',WAGO_BOUNDS:mode},stdio:['ignore',fd,fd]});
 const code=await new Promise(r=>child.on('exit',(c,s)=>r(c??s)));fs.closeSync(fd);
 const row={name,bin,args,code,start:new Date(start).toISOString(),seconds:(Date.now()-start)/1000};
 fs.writeFileSync(`${root}/profile-${name}.status.json`,JSON.stringify(row,null,2));console.log(row);
 if(code!==0)process.exit(1);
}
