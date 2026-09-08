import fs from 'node:fs';
import {spawn} from 'node:child_process';
const out='/tmp/wago-pr564-HbN430',dir=`${out}/confirmations`;
const cwd='/home/jtenner/Projects/wago/bench';
const all=JSON.parse(fs.readFileSync(`${out}/comparison.json`));
const selected=new Map();
for(const r of all) {
  if(r.unit!=='ns/op'||/Wazero|_wazero/.test(r.name))continue;
  if((r.delta>0&&r.p!==null&&r.p<0.05)||r.delta>5)selected.set(`${r.mode}/${r.name}`,{mode:r.mode,name:r.name});
}
fs.mkdirSync(dir,{recursive:true});
fs.writeFileSync(`${dir}/selection.json`,JSON.stringify([...selected.values()],null,2)+'\n');
const statusPath=`${dir}/status.jsonl`;
const done=new Set(fs.existsSync(statusPath)?fs.readFileSync(statusPath,'utf8').trim().split('\n').filter(Boolean).map(JSON.parse).filter(r=>r.code===0).map(r=>r.key):[]);
const escape=s=>s.replace(/[.*+?^${}()|[\]\\]/g,'\\$&');
let caseIndex=0;
for(const {mode,name} of selected.values()) {
  const expression=name.split('/').map(s=>`^${escape(s)}$`).join('/');
  for(let sample=0;sample<12;sample++)for(const label of (sample%2?['candidate','base']:['base','candidate'])) {
    const key=`${label}-${mode}-${caseIndex}-${sample}`;
    if(done.has(key))continue;
    const fd=fs.openSync(`${dir}/${key}.txt`,'w'),start=Date.now();
    const args=['-v','-o',`${dir}/${key}.resource.txt`,`${out}/${label}-${mode}.test`,'-test.run=^$',`-test.bench=${expression}`,'-test.benchmem','-test.count=1','-test.benchtime=300ms','-test.timeout=10m','-test.v','-wago.bench.isa'];
    fs.writeFileSync(`${out}/confirmation-progress.json`,JSON.stringify({name,mode,key,caseIndex,cases:selected.size,start:new Date().toISOString()}));
    const child=spawn(`${out}/time-tool/usr/bin/time`,args,{cwd,env:{...process.env,GOMAXPROCS:'8',GOGC:'100',WAGO_BOUNDS:mode},stdio:['ignore',fd,fd]});
    const code=await new Promise(resolve=>child.on('exit',(c,s)=>resolve(c??s)));fs.closeSync(fd);
    const status={key,label,mode,name,sample,code,seconds:(Date.now()-start)/1000};
    fs.appendFileSync(statusPath,JSON.stringify(status)+'\n');
    if(code!==0){console.log(status);process.exit(1);}done.add(key);
  }
  console.log(`${new Date().toISOString()} Confirmed ${mode}/${name} (${++caseIndex}/${selected.size})`);
}
for(const label of ['base','candidate'])for(const mode of ['explicit','signals']) {
  const files=fs.readdirSync(dir).filter(s=>s.startsWith(`${label}-${mode}-`)&&s.endsWith('.txt')&&!s.endsWith('.resource.txt')).sort();
  fs.writeFileSync(`${out}/confirmation-${label}-${mode}.txt`,files.map(file=>`# ${file}\n`+fs.readFileSync(`${dir}/${file}`,'utf8')).join('\n'));
}
