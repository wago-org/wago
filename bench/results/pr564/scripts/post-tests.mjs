import fs from 'node:fs';
import {spawn} from 'node:child_process';
const out='/tmp/wago-pr564-HbN430',repo='/home/jtenner/Projects/wago';
const qemu=`${out}/qemu/usr/bin/qemu-aarch64`;
const ref='9d36019973201a19f9c9ebb0f10828b2fe2374aa';
const env={...process.env,GOMAXPROCS:'8',GOFLAGS:'-buildvcs=false',PATH:`${repo}/.tools/wabt-1.0.41-linux-x64/bin:${process.env.PATH}`,WAGO_SPEC_INTERPRETER:`${repo}/.tools/spec-interpreter-${ref}/wasm`,WAGO_SPEC_INTERPRETER_REVISION:ref};
delete env.WAGO_HOME;
const tasks=[
  ['native-runtime','go',['test','./src/wago','-count=1','-timeout=20m']],
  ['native-guard-final','go',['test','-tags','wago_guardpage','./src/wago','-count=1','-timeout=20m']],
  ['arm64-build-runtime','go',['test','-c','-o',`${out}/arm64-wago-final.test`,'./src/wago'],{GOOS:'linux',GOARCH:'arm64',CGO_ENABLED:'0'}],
  ['arm64-build-guard','go',['test','-tags','wago_guardpage','-c','-o',`${out}/arm64-wago-final-signals.test`,'./src/wago'],{GOOS:'linux',GOARCH:'arm64',CGO_ENABLED:'0'}],
  ['arm64-backend-final',qemu,[`${out}/arm64-backend.test`,'-test.run=TestAdapterTemplate|TestMemoryAddressUsesUpperZeroFact|TestParallelModuleHints|TestBuildModuleTypeCache|TestModuleTypeCache|TestCompileWorkers','-test.v']],
  ['arm64-runtime',qemu,[`${out}/arm64-wago-final.test`,'-test.run=TestMemoryValueFactsAcrossTransfers|TestValidatedTypeIndexedControlRequirementsAndArtifact|TestGCFrameRootPlanValidationAnalysisParity','-test.v']],
  ['arm64-guard',qemu,[`${out}/arm64-wago-final-signals.test`,'-test.run=TestMemoryValueFactsAcrossTransfers|TestValidatedTypeIndexedControlRequirementsAndArtifact','-test.v']],
  ['cli-isolated','go',['test','./cli/runtime/commands/build','./cli/runtime/commands/run','./cli/runtime/commands/validate','-count=1'],{WAGO_HOME:`${out}/isolated-cli`}],
  ['self-clean-env','go',['test','./cli/manager/internal/self','-count=1']],
];
fs.writeFileSync(`${out}/post-test-commands.json`,JSON.stringify({envOverrides:Object.fromEntries(['GOMAXPROCS','GOFLAGS','PATH','WAGO_SPEC_INTERPRETER','WAGO_SPEC_INTERPRETER_REVISION'].map(k=>[k,env[k]])),unset:['WAGO_HOME'],tasks},null,2)+'\n');
for(const [label,cmd,args,extra={}] of tasks) {
  if(process.argv[2]&&process.argv[2]!==label)continue;
  const cwd=label==='arm64-backend-final'?`${repo}/src/core/compiler/backend/railshot/arm64`:repo;
  const fd=fs.openSync(`${out}/test-${label}.txt`,'w'),start=Date.now();
  console.log(`${new Date().toISOString()} ${label}`);
  const child=spawn(cmd,args,{cwd,env:{...env,...extra},stdio:['ignore',fd,fd]});
  const code=await new Promise(resolve=>child.on('exit',(c,s)=>resolve(c??s)));fs.closeSync(fd);
  const status={label,cmd,args,cwd,code,seconds:(Date.now()-start)/1000};
  fs.writeFileSync(`${out}/test-${label}.status.json`,JSON.stringify(status,null,2)+'\n');
  console.log(status);
  if(code!==0)process.exitCode=1;
}
