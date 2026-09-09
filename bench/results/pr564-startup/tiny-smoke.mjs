import fs from 'node:fs';
import {spawnSync} from 'node:child_process';
const repo='/home/jtenner/Projects/wago',root=`${repo}/.tmp/pr564-memory`,old=`${repo}/.tmp/pr564-absolute-costs`;
const rows=[];
for(const n of [20,30])for(let repeat=0;repeat<3;repeat++)for(const label of ['base','before','final']) {
 const bin=`${old}/size/${label}-runtime-minimal-tiny`;
 const args=['run','--invoke','fib','tests/fixtures/wasm/fib.wasm',String(n)];
 const start=Date.now();
 const r=spawnSync(bin,args,{cwd:repo,env:{...process.env,WAGO_HOME:`${root}/tiny-smoke-home`},encoding:'utf8',timeout:30000});
 const row={label,n,repeat,bin,args,status:r.status,signal:r.signal,error:r.error?.message,seconds:(Date.now()-start)/1000,stdout:r.stdout,stderr:r.stderr};
 rows.push(row);console.log(row);fs.writeFileSync(`${root}/tiny-smoke.json`,JSON.stringify(rows,null,2)+'\n');
}
