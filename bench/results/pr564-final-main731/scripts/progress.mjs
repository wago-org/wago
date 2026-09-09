import fs from 'node:fs';
const root='/home/jtenner/Projects/wago/.tmp/pr564-memory/final-closures';
const readRows=p=>fs.existsSync(p)?fs.readFileSync(p,'utf8').trim().split('\n').filter(Boolean).map(JSON.parse):[];
const stage=JSON.parse(fs.readFileSync(`${root}/qualification-progress.json`));
const full=readRows(`${root}/full/status.jsonl`);
const result={time:new Date().toISOString(),stage:stage.stage,fullProcesses:full.length,failures:full.filter(r=>r.code!==0).length};
if(stage.stage==='full'){
 const old=readRows(`${root}/../final731/full/status.jsonl`),done=new Set(full.map(r=>r.key));
 const remaining=old.filter(r=>!done.has(r.key)).reduce((s,r)=>s+r.seconds,0);
 result.current=JSON.parse(fs.readFileSync(`${root}/full/progress.json`)).key;
 result.priorRunRemainingMinutes=Math.ceil(remaining/60);
}else{
 for(const name of ['confirm-explicit','confirm-signals','fixed-memory-explicit','fixed-instantiate-explicit','fixed-rss-explicit','fixed-memory-signals','fixed-instantiate-signals','fixed-rss-signals']){
  const rows=readRows(`${root}/${name}/status.jsonl`);if(rows.length)result[name]={processes:rows.length,last:rows.at(-1).name,failures:rows.filter(r=>r.code!==0).length};
 }
}
console.log(JSON.stringify(result,null,2));
