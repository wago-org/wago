import {spawn} from 'node:child_process';
const root='/home/jtenner/Projects/wago/.tmp/pr564-memory';
for(const mode of ['explicit','signals']) {
 const names=['BenchmarkCompileWorkers/json-as/p2','BenchmarkCompileWorkers/json-as/p4','BenchmarkCompileWorkers/json-as/p8','BenchmarkCompileWorkers/lua/p4','BenchmarkCompileWorkers/lua/p8','BenchmarkCompileWorkers/esbuild/p2','BenchmarkCompileFull/many_funcs','BenchmarkCompileFull/json-as','BenchmarkValidate/tiny','BenchmarkPluginInstantiate/ruby','BenchmarkExec/coremark.coremark_run'];
 const child=spawn(process.execPath,[`${root}/focused.mjs`,`compact-${mode}`],{env:{...process.env,LABELS:'base,before,compact',SAMPLES:'6',BENCHTIME:'500ms',CASES:JSON.stringify(names.map(n=>[mode,n]))},stdio:'inherit'});
 if(await new Promise(r=>child.on('exit',r))!==0)throw Error(mode);
 const report=spawn(process.execPath,[`${root}/focused-report.mjs`,`compact-${mode}`],{stdio:'inherit'});
 if(await new Promise(r=>report.on('exit',r))!==0)throw Error('report');
}
