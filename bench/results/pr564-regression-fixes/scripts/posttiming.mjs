import fs from 'node:fs';
import {spawn} from 'node:child_process';
const root='/tmp/wago-pr564-regressions-zHTLww',repo='/home/jtenner/Projects/wago';
const env={...process.env,GOMAXPROCS:'8',GOGC:'100'};delete env.GOROOT;
async function run(name,cmd,args,cwd=repo,overrides={}) {
 const path=`${root}/${name}.txt`;
 if(fs.existsSync(path))throw Error(`Refusing to overwrite ${path}`);
 const fd=fs.openSync(path,'w'),start=Date.now();
 const child=spawn(cmd,args,{cwd,env:{...env,...overrides},stdio:['ignore',fd,fd]});
 const code=await new Promise(r=>child.on('exit',(c,s)=>r(c??s)));fs.closeSync(fd);
 const row={name,cmd,args,cwd,code,start:new Date(start).toISOString(),seconds:(Date.now()-start)/1000};
 fs.writeFileSync(`${path}.status.json`,JSON.stringify(row,null,2)+'\n');console.log(row);
 return code;
}
if(await run('build-result-layout','go',['test','-c','-o',`${root}/final-wago.test`,'./src/wago']))process.exit(1);
if(await run('build-result-layout-signals','go',['test','-tags','wago_guardpage','-c','-o',`${root}/final-wago-signals.test`,'./src/wago']))process.exit(1);
for(const mode of ['explicit','signals']) {
const runName=mode==='explicit'?'result-layout':'result-layout-signals';
const dir=`${root}/${runName}`;fs.mkdirSync(dir,{recursive:true});
const binary=mode==='explicit'?'final-wago.test':'final-wago-signals.test';
for(const independent of [false,true])for(let sample=0;sample<6;sample++)for(const packed of sample%2?[false,true]:[true,false]) {
 const name=`${runName}/independent-${independent}-packed-${packed}-${sample}`;
 if(await run(name,`${root}/${binary}`,['-test.run=^$',`-test.bench=^BenchmarkInstanceResultLayout$/^independent=${independent}$/^packed=${packed}$`,'-test.benchmem','-test.benchtime=500ms','-test.count=1','-test.timeout=5m'],`${repo}/src/wago`,{WAGO_BOUNDS:mode}))process.exit(1);
}
const files=fs.readdirSync(dir).filter(s=>s.endsWith('.txt')).sort();
fs.writeFileSync(`${dir}/layout.bench`,files.map(s=>fs.readFileSync(`${dir}/${s}`,'utf8')).join('\n'));
}
if(await run('test-arm64-backend-full','/tmp/wago-pr564-HbN430/qemu/usr/bin/qemu-aarch64',[`${root}/arm64.test`,'-test.v','-test.timeout=10m'],`${repo}/src/core/compiler/backend/railshot/arm64`))process.exitCode=1;
