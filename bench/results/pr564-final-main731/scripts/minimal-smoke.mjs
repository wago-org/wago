import fs from 'node:fs';
import {spawnSync} from 'node:child_process';
import {createHash} from 'node:crypto';
const repo='/home/jtenner/Projects/wago',root=`${repo}/.tmp/pr564-memory/final-closures`;
const binary=`${root}/size/final-runtime-minimal-tiny`,rows=[];
const cases=[['version',['--version'],s=>s.includes('tinygo 0.41.1')],['fib20',['run','--invoke','fib','tests/fixtures/wasm/fib.wasm','20'],s=>s.trim()==='fib(20) = 6765'],['fib30',['run','--invoke','fib','tests/fixtures/wasm/fib.wasm','30'],s=>s.trim()==='fib(30) = 832040'],['hypot',['run','--invoke','hypot','tests/fixtures/wasm/fprog.wasm','3.0','4.0'],s=>s.includes('= 5')]];
const identity={head:JSON.parse(fs.readFileSync(`${root}/metadata.json`)).head,binary,bytes:fs.statSync(binary).size,sha256:createHash('sha256').update(fs.readFileSync(binary)).digest('hex'),rows};
for(let repeat=0;repeat<20;repeat++)for(const[name,args,check]of cases){
 const r=spawnSync(binary,args,{cwd:repo,env:{...process.env,WAGO_HOME:`${root}/minimal-cli-home`},encoding:'utf8',timeout:15000});
 rows.push({repeat,name,args,status:r.status,signal:r.signal,error:r.error?.message,stdout:r.stdout,stderr:r.stderr,passed:r.status===0&&check(r.stdout??'')});
 fs.writeFileSync(`${root}/minimal-tiny-smoke.json`,JSON.stringify(identity,null,2)+'\n');
}
console.log({samples:rows.length,failed:rows.filter(r=>!r.passed).length});
if(rows.some(r=>!r.passed))process.exitCode=1;
