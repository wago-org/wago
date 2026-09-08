import fs from 'node:fs';
import {spawn} from 'node:child_process';
const out = '/tmp/wago-pr564-HbN430';
const repo = '/home/jtenner/Projects/wago';
const tasks = {
  focused: ['go',['test','./src/core/compiler/wasm','./src/core/compiler/frontend','./src/core/compiler/backend/railshot/shared','./src/core/compiler/backend/railshot/amd64','./src/wago','-run','TestValidated|TestValidateModuleWithAnalysis|TestValidationAnalysis|TestBuildModuleTypeCache|TestModuleTypeCache|TestGlobalHint|TestLowestIndexError|TestFuncHintsSize|TestStackArena|TestParallelModuleHints|TestCompileWorkers|TestMemoryValueFacts','-count=1','-timeout=10m']],
  full: ['go',['test','./...','-count=1','-timeout=20m']],
  benchtests: ['go',['test','./...','-count=1','-timeout=20m'],`${repo}/bench`],
  race: ['go',['test','-race','./src/core/compiler/wasm','./src/core/compiler/frontend','./src/core/compiler/backend/railshot/shared','./src/core/compiler/backend/railshot/amd64','-count=1','-timeout=15m']],
  guard: ['go',['test','-tags','wago_guardpage','./src/wago','-count=1','-timeout=20m']],
  corpusguard: ['go',['test','-tags','wago_guardpage','-run','TestCorpusDifferential|TestJsonAsGuardCorrect|^TestCorpus$','-count=1','-timeout=20m','.'],`${repo}/bench`],
  spec2: ['make',['spec2']],
  micro: ['go',['test','./src/core/compiler/wasm','./src/core/compiler/backend/railshot/amd64','-run','^$','-bench','BenchmarkValidatedDynamicCallFacts|BenchmarkBuildModuleTypeCache','-benchmem','-count=6','-benchtime=200ms']],
};
for (const label of process.argv.slice(2)) {
  const [cmd,args,cwd=repo] = tasks[label];
  const fd=fs.openSync(`${out}/test-${label}.txt`,'w');
  const start = Date.now();
  console.log(`${new Date().toISOString()} ${label}: ${cmd} ${args.join(' ')}`);
  const child=spawn(cmd,args,{cwd,env:{...process.env,GOMAXPROCS:'8'},stdio:['ignore',fd,fd]});
  const code=await new Promise(resolve=>child.on('exit',(c,s)=>resolve(c??s)));
  fs.closeSync(fd);
  const status={label,code,seconds:(Date.now()-start)/1000};
  fs.writeFileSync(`${out}/test-${label}.status.json`,JSON.stringify(status,null,2)+'\n');
  console.log(status);
  if(code!==0)process.exitCode=1;
}
