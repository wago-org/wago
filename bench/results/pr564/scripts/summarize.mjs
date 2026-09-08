import fs from 'node:fs';
const out='/tmp/wago-pr564-HbN430';
const records=fs.readFileSync(`${out}/paired/status.jsonl`,'utf8').trim().split('\n').filter(Boolean).map(JSON.parse);
const groups=new Map();
const skips=[];
for(const row of records) {
  const k=`${row.label}-${row.mode}`;
  if(!groups.has(k))groups.set(k,{label:row.label,mode:row.mode,processes:0,failed:[],seconds:0,maxRSSKiB:0,largestProcess:''});
  const g=groups.get(k);g.processes++;g.seconds+=row.seconds;
  if(row.code!==0)g.failed.push(row);
  const resource=fs.readFileSync(`${out}/paired/${row.key}.resource.txt`,'utf8');
  const rss=Number(resource.match(/Maximum resident set size \(kbytes\): (\d+)/)?.[1]??0);
  if(rss>g.maxRSSKiB){g.maxRSSKiB=rss;g.largestProcess=row.key;}
  const lines=fs.readFileSync(`${out}/paired/${row.key}.txt`,'utf8').split('\n');
  for(let i=0;i<lines.length;i++)if(/--- SKIP|skipping|not present|absent/.test(lines[i]))skips.push({key:row.key,line:i+1,text:lines[i],previous:lines[i-1]??''});
}
const result={groups:[...groups.values()],skips};
fs.writeFileSync(`${out}/run-summary.json`,JSON.stringify(result,null,2)+'\n');
console.log(JSON.stringify({groups:result.groups,skipLines:skips.length},null,2));
