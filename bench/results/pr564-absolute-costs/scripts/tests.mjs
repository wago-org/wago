import fs from 'node:fs';
import {spawn} from 'node:child_process';
const root='/home/jtenner/Projects/wago/.tmp/pr564-absolute-costs',repo='/home/jtenner/Projects/wago';
const ref='9d36019973201a19f9c9ebb0f10828b2fe2374aa';
const env={...process.env,GOMAXPROCS:'8',PATH:`${repo}/.tools/wabt-1.0.41-linux-x64/bin:${process.env.PATH}`,WAGO_SPEC_INTERPRETER:`${repo}/.tools/spec-interpreter-${ref}/wasm`,WAGO_SPEC_INTERPRETER_REVISION:ref};
delete env.GOROOT;
const focused='TestFuncSigIntRegABI|TestReferenceStore|TestCompiledStructural|TestRuntimeRejectsForcedHash|TestRegABI|TestFuncRef|TestTailCall|Test.*Instantiate|Test.*HostReentry';
const tasks=[
 ['native','go',['test','./src/...','-count=1','-timeout=20m']],
 ['guard','go',['test','-tags','wago_guardpage','./src/...','-count=1','-timeout=20m']],
 ['race','go',['test','-race','./src/wago','-run',focused,'-count=1','-timeout=10m']],
 ['bench','go',['test','./...','-count=1'],`${repo}/bench`],
 ['go122','/home/jtenner/.local/share/mise/installs/go/1.22.12/bin/go',['test','./src/wago','-run',focused,'-count=1']],
];
for(const [name,cmd,args,cwd=repo] of tasks) {
 const fd=fs.openSync(`${root}/test-${name}.txt`,'w'),start=Date.now();
 const child=spawn(cmd,args,{cwd,env,stdio:['ignore',fd,fd]});
 const code=await new Promise(r=>child.on('exit',(c,s)=>r(c??s)));fs.closeSync(fd);
 const status={name,cmd,args,cwd,code,start:new Date(start).toISOString(),seconds:(Date.now()-start)/1000};
 fs.writeFileSync(`${root}/test-${name}.status.json`,JSON.stringify(status,null,2));console.log(status);
 if(code!==0)process.exitCode=1;
}
