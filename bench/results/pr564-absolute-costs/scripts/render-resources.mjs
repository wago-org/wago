import fs from 'node:fs';
import {median,rankP} from './stats.mjs';
const root='/home/jtenner/Projects/wago/.tmp/pr564-absolute-costs',out=`${root}/rendered`;
const csv=(headers,rows)=>headers.join(',')+'\n'+rows.map(r=>headers.map(h=>{
 const s=r[h]===undefined||r[h]===null?'':String(r[h]);return /[,"\n]/.test(s)?'"'+s.replaceAll('"','""')+'"':s;
}).join(',')).join('\n')+'\n';
const rows=[];
for(const run of fs.readdirSync(root).filter(s=>/^(instantiate-fixed-|compiler-fixed-|resource-(explicit|signals)-(warm|fixed))/.test(s))) {
 const dir=`${root}/${run}`;
 const identity=JSON.parse(fs.readFileSync(`${dir}/identity.json`));
 const comparison=JSON.parse(fs.readFileSync(`${dir}/comparison.json`));
 for(const r of comparison.filter(r=>r.label==='final'))rows.push({run,bounds:r.mode,benchmark:r.name,unit:r.unit,main_median:r.base,after_median:r.candidate,change_percent:r.delta,p:r.p,main_samples:r.baseSamples,after_samples:r.candidateSamples,prior_median:r.prior,after_vs_prior_percent:r.priorDelta,p_vs_prior:r.priorP,benchtime:identity.benchtime,note:identity.benchtime==='1x'?'single-operation memory check; not a timing claim':identity.benchtime.endsWith('x')?'equal operation counts; separate from warm repeats':'warm resource diagnostic; separate from full suite'});
 }
fs.writeFileSync(`${out}/resource-diagnostics-main-vs-after.csv`,csv(['run','bounds','benchmark','unit','main_median','after_median','change_percent','p','main_samples','after_samples','prior_median','after_vs_prior_percent','p_vs_prior','benchtime','note'],rows));
const status=fs.readFileSync(`${root}/full/status.jsonl`,'utf8').trim().split('\n').map(JSON.parse);
const peaks=status.map(r=>({bounds:r.mode,label:r.label,group:r.top,sample:r.sample,peak_rss_KiB:+fs.readFileSync(`${root}/full/${r.key}.txt.resource`,'utf8').match(/Maximum resident set size \(kbytes\):\s*(\d+)/)[1],key:r.key}));
fs.writeFileSync(`${out}/full-process-rss.csv`,csv(['bounds','label','group','sample','peak_rss_KiB','key'],peaks));
const summary=[];
for(const bounds of ['explicit','signals'])for(const group of [...new Set(peaks.map(r=>r.group))]) {
 const values=label=>peaks.filter(r=>r.bounds===bounds&&r.label===label&&r.group===group).map(r=>r.peak_rss_KiB);
 const a=values('base'),b=values('final');if(!a.length&&!b.length)continue;
 if(a.length!==6||b.length!==6)throw Error(`RSS coverage mismatch: ${bounds}/${group}`);
 summary.push({bounds,group,main_median_rss_KiB:median(a),after_median_rss_KiB:median(b),change_percent:100*(median(b)/median(a)-1),p:rankP(a,b),main_max_rss_KiB:Math.max(...a),after_max_rss_KiB:Math.max(...b),samples_each:a.length});
}
fs.writeFileSync(`${out}/full-process-rss-summary.csv`,csv(['bounds','group','main_median_rss_KiB','after_median_rss_KiB','change_percent','p','main_max_rss_KiB','after_max_rss_KiB','samples_each'],summary));
console.log({resourceMetrics:rows.length,rssProcesses:peaks.length,rssGroups:summary.length});
