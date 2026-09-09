import fs from 'node:fs';
import {spawn,execFileSync} from 'node:child_process';
import {createHash} from 'node:crypto';
const root='/home/jtenner/Projects/wago/.tmp/pr564-last-pass',repo='/home/jtenner/Projects/wago';
const checks=JSON.parse(fs.readFileSync(`${root}/checks.json`));
if(checks.length!==8||checks.some(r=>r.code!==0))throw Error('Correctness checks incomplete');
const audit=JSON.parse(fs.readFileSync(`${root}/code-audit.json`));
if(audit.length!==112||audit.some(r=>!r.equal))throw Error('Code audit incomplete');
const cold=['BenchmarkCompileFullWorkers/esbuild/p2','BenchmarkCompileFullWorkers/esbuild/p4','BenchmarkCompileFullWorkers/esbuild/auto','BenchmarkCompileFullWorkers/sqlite3/p2','BenchmarkCompileWorkers/json-as/p8'];
const timed=['BenchmarkCompileFullWorkers/esbuild/p4','BenchmarkCompileFullWorkers/sqlite3/p2','BenchmarkCompileWorkers/json-as/p8','BenchmarkCompileCompact/json-as','BenchmarkExec/coremark.coremark_run','BenchmarkExec/isa_f32.min','BenchmarkExec/tiny.add','BenchmarkExecParallel/process/isa_bulk_mem.copy_fwd_64'];
const plan=['explicit','signals'].flatMap(mode=>[['cold',cold,'1x'],['timed',timed,'250ms']].map(([kind,names,benchtime])=>({name:`${kind}-${mode}`,cases:names.map(n=>[mode,n]),benchtime,samples:6})));
const source=execFileSync('git',['diff','HEAD','--','src/core/compiler/backend/railshot'],{cwd:repo});
fs.writeFileSync(`${root}/source.patch`,source);
fs.writeFileSync(`${root}/plan.json`,JSON.stringify({main:'731e95ff2cda7309eaf6d956f1417066bf7f1b69',before:execFileSync('git',['rev-parse','HEAD'],{cwd:repo,encoding:'utf8'}).trim(),sourcePatchSHA256:createHash('sha256').update(source).digest('hex'),go:execFileSync('go',['version'],{encoding:'utf8'}).trim(),plan,totalProcesses:plan.reduce((n,p)=>n+p.cases.length*p.samples*3,0)},null,2)+'\n');
const statuses=[];
for(const p of plan){
 const start=Date.now(),fd=fs.openSync(`${root}/${p.name}-driver.txt`,'w');
 const child=spawn(process.execPath,[`${root}/focused.mjs`,p.name],{cwd:root,env:{...process.env,LABELS:'base,before,final',SAMPLES:String(p.samples),BENCHTIME:p.benchtime,RESOURCE_LOGS:'1',CASES:JSON.stringify(p.cases)},stdio:['ignore',fd,fd]});
 const code=await new Promise(r=>child.on('exit',(c,s)=>r(c??s)));fs.closeSync(fd);
 statuses.push({name:p.name,code,start:new Date(start).toISOString(),seconds:(Date.now()-start)/1000});fs.writeFileSync(`${root}/confirm-status.json`,JSON.stringify(statuses,null,2)+'\n');
 if(code!==0)throw Error(p.name);
 execFileSync(process.execPath,[`${root}/focused-report.mjs`,p.name],{cwd:root});
 console.log(p.name,'complete',statuses.at(-1).seconds);
}
