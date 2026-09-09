import fs from 'node:fs';
import {spawn,execFileSync} from 'node:child_process';
const root='/home/jtenner/Projects/wago/.tmp/pr564-memory/final731';
const work='/home/jtenner/Projects/wago/.tmp/pr564-memory/hint-scratch';
fs.writeFileSync(`${root}/hint-scratch-source.patch`,execFileSync('git',['diff','HEAD'],{cwd:work}));
const names=['BenchmarkCompileWorkers/json-as/p4','BenchmarkCompileWorkers/json-as/p8','BenchmarkCompileFullWorkers/json-as/p8','BenchmarkCompileWorkers/lua/p8','BenchmarkCompileFullWorkers/esbuild/p4','BenchmarkCompileWorkers/sqlite3/p8'];
for(const mode of ['explicit','signals'])for(const[suffix,benchtime]of [['cold','1x'],['warm','500ms']]) {
 const run=`hintscratch-${suffix}-${mode}`;
 const child=spawn(process.execPath,[`${root}/focused.mjs`,run],{env:{...process.env,LABELS:'base,final,hintscratch',SAMPLES:'6',BENCHTIME:benchtime,CASES:JSON.stringify(names.map(n=>[mode,n]))},stdio:'inherit'});
 if(await new Promise(r=>child.on('exit',r))!==0)throw Error(run);
 const report=spawn(process.execPath,[`${root}/focused-report.mjs`,run],{stdio:'inherit'});
 if(await new Promise(r=>report.on('exit',r))!==0)throw Error('report');
}
