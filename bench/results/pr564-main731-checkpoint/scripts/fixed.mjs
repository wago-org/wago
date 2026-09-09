import {spawn} from 'node:child_process';
const root='/home/jtenner/Projects/wago/.tmp/pr564-memory/final731';
for(const mode of ['explicit','signals'])for(const [suffix,benchtime,names] of [
 ['memory','1x',['BenchmarkCompileWorkers/json-as/p2','BenchmarkCompileWorkers/json-as/p4','BenchmarkCompileWorkers/json-as/p8','BenchmarkCompileWorkers/lua/p4','BenchmarkCompileWorkers/lua/p8','BenchmarkCompileWorkers/esbuild/p2','BenchmarkCompileFull/many_funcs','BenchmarkCompileFull/json-as','BenchmarkCompileFullWorkers/esbuild/p4','BenchmarkCompileWorkers/esbuild/p8','BenchmarkCompileWorkers/ruby/p8','BenchmarkCompileWorkers/sqlite3/p4','BenchmarkCompileWorkers/sqlite3/p8','BenchmarkCompileFullWorkers/sqlite3/p8','BenchmarkCompileFullWorkers/json-as/p8','BenchmarkCompileWorkers/tiny/p2']],
 ['instantiate','128x',['BenchmarkPluginInstantiate/ruby']],
 ['rss','1x',['BenchmarkCompileCompact']],
]) {
 const run=`fixed-${suffix}-${mode}`;
 const child=spawn(process.execPath,[`${root}/focused.mjs`,run],{env:{...process.env,LABELS:'base,final',SAMPLES:'6',BENCHTIME:benchtime,RESOURCE_LOGS:'1',CASES:JSON.stringify(names.map(n=>[mode,n]))},stdio:'inherit'});
 if(await new Promise(r=>child.on('exit',r))!==0)throw Error(run);
 const report=spawn(process.execPath,[`${root}/focused-report.mjs`,run],{stdio:'inherit'});
 if(await new Promise(r=>report.on('exit',r))!==0)throw Error('report');
}
