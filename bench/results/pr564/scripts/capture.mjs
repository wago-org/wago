import fs from 'node:fs';
import {execFileSync} from 'node:child_process';
const out = '/tmp/wago-pr564-HbN430';
const run = (...args) => execFileSync(args[0], args.slice(1), {encoding:'utf8'});
const pr = JSON.parse(run('gh','pr','view','564','--json','number,title,body,headRefName,headRefOid,baseRefName,url,state,comments,reviews'));
fs.writeFileSync(`${out}/pr.json`, JSON.stringify(pr,null,2)+'\n');
fs.writeFileSync(`${out}/pr-description.md`,pr.body+'\n');
const report = run('git','show',`${pr.headRefOid}:REPORT.md`);
fs.writeFileSync(`${out}/original-REPORT.md`,report);
const inventory = [];
for (const [source, content] of [['PR description',pr.body],['REPORT.md',report]]) {
  let section = '';
  content.split('\n').forEach((text,i) => {
    if (/^#+ /.test(text)) section = text;
    if (/\d/.test(text) && /%|\|.*\d|\b(?:bytes|bits|ms|us|ns|KiB|MiB|B\/op|allocs|samples|locals)\b/.test(text)) inventory.push({source,line:i+1,section,text});
  });
}
fs.writeFileSync(`${out}/metric-inventory.json`,JSON.stringify(inventory,null,2)+'\n');
fs.writeFileSync(`${out}/environment.txt`,run('go','version')+run('uname','-a')+run('lscpu')+'\nBASE '+run('git','rev-parse','origin/main')+'PR '+pr.headRefOid+'\nGOMAXPROCS=8; GOGC=100; benchtime=100ms; count=6; ISA included\n');
console.log(`Saved ${inventory.length} metric-bearing lines, full source snapshots, and host metadata in ${out}`);
