import fs from 'node:fs';
import {spawn} from 'node:child_process';
const out='/tmp/wago-pr564-regressions-zHTLww', repo='/home/jtenner/Projects/wago';
const ref='9d36019973201a19f9c9ebb0f10828b2fe2374aa';
const env={...process.env,GOMAXPROCS:'8',PATH:`${repo}/.tools/wabt-1.0.41-linux-x64/bin:${process.env.PATH}`,WAGO_SPEC_INTERPRETER:`${repo}/.tools/spec-interpreter-${ref}/wasm`,WAGO_SPEC_INTERPRETER_REVISION:ref};
delete env.GOROOT;
const tasks=[
 ['native','go',['test','./src/...','-count=1','-timeout=20m']],
 ['guard','go',['test','-tags','wago_guardpage','./src/...','-count=1','-timeout=20m']],
 ['race','go',['test','-race','./src/core/compiler/...','./src/wago','-run','TestArena|TestStack|TestGlobalHint|TestModuleStack|Test.*Parallel|TestCompileWorkers|TestInstanceResultStorage|TestPreparedFunction|TestMemoryValueFactsAcrossTransfers|Test.*HostReentry|Test.*Close|Test.*Borrow','-count=1','-timeout=10m']],
 ['bench','go',['test','./...','-count=1'],`${repo}/bench`],
 ['arm64-backend','/tmp/wago-pr564-HbN430/qemu/usr/bin/qemu-aarch64',[`${out}/arm64.test`,'-test.run=TestArena|TestStack|TestSubDefault|TestHinted|TestGlobalHint|TestModuleStack|Test.*Parallel|TestCompileWorkers|TestFuncHintsSize','-test.v'],`${repo}/src/core/compiler/backend/railshot/arm64`],
 ['arm64-runtime','/tmp/wago-pr564-HbN430/qemu/usr/bin/qemu-aarch64',[`${out}/arm64-wago.test`,'-test.run=TestInstanceResultStorage|TestPreparedFunction|TestMemoryValueFactsAcrossTransfers|Test.*HostReentry','-test.v'],`${repo}/src/wago`],
 ['arm64-guard','/tmp/wago-pr564-HbN430/qemu/usr/bin/qemu-aarch64',[`${out}/arm64-wago-signals.test`,'-test.run=TestInstanceResultStorage|TestPreparedFunction|TestMemoryValueFactsAcrossTransfers|Test.*HostReentry','-test.v'],`${repo}/src/wago`],
 ['go122-runtime','/home/jtenner/.local/share/mise/installs/go/1.22.12/bin/go',['test','./src/wago','-run','TestInstanceResultStorage|TestPreparedFunction|TestMemoryValueFactsAcrossTransfers|Test.*HostReentry','-count=1']],
 ['arm64-prior-timer','/tmp/wago-pr564-HbN430/qemu/usr/bin/qemu-aarch64',['/tmp/wago-pr564-HbN430/arm64-wago-final.test','-test.run=^TestHostReentryRefreshesMemorySizeAfterNestedGrow$','-test.v'],`${repo}/src/wago`],
];
for(const [name,cmd,args,cwd=repo] of tasks) {
 if(process.argv[2]&&name!==process.argv[2])continue;
 const path=`${out}/test-${name}.txt`;
 if(fs.existsSync(path)) {fs.renameSync(path,path+'.initial');fs.renameSync(path+'.status.json',path+'.initial.status.json');}
 const fd=fs.openSync(path,'w'),start=Date.now();
 console.log(`${new Date().toISOString()} start ${name}`);
 const child=spawn(cmd,args,{cwd,env,stdio:['ignore',fd,fd]});
 const code=await new Promise(r=>child.on('exit',(c,s)=>r(c??s)));fs.closeSync(fd);
 const status={name,cmd,args,cwd,code,seconds:(Date.now()-start)/1000};
 fs.writeFileSync(path+'.status.json',JSON.stringify(status,null,2));console.log(status);
 if(code!==0)process.exitCode=1;
}
