import fs from 'node:fs';
import {spawn} from 'node:child_process';
const repo='/home/jtenner/Projects/wago',root=`${repo}/.tmp/pr564-memory/final-closures`,rows=[];
for(const mode of ['explicit','signals']) {
 const inputs={};
 for(const label of ['base','final']) {
  const cwd=label==='base'?`${repo}/.tmp/pr564-memory/main-latest/bench`:`${repo}/bench`,name=`audit-compact-${label}-${mode}`;
  const args=['test',`-overlay=${root}/audit-${label}.json`,...(mode==='signals'?['-tags','wago_guardpage']:[]),'-run','^Test(AllExecutableCodeAudit|AbsoluteCostCodeAudit)$','-count=1','-v','-wago.bench.isa'];
  const fd=fs.openSync(`${root}/${name}.txt`,'w'),start=Date.now();
  const child=spawn('go',args,{cwd,env:{...process.env,GOMAXPROCS:'8',GOGC:'100',WAGO_BOUNDS:mode},stdio:['ignore',fd,fd]});
  const code=await new Promise(r=>child.on('exit',(c,s)=>r(c??s)));fs.closeSync(fd);
  fs.writeFileSync(`${root}/${name}.status.json`,JSON.stringify({args,cwd,code,start:new Date(start).toISOString(),seconds:(Date.now()-start)/1000},null,2));
  if(code!==0)throw Error(`${name}: ${code}`);
  const text=fs.readFileSync(`${root}/${name}.txt`,'utf8');
  inputs[label]=Object.fromEntries([...text.matchAll(/(?:EXEC_CODE|CODE) (\S+) bytes=(\d+) sha256=([a-f0-9]{64})/g)].map(m=>[m[1],{bytes:+m[2],sha256:m[3]}]));
 }
 const names=Object.keys(inputs.base).sort();
 if(names.length===0||JSON.stringify(names)!==JSON.stringify(Object.keys(inputs.final).sort()))throw Error('Code audit coverage mismatch');
 for(const name of names)rows.push({mode,name,base:inputs.base[name],after:inputs.final[name],equal:JSON.stringify(inputs.base[name])===JSON.stringify(inputs.final[name])});
}
fs.writeFileSync(`${root}/audit-compact-summary.json`,JSON.stringify(rows,null,2)+'\n');
console.log({codePairs:rows.length,changed:rows.filter(r=>!r.equal)});
