import fs from 'node:fs';
import {spawnSync} from 'node:child_process';
const out='/tmp/wago-pr564-HbN430',repo='/home/jtenner/Projects/wago';
const rows=[];
for(const label of ['base','candidate'])for(const profile of ['manager','runtime-standard','runtime-minimal','runtime-minimal-tiny']) {
  const cmd=`${out}/build-size/${label}-${profile}`;
  const args=profile==='manager'?['version']:['run','tests/fixtures/wasm/fib.wasm','30'];
  const result=spawnSync(cmd,args,{cwd:repo,env:{...process.env,WAGO_CONFIG:`${out}/isolated-settings.json`,WAGO_HOME:`${out}/isolated-build-smoke`},encoding:'utf8',timeout:30000});
  const ok=result.status===0&&(profile==='manager'||result.stdout.includes('832040'));
  rows.push({label,profile,cmd,args,code:result.status,stdout:result.stdout,stderr:result.stderr,error:result.error?String(result.error):null,ok});
  if(!ok)process.exitCode=1;
}
fs.writeFileSync(`${out}/test-build-smoke.json`,JSON.stringify(rows,null,2)+'\n');
console.log(rows);
