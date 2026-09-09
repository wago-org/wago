import fs from 'node:fs';
import {spawn,execFileSync} from 'node:child_process';
const root='/home/jtenner/Projects/wago/.tmp/pr564-memory/final-closures';
const head=JSON.parse(fs.readFileSync(`${root}/metadata.json`)).head;
if(execFileSync('git',['rev-parse','HEAD'],{encoding:'utf8'}).trim()!==head)throw Error('Source head changed');
const checks=JSON.parse(fs.readFileSync(`${root}/extra-checks.json`));
if(checks.length!==4||checks.some(r=>r.status!==0&&!['main-wine-installer','final-wine-installer'].includes(r.name)))throw Error('Unexpected release check failure');
for(const file of ['minimal-tiny-smoke.json','standard-tiny-smoke.json']){
 const data=JSON.parse(fs.readFileSync(`${root}/${file}`));
 if(data.head!==head||data.rows.length!==80||data.rows.some(r=>!r.passed))throw Error(file);
}
const host=spawn(process.execPath,[`${root}/host.mjs`],{stdio:'ignore'});
const stages=[['full'],['analyze'],['render','--full-only'],['repeat'],['fixed'],['render','--confirmed-only']];
try{
 for(const[stage,...args]of stages){
  const start=new Date().toISOString();
  fs.writeFileSync(`${root}/qualification-progress.json`,JSON.stringify({stage,start,head}));
  const fd=fs.openSync(`${root}/${stage}-qualification.txt`,'w');
  const child=spawn(process.execPath,[`${root}/${stage}.mjs`,...args],{stdio:['ignore',fd,fd]});
  const code=await new Promise(resolve=>child.on('exit',(c,s)=>resolve(c??s)));fs.closeSync(fd);
  fs.appendFileSync(`${root}/qualification-status.jsonl`,JSON.stringify({stage,args,start,end:new Date().toISOString(),code,head})+'\n');
  console.log(stage,code);if(code!==0)throw Error(stage);
 }
 fs.writeFileSync(`${root}/qualification-progress.json`,JSON.stringify({stage:'complete',end:new Date().toISOString(),head}));
}finally{host.kill('SIGTERM');}
