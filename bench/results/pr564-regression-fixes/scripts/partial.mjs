import fs from 'node:fs';
const out='/tmp/wago-pr564-regressions-zHTLww/full';
const mode=process.argv[2]||'explicit';
const median = v => {const a=[...v].sort((a,b)=>a-b);return a.length%2 ? a[(a.length-1)/2] : (a[a.length/2-1]+a[a.length/2])/2;};
function parse(path) {
  const rows=new Map();
  for (const line of fs.readFileSync(path,'utf8').split('\n')) {
    const parts=line.trim().split(/\s+/);
    if(!/^Benchmark/.test(parts[0])||!/^\d+$/.test(parts[1])||parts.length<4)continue;
    const name=parts[0].replace(/-\d+$/,'');
    if(!rows.has(name)) rows.set(name,{});
    const sample={};
    for(let i=2;i+1<parts.length;i+=2) {
      const value=Number(parts[i]);
      if(!Number.isFinite(value)) continue;
      (rows.get(name)[parts[i+1]]??=[]).push(value);
      sample[parts[i+1]]=value;
    }
    // The execution harness normalizes time, but Go's allocation counters
    // remain per timed batch. Keep those raw metrics and add per-call values
    // derived from each sample's reported batch size before taking medians.
    if(sample['calls/batch']>0)for(const [source,target] of [['B/op','B/call'],['allocs/op','allocs/call']]) {
      if(source in sample)(rows.get(name)[target]??=[]).push(sample[source]/sample['calls/batch']);
    }
  }
  return rows;
}
// Exact two-sided permutation test of rank sums, including average-rank ties.
// Dynamic programming counts partitions of the doubled integer ranks. This
// works for both the six-pair screen and twelve-pair focused confirmation.
function rankP(a,b) {
  if(a.length!==b.length||a.length<6)return null;
  const values=[...a.map(v=>({v,a:true})),...b.map(v=>({v,a:false}))].sort((x,y)=>x.v-y.v);
  const ranks=[];let observed=0;
  for(let i=0;i<values.length;) {
    let j=i+1;while(j<values.length&&values[j].v===values[i].v)j++;
    const rank=i+1+j;
    for(let k=i;k<j;k++){ranks.push(rank);if(values[k].a)observed+=rank;}
    i=j;
  }
  const n=a.length,center=n*(2*n+1),threshold=Math.abs(observed-center)-1e-9;
  const counts=Array.from({length:n+1},()=>new Map());counts[0].set(0,1);
  for(const rank of ranks)for(let k=n;k>0;k--)for(const [sum,count] of counts[k-1]) {
    counts[k].set(sum+rank,(counts[k].get(sum+rank)??0)+count);
  }
  let extreme=0,total=0;
  for(const [sum,count] of counts[n]){total+=count;if(Math.abs(sum-center)>=threshold)extreme+=count;}
  return extreme/total;
}

const inputs={base:new Map(),final:new Map()};
const statuses=fs.readFileSync(out+'/status.jsonl','utf8').trim().split('\n').map(JSON.parse);
const counts={};for(const r of statuses)if(r.mode===mode&&r.code===0)counts[r.top]=(counts[r.top]||0)+1;
for(const r of statuses) {
 if(r.mode!==mode||r.code!==0||counts[r.top]!==12)continue;
 for(const [name,metrics] of parse(out+'/'+r.key+'.txt')) {
  const dst=inputs[r.label];if(!dst.has(name))dst.set(name,{});
  for(const [unit,values] of Object.entries(metrics))(dst.get(name)[unit]??=[]).push(...values);
 }
}
const rows=[];
for(const [name,metrics] of inputs.base)for(const [unit,a] of Object.entries(metrics)) {
 const b=inputs.final.get(name)?.[unit];if(!b)throw Error(name+' missing '+unit);
 if(a.length!==6||b.length!==6)throw Error(name+' count '+a.length+'/'+b.length);
 const base=median(a),final=median(b);
 rows.push({mode,name,unit,base,final,delta:base?100*(final/base-1):final===0?0:null,p:rankP(a,b),baseSamples:a.length,finalSamples:b.length});
}
fs.writeFileSync(out+`/partial-${mode}-comparison.json`,JSON.stringify(rows,null,2));
const warnings=rows.filter(r=>r.unit==='ns/op'&&!/Wazero|_wazero/.test(r.name)&&r.delta>0&&(r.p<.05||r.delta>5)).sort((a,b)=>b.delta-a.delta);
fs.writeFileSync(out+`/partial-${mode}-warnings.json`,JSON.stringify(warnings,null,2));
console.log(JSON.stringify({paired:rows.length,warnings:warnings.length,largest:warnings.slice(0,20)},null,2));
