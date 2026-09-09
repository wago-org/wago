import {spawn} from 'node:child_process';
const root='/home/jtenner/Projects/wago/.tmp/pr564-memory/final731';
for(const mode of ['explicit','signals']) {
 const run=`hintscratch-esbuild-fixed-${mode}`;
 const child=spawn(process.execPath,[`${root}/focused.mjs`,run],{env:{...process.env,LABELS:'base,final,hintscratch',SAMPLES:'12',BENCHTIME:'3x',RESOURCE_LOGS:'1',CASES:JSON.stringify([[mode,'BenchmarkCompileFullWorkers/esbuild/p4']])},stdio:'inherit'});
 if(await new Promise(r=>child.on('exit',r))!==0)throw Error(run);
 const report=spawn(process.execPath,[`${root}/focused-report.mjs`,run],{stdio:'inherit'});
 if(await new Promise(r=>report.on('exit',r))!==0)throw Error('report');
}
