import fs from 'node:fs';
import {spawn,execFileSync} from 'node:child_process';
const out='/tmp/wago-pr564-HbN430';
const cwd='/home/jtenner/Projects/wago/bench';
const samples=6;
const listed=(label,mode)=>execFileSync(`${out}/${label}-${mode}.test`,['-test.list=Benchmark'],{cwd,encoding:'utf8'}).split('\n').filter(s=>s.startsWith('Benchmark'));
const modeTops=Object.fromEntries(['explicit','signals'].map(mode=>[mode,[...new Set([...listed('base',mode),...listed('candidate',mode)])]]));
const total=Object.values(modeTops).reduce((n,tops)=>n+tops.length*2*samples,0);
const dir=`${out}/paired`;
fs.mkdirSync(dir,{recursive:true});
const statusPath=`${dir}/status.jsonl`;
const done=new Set(fs.existsSync(statusPath)?fs.readFileSync(statusPath,'utf8').trim().split('\n').filter(Boolean).map(JSON.parse).filter(r=>r.code===0).map(r=>r.key):[]);
for(const mode of ['explicit','signals']) {
  for(const top of modeTops[mode]) {
    for(let sample=0;sample<samples;sample++) {
      const order=sample%2?['candidate','base']:['base','candidate'];
      for(const label of order) {
        const key=`${label}-${mode}-${top}-${sample}`;
        if(done.has(key))continue;
        const file=`${dir}/${key}.txt`;
        const fd=fs.openSync(file,'w');
        const args=['-v','-o',`${dir}/${key}.resource.txt`,`${out}/${label}-${mode}.test`,'-test.run=^$',`-test.bench=^${top}$`,'-test.benchmem','-test.count=1','-test.benchtime=100ms','-test.timeout=10m','-test.v','-wago.bench.isa'];
        const start=Date.now();
        fs.writeFileSync(`${out}/progress.json`,JSON.stringify({key,start:new Date(start).toISOString(),completed:done.size,total}));
        const child=spawn(`${out}/time-tool/usr/bin/time`,args,{cwd,env:{...process.env,GOMAXPROCS:'8',GOGC:'100',WAGO_BOUNDS:mode==='signals'?'signals':'explicit'},stdio:['ignore',fd,fd]});
        const code=await new Promise(resolve=>child.on('exit',(c,s)=>resolve(c??s)));
        fs.closeSync(fd);
        const status={key,label,mode,top,sample,code,seconds:(Date.now()-start)/1000};
        fs.appendFileSync(statusPath,JSON.stringify(status)+'\n');
        if(code!==0){console.log(status);process.exit(1);}
        done.add(key);
      }
    }
    console.log(`${new Date().toISOString()} Completed ${mode}/${top} (${done.size}/${total} processes)`);
  }
}
for(const label of ['base','candidate'])for(const mode of ['explicit','signals']) {
  const path=`${out}/${label}-${mode}.txt`;
  if(fs.existsSync(path)&&!fs.existsSync(`${out}/initial-${label}-${mode}.txt`))fs.renameSync(path,`${out}/initial-${label}-${mode}.txt`);
  const files=fs.readdirSync(dir).filter(s=>s.startsWith(`${label}-${mode}-`)&&s.endsWith('.txt')&&!s.endsWith('.resource.txt')).sort();
  fs.writeFileSync(path,files.map(file=>`# ${file}\n`+fs.readFileSync(`${dir}/${file}`,'utf8')).join('\n'));
  fs.writeFileSync(`${out}/${label}-${mode}.status.json`,JSON.stringify({label,mode,code:0,files:files.length,method:'six alternating-order pairs, one top-level benchmark and sample per process'},null,2)+'\n');
}
