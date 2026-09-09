import fs from 'node:fs';
import {spawn} from 'node:child_process';
const repo='/home/jtenner/Projects/wago',root=`${repo}/.tmp/pr564-absolute-costs`;
async function run(name,cmd,args,cwd=repo,extra={}) {
 const fd=fs.openSync(`${root}/${name}.txt`,'w'),start=Date.now();
 const child=spawn(cmd,args,{cwd,env:{...process.env,GOMAXPROCS:'8',GOGC:'100',...extra},stdio:['ignore',fd,fd]});
 const code=await new Promise(r=>child.on('exit',(c,s)=>r(c??s)));fs.closeSync(fd);
 const status={name,cmd,args,cwd,code,start:new Date(start).toISOString(),seconds:(Date.now()-start)/1000};
 fs.writeFileSync(`${root}/${name}.status.json`,JSON.stringify(status,null,2));console.log(status);
 if(code!==0)throw Error(`${name}: ${code}`);
}
for(const label of ['base','final'])for(const mode of ['explicit','signals']) {
 const cwd=label==='base'?`${root}/main/bench`:`${repo}/bench`;
 const overlay=`${root}/audit-${label}.json`;
 fs.writeFileSync(overlay,JSON.stringify({Replace:{[`${cwd}/absolute_code_audit_test.go`]:`${root}/code_audit_test.go`}}));
 const args=['test',`-overlay=${overlay}`,...(mode==='signals'?['-tags','wago_guardpage']:[]),'-run','^TestAbsoluteCostCodeAudit$','-count=1','-v','-wago.bench.isa'];
 await run(`audit-${label}-${mode}`,'go',args,cwd,{WAGO_BOUNDS:mode});
}
await run('micro-before','go',['test',`-overlay=${root}/before-overlay.json`,'./src/wago','-run','^$','-bench','^BenchmarkFuncSigIntRegABI$','-benchmem','-count=6','-benchtime=500ms']);
await run('micro-final','go',['test','./src/wago','-run','^$','-bench','^BenchmarkFuncSigIntRegABI$','-benchmem','-count=6','-benchtime=500ms']);
await run('profile-runner','node',[`${root}/profile.mjs`]);
for(const [name,mode] of [['ruby-instantiate','signals'],['coremark','explicit'],['wasm3','signals']]) {
 await run(`profile-${name}-cpu-top`,'go',['tool','pprof','-top',`${root}/before-${mode}.test`,`${root}/${name}.cpu`]);
 await run(`profile-${name}-alloc-top`,'go',['tool','pprof','-top','-alloc_space',`${root}/before-${mode}.test`,`${root}/${name}.mem`]);
}
await run('profile-ruby-abi-source','go',['tool','pprof','-list=funcSigIntRegABI','-alloc_space',`${root}/before-signals.test`,`${root}/ruby-instantiate.mem`]);
