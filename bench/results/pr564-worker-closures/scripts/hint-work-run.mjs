import fs from 'node:fs';
import {spawn,execFileSync} from 'node:child_process';
const root='/home/jtenner/Projects/wago/.tmp/pr564-memory/final731';
const work='/home/jtenner/Projects/wago/.tmp/pr564-memory/hint-work';
fs.writeFileSync(`${root}/hint-work-source.patch`,execFileSync('git',['diff','HEAD','--','src/core/compiler/backend/railshot/amd64/compile.go','src/core/compiler/backend/railshot/arm64/compile.go'],{cwd:work}));
const names=['BenchmarkCompileWorkers/json-as/p2','BenchmarkCompileWorkers/json-as/p4','BenchmarkCompileWorkers/json-as/p8','BenchmarkCompileFullWorkers/json-as/p8','BenchmarkCompileWorkers/lua/p4','BenchmarkCompileWorkers/lua/p8','BenchmarkCompileWorkers/esbuild/p4','BenchmarkCompileWorkers/sqlite3/p8'];
for(const mode of ['explicit','signals'])for(const[suffix,benchtime]of [['cold','1x'],['warm','500ms']]) {
 const run=`hintwork-${suffix}-${mode}`;
 const child=spawn(process.execPath,[`${root}/focused.mjs`,run],{env:{...process.env,LABELS:'base,final,hintwork',SAMPLES:'6',BENCHTIME:benchtime,CASES:JSON.stringify(names.map(n=>[mode,n]))},stdio:'inherit'});
 if(await new Promise(r=>child.on('exit',r))!==0)throw Error(run);
 const report=spawn(process.execPath,[`${root}/focused-report.mjs`,run],{stdio:'inherit'});
 if(await new Promise(r=>report.on('exit',r))!==0)throw Error('report');
}
