import fs from 'node:fs';
import {execFileSync} from 'node:child_process';
import {rankP} from './analyze.mjs';
const root='/tmp/wago-pr564-regressions-zHTLww',dir=`${root}/full`;
const rows=JSON.parse(fs.readFileSync(`${dir}/comparison.json`));
const status=fs.readFileSync(`${dir}/status.jsonl`,'utf8').trim().split('\n').map(JSON.parse);
const confirmation=[];
for(const mode of ['explicit','signals']) {
 const file=`${root}/confirm-${mode}/summary.json`;if(!fs.existsSync(file))continue;
 for(const row of JSON.parse(fs.readFileSync(file))) {
  const a=row.samples.base,b=row.samples.final;
  confirmation.push({mode,name:row.name,unit:row.unit,base:a.median,final:b.median,delta:b.delta,p:rankP(a.values,b.values),baseSamples:a.n,finalSamples:b.n});
 }
 const table=execFileSync('/tmp/wago-pr564-HbN430/bin/benchstat',[`${dir}/base-${mode}.bench`,`${dir}/final-${mode}.bench`],{encoding:'utf8',maxBuffer:64*1024*1024});
 fs.writeFileSync(`${dir}/benchstat-${mode}.txt`,table.split('\n').map(s=>s.trimEnd()).join('\n'));
}
fs.writeFileSync(`${dir}/confirmed-timing.json`,JSON.stringify(confirmation,null,2)+'\n');
const confirmed=confirmation.filter(r=>r.unit==='ns/op'&&r.delta>0&&r.p<.05);
const processSummary=[];
for(const mode of ['explicit','signals'])for(const label of ['base','final']) {
 const group=status.filter(r=>r.mode===mode&&r.label===label);
 const rss=group.map(r=>Number(fs.readFileSync(`${dir}/${r.key}.txt.resource`,'utf8').match(/Maximum resident set size \(kbytes\):\s*(\d+)/)?.[1]??NaN));
 processSummary.push({mode,label,processes:group.length,failed:group.filter(r=>r.code!==0).length,seconds:group.reduce((s,r)=>s+r.seconds,0),maxRSSKiB:Math.max(...rss)});
}
const skipped=new Set();
for(const r of status)for(const line of fs.readFileSync(`${dir}/${r.key}.txt`,'utf8').split('\n'))if(line.includes('--- SKIP:'))skipped.add(`${r.mode}/${r.label}: ${line.trim()}`);
const resourceRows=rows.filter(r=>['B/op','allocs/op','B/call','allocs/call'].includes(r.unit)&&r.candidate>r.base&&!/Wazero|_wazero/.test(r.name));
fs.writeFileSync(`${dir}/resource-increases.tsv`,'bounds\tbenchmark\tunit\tmain\tfixed\tdelta_percent\tp\n'+resourceRows.sort((a,b)=>(b.delta??Infinity)-(a.delta??Infinity)).map(r=>[r.mode,r.name,r.unit,r.base,r.candidate,r.delta,r.p].join('\t')).join('\n')+'\n');
const code=rows.filter(r=>r.unit==='code-B'&&!/Wazero|_wazero/.test(r.name));
const q={processSummary,pairedMetrics:rows.filter(r=>!r.status).length,unpaired:rows.filter(r=>r.status),badSampleCounts:rows.filter(r=>!r.status&&(r.baseSamples!==6||r.candidateSamples!==6)),skipped:[...skipped],codeSize:{rows:code.length,changed:code.filter(r=>r.base!==r.candidate).length,grown:code.filter(r=>r.candidate>r.base).length},confirmationCases:confirmation.filter(r=>r.unit==='ns/op').length,confirmedTiming:confirmed,resourceIncreases:resourceRows.length};
fs.writeFileSync(`${dir}/qualification.json`,JSON.stringify(q,null,2)+'\n');
console.log(JSON.stringify(q,null,2));
