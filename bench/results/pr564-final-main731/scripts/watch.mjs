import fs from 'node:fs';
import {execFileSync} from 'node:child_process';
const root='/home/jtenner/Projects/wago/.tmp/pr564-memory/final-closures';
const expected=JSON.parse(fs.readFileSync(`${root}/metadata.json`)).head;
let tick=0,green=false;
while(true){
 const progress=JSON.parse(execFileSync(process.execPath,[`${root}/progress.mjs`],{encoding:'utf8'}));
 console.log(JSON.stringify(progress));
 if(!green||tick%6===0){
  try{
   const raw=execFileSync('gh',['pr','view','564','--json','headRefOid,mergeable,mergeStateStatus,reviewDecision,statusCheckRollup'],{encoding:'utf8',timeout:20000});
   const ci=JSON.parse(raw),pending=ci.statusCheckRollup.filter(r=>r.status!=='COMPLETED'),failed=ci.statusCheckRollup.filter(r=>!['','SUCCESS','SKIPPED','NEUTRAL',null].includes(r.conclusion));
   fs.writeFileSync(`${root}/ci-current.json`,raw);
   const row={time:new Date().toISOString(),head:ci.headRefOid,pending:pending.map(r=>r.name),failed:failed.map(r=>({name:r.name,conclusion:r.conclusion})),review:ci.reviewDecision};
   fs.appendFileSync(`${root}/ci-monitor.jsonl`,JSON.stringify(row)+'\n');console.log(JSON.stringify(row));
   if(ci.headRefOid!==expected||failed.length){process.exitCode=1;break;}
   if(!pending.length&&ci.statusCheckRollup.length){green=true;fs.writeFileSync(`${root}/ci-b4f236-green.json`,raw);}
  }catch(error){console.log(JSON.stringify({ciReadError:error.message}));}
 }
 if(progress.stage==='complete')break;
 const status=fs.existsSync(`${root}/qualification-status.jsonl`)?fs.readFileSync(`${root}/qualification-status.jsonl`,'utf8').trim().split('\n').filter(Boolean).map(JSON.parse):[];
 if(status.some(r=>r.code!==0)||progress.failures){process.exitCode=1;break;}
 tick++;await new Promise(resolve=>setTimeout(resolve,45000));
}
