import fs from 'node:fs';
export const median = v => {const a=[...v].sort((a,b)=>a-b);return a.length%2 ? a[(a.length-1)/2] : (a[a.length/2-1]+a[a.length/2])/2;};
export function parse(path) {
 const rows=new Map();
 for(const line of fs.readFileSync(path,'utf8').split('\n')) {
  const p=line.trim().split(/\s+/);
  if(!/^Benchmark/.test(p[0])||!/^\d+$/.test(p[1])||p.length<4)continue;
  const name=p[0].replace(/-\d+$/,'');
  if(!rows.has(name))rows.set(name,{});
  const sample={};
  for(let i=2;i+1<p.length;i+=2)if(Number.isFinite(+p[i])) {
   (rows.get(name)[p[i+1]]??=[]).push(+p[i]);sample[p[i+1]]=+p[i];
  }
  if(sample['calls/batch']>0)for(const [source,target] of [['B/op','B/call'],['allocs/op','allocs/call']]) {
   if(source in sample)(rows.get(name)[target]??=[]).push(sample[source]/sample['calls/batch']);
  }
 }
 return rows;
}
export function rankP(a,b) {
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
 for(const rank of ranks)for(let k=n;k>0;k--)for(const [sum,count] of counts[k-1])counts[k].set(sum+rank,(counts[k].get(sum+rank)??0)+count);
 let extreme=0,total=0;
 for(const [sum,count]of counts[n]){total+=count;if(Math.abs(sum-center)>=threshold)extreme+=count;}
 return extreme/total;
}
