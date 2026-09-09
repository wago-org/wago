import fs from 'node:fs';
import {spawnSync} from 'node:child_process';
import {createHash} from 'node:crypto';
const repo='/home/jtenner/Projects/wago',root=`${repo}/.tmp/pr564-memory`;
const binary=process.argv[2]||`${root}/namecheck-tiny`,stem=process.argv[3]||'namecheck-smoke',rows=[];
const cases=[['version',['--version'],s=>s.includes('tinygo 0.41.1')],['fib20',['run','--invoke','fib','tests/fixtures/wasm/fib.wasm','20'],s=>s.trim()==='fib(20) = 6765'],['fib30',['run','--invoke','fib','tests/fixtures/wasm/fib.wasm','30'],s=>s.trim()==='fib(30) = 832040'],['hypot',['run','--invoke','hypot','tests/fixtures/wasm/fprog.wasm','3.0','4.0'],s=>s.includes('= 5')]];
const env={...process.env,WAGO_HOME:`${root}/namecheck-home`};
for(let repeat=0;repeat<20;repeat++)for(const [name,args,check]of cases){
 const r=spawnSync(binary,args,{cwd:repo,env,encoding:'utf8',timeout:15000});
 const row={name,args,repeat,status:r.status,signal:r.signal,error:r.error?.message,stdout:r.stdout,stderr:r.stderr,passed:r.status===0&&check(r.stdout||'')};rows.push(row);
 fs.writeFileSync(`${root}/${stem}.json`,JSON.stringify({binary,sha256:createHash('sha256').update(fs.readFileSync(binary)).digest('hex'),WAGO_HOME:env.WAGO_HOME,rows},null,2)+'\n');
 if(!row.passed)console.log(row);
}
console.log({total:rows.length,failed:rows.filter(r=>!r.passed).length});
if(rows.some(r=>!r.passed))process.exitCode=1;
