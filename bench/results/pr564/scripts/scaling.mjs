import fs from 'node:fs';
import {spawn,execFileSync} from 'node:child_process';
const out='/tmp/wago-pr564-HbN430',dir=`${out}/single-worker`;
fs.mkdirSync(dir,{recursive:true});
const names=['BenchmarkExecParallel/process/swar-pack-parse.parse4','BenchmarkExecParallel/independent/fib_iter.fib','BenchmarkExecParallel/process/xjb-mulhi.mulhi','BenchmarkExecGlobalGet_wago'];
const escape=s=>s.replace(/[.*+?^${}()|[\]\\]/g,'\\$&');
const rows=[];
for(let index=0;index<names.length;index++) {
  const name=names[index],expression=name.split('/').map(s=>`^${escape(s)}$`).join('/');
  const samples={base:[],candidate:[]};
  for(let sample=0;sample<12;sample++)for(const label of sample%2?['candidate','base']:['base','candidate']) {
    const key=`${label}-${index}-${sample}`,fd=fs.openSync(`${dir}/${key}.txt`,'w');
    const args=['-v','-o',`${dir}/${key}.resource.txt`,`${out}/${label}-explicit.test`,'-test.run=^$',`-test.bench=${expression}`,'-test.benchmem','-test.benchtime=300ms','-test.count=1','-test.timeout=10m','-test.v','-wago.bench.isa'];
    const child=spawn(`${out}/time-tool/usr/bin/time`,args,{cwd:'/home/jtenner/Projects/wago/bench',env:{...process.env,GOMAXPROCS:'1',GOGC:'100',WAGO_BOUNDS:'explicit'},stdio:['ignore',fd,fd]});
    const code=await new Promise(resolve=>child.on('exit',(c,s)=>resolve(c??s)));fs.closeSync(fd);
    fs.appendFileSync(`${dir}/status.jsonl`,JSON.stringify({key,name,sample,label,code,GOMAXPROCS:1})+'\n');
    if(code!==0)throw new Error(`${key} failed: ${code}`);
    for(const line of fs.readFileSync(`${dir}/${key}.txt`,'utf8').split('\n')) {
      const p=line.trim().split(/\s+/);
      if(p[0]?.replace(/-\d+$/,'')!==name||!/^\d+$/.test(p[1]))continue;
      const at=p.indexOf('ns/op');if(at>1)samples[label].push(Number(p[at-1]));
    }
  }
  const median=a=>{if(a.length!==12)throw new Error(`wrong sample count for ${name}`);a.sort((x,y)=>x-y);return(a[5]+a[6])/2;};
  const base=median(samples.base),candidate=median(samples.candidate);
  rows.push({name,GOMAXPROCS:1,base,candidate,delta:100*(candidate/base-1),samples});
  console.log(rows.at(-1));
}
for(const label of ['base','candidate']) {
  const files=fs.readdirSync(dir).filter(s=>s.startsWith(`${label}-`)&&s.endsWith('.txt')&&!s.endsWith('.resource.txt')).sort();
  fs.writeFileSync(`${out}/single-worker-${label}.txt`,files.map(s=>fs.readFileSync(`${dir}/${s}`,'utf8')).join('\n'));
}
fs.writeFileSync(`${out}/single-worker-summary.json`,JSON.stringify({note:'Separate GOMAXPROCS=1 diagnostic, not mixed into the GOMAXPROCS=8 full comparison. See benchstat-single-worker.txt for significance tests.',rows},null,2)+'\n');
fs.writeFileSync(`${out}/benchstat-single-worker.txt`,execFileSync(`${out}/bin/benchstat`,[`${out}/single-worker-base.txt`,`${out}/single-worker-candidate.txt`],{encoding:'utf8'}));
