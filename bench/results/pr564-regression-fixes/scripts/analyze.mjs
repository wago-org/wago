import fs from 'node:fs';
const out='/tmp/wago-pr564-regressions-zHTLww/full';
export const median = v => {const a=[...v].sort((a,b)=>a-b);return a.length%2 ? a[(a.length-1)/2] : (a[a.length/2-1]+a[a.length/2])/2;};
export function parse(path) {
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
  for(const rank of ranks)for(let k=n;k>0;k--)for(const [sum,count] of counts[k-1]) {
    counts[k].set(sum+rank,(counts[k].get(sum+rank)??0)+count);
  }
  let extreme=0,total=0;
  for(const [sum,count] of counts[n]){total+=count;if(Math.abs(sum-center)>=threshold)extreme+=count;}
  return extreme/total;
}
const all=[];
for(const mode of ['explicit','signals']) {
  const a=parse(`${out}/base-${mode}.bench`),b=parse(`${out}/final-${mode}.bench`);
  for(const [name,metrics] of a) for(const [unit,base] of Object.entries(metrics)) {
    const candidate=b.get(name)?.[unit];
    if(!candidate) { all.push({mode,name,unit,status:'missing candidate'});continue; }
    const old=median(base), now=median(candidate);
    all.push({mode,name,unit,base:old,candidate:now,delta:old!==0?(now/old-1)*100:now===0?0:null,baseSamples:base.length,candidateSamples:candidate.length,p:rankP(base,candidate)});
  }
  for(const [name] of b) if(!a.has(name))all.push({mode,name,status:'candidate-only'});
}
fs.writeFileSync(`${out}/comparison.json`,JSON.stringify(all,null,2)+'\n');
fs.writeFileSync(`${out}/comparison.tsv`,'bounds\tbenchmark\tunit\tbase_median\tcandidate_median\tdelta_percent\tp\tbase_n\tcandidate_n\tstatus\n'+all.map(r=>[r.mode,r.name,r.unit,r.base,r.candidate,r.delta,r.p,r.baseSamples,r.candidateSamples,r.status??'paired'].join('\t')).join('\n')+'\n');
const regressions=all.filter(r=>r.unit==='ns/op' && r.delta>0 && r.p!==null && r.p<0.05 && !/Wazero|_wazero/.test(r.name));
fs.writeFileSync(`${out}/regression-screen.json`,JSON.stringify(regressions,null,2)+'\n');
const summary=[];
const large=new Set(['json-as','lua','sqlite3','ruby','esbuild']);
const applications=new Set(['json-as','blake-as','utf-as','json-as-simd','blake-as-simd','utf-as-simd','coremark','blake3','qoi','lz4','zlib','zstd','wasm3','lua','sqlite3','ruby','esbuild']);
for(const mode of ['explicit','signals'])for(const stage of ['Decode','Validate','ValidateWorkers','Compile','CompileCompact','CompileFull','CompileWorkers','CompileFullWorkers','CompileMultiModuleThroughput','Instantiate','Exec','ExecParallel','PluginInstantiate','PluginExec']) {
  for(const unit of ['ns/op','B/op','allocs/op','B/call','allocs/call','code-B'])for(const corpus of ['all-including-ISA','default','five-large','applications']) {
    // PluginExec manually stops the allocation timer. Preserve raw values but
    // do not summarize its printed zero allocation counts as measurements.
    if(stage==='PluginExec'&&unit!=='ns/op')continue;
    const rows=all.filter(r=>{
      if(r.mode!==mode||!r.name.startsWith(`Benchmark${stage}/`)||r.unit!==unit||r.status)return false;
      const parts=r.name.split('/');
      const mod=parts[stage==='ExecParallel'?2:1].split('.')[0];
      return corpus==='all-including-ISA'||corpus==='default'&&!mod.startsWith('isa_')||corpus==='five-large'&&large.has(mod)||corpus==='applications'&&applications.has(mod);
    });
    const positive=rows.filter(r=>r.base>0&&r.candidate>0);
    if(rows.length)summary.push({mode,stage,corpus,unit,rows:rows.length,positiveRows:positive.length,allEqual:rows.every(r=>r.base===r.candidate),delta:positive.length?100*Math.expm1(positive.reduce((s,r)=>s+Math.log(r.candidate/r.base),0)/positive.length):rows.every(r=>r.base===r.candidate)?0:null});
  }
}
fs.writeFileSync(`${out}/summary.json`,JSON.stringify(summary,null,2)+'\n');
if(process.argv.includes('--confirm')) {
  const focused=[];
  for(const mode of ['explicit','signals']) {
    const ap=`${out}/confirmation-base-${mode}.txt`,bp=`${out}/confirmation-candidate-${mode}.txt`;
    if(!fs.existsSync(ap)||!fs.existsSync(bp))continue;
    const a=parse(ap),b=parse(bp);
    for(const [name,metrics]of a)for(const[unit,base]of Object.entries(metrics)) {
      const candidate=b.get(name)?.[unit];if(!candidate)continue;
      const old=median(base),now=median(candidate);
      focused.push({mode,name,unit,base:old,candidate:now,delta:old!==0?(now/old-1)*100:now===0?0:null,baseSamples:base.length,candidateSamples:candidate.length,p:rankP(base,candidate)});
    }
  }
  fs.writeFileSync(`${out}/confirmation-summary.json`,JSON.stringify(focused,null,2)+'\n');
}
console.log(JSON.stringify({metrics:all.length,summaryRows:summary.length,regressions:regressions.length}));
