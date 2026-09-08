import fs from 'node:fs';
import {execFileSync} from 'node:child_process';
const out='/tmp/wago-pr564-HbN430';
const base='447f057115ee04d9e58580061dbee696becec21f';
const head='ab29bf3a9ad215833b5138220de6cd7190461a78';
const commits=execFileSync('git',['log','--reverse','--format=commit %H%n%s%n%b',`${base}..${head}`],{encoding:'utf8'});
fs.writeFileSync(`${out}/pr-commit-messages.txt`,commits);
const inventory=[];
const sources=['pr-description.md','original-REPORT.md','pr-commit-messages.txt','reviewed-ci-size.md','reviewed-ci-size-profiles.tsv','reviewed-ci-size-symbols.tsv'].filter(s=>fs.existsSync(`${out}/${s}`));
for(const source of sources) {
  let section='',commit='',tableHeader='';
  const lines=fs.readFileSync(`${out}/${source}`,'utf8').split('\n');
  if(source.endsWith('.tsv'))tableHeader=lines[0];
  lines.forEach((text,index)=>{
    if(/^#+ /.test(text)){section=text;tableHeader='';}
    if(/^commit /.test(text)){commit=text.slice(7);section=lines[index+1]??'';}
    if(text.startsWith('|')&&lines[index+1]?.match(/^\|[- :|]+\|$/))tableHeader=text;
    // Include every number-bearing line, not just a guessed unit vocabulary.
    // Also preserve non-numeric equality and resource claims. Source snapshots
    // retain surrounding prose and definitions; entries are claims, not proof.
    if(!/\d/.test(text)&&!/(?:unchanged|identical|parity|allocation|heap|latency|code.size|peak.memory)/i.test(text))return;
    const metrics=[];
    if(/latency|\b(?:[num]?s|ms)\b|compile|decode|validat|execution|throughput/i.test(text+' '+tableHeader))metrics.push('time');
    if(/heap|B\/op|KiB|MiB|memory|footprint|\bRSS\b/i.test(text+' '+tableHeader))metrics.push('memory');
    if(/alloc/i.test(text+' '+tableHeader))metrics.push('allocations');
    if(/code|native|\bbytes?\b|\bB\b/i.test(text+' '+tableHeader))metrics.push('code-or-storage-size');
    if(/sample|pair|p[=<]|signific|flat|checkpoint|commit|HEAD|base|main/i.test(text))metrics.push('method-or-checkpoint');
    inventory.push({source,line:index+1,section,...(commit?{commit}:{}),...(tableHeader&&(text.startsWith('|')||source.endsWith('.tsv'))?{tableHeader}:{}),metrics,text});
  });
}
fs.writeFileSync(`${out}/metric-inventory.json`,JSON.stringify({reviewedBase:base,reviewedHead:head,note:'Historical reported claims, not current qualification. All numeric source lines are indexed to avoid losing unitless table values, continuation lines, or standalone byte counts.',entries:inventory},null,2)+'\n');
fs.writeFileSync(`${out}/metric-inventory.tsv`,'source\tline\tsection\tcommit\tcategories\ttable_header\tclaim\n'+inventory.map(r=>[r.source,r.line,r.section,r.commit??'',r.metrics.join(','),r.tableHeader??'',r.text].map(v=>String(v).replaceAll('\t',' ')).join('\t')).join('\n')+'\n');
console.log(JSON.stringify({entries:inventory.length,sources:Object.fromEntries(sources.map(s=>[s,inventory.filter(r=>r.source===s).length]))}));
