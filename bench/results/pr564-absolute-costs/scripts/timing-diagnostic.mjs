import fs from 'node:fs';
import {spawn} from 'node:child_process';
const root='/home/jtenner/Projects/wago/.tmp/pr564-absolute-costs';
async function run(file,args=[],env={}) {
 const child=spawn(process.execPath,[`${root}/${file}`,...args],{env:{...process.env,...env},stdio:'inherit'});
 const code=await new Promise(r=>child.on('exit',(c,s)=>r(c??s)));
 if(code!==0)throw Error(`${file}: ${code}`);
}
const listed=JSON.parse(fs.readFileSync(`${root}/listed-cases.json`));
const selected=[];
for(const mode of ['explicit','signals']) {
 const rows=JSON.parse(fs.readFileSync(`${root}/confirm-${mode}/comparison.json`));
 const warnings=rows.filter(r=>r.label==='final'&&r.unit==='ns/op'&&r.delta>0&&(
  r.p!==null&&r.p<.05||(r.delta>5||r.base>=1e6)&&listed.some(old=>old.mode===mode&&old.name===r.name)
 )).sort((a,b)=>b.base-a.base);
 selected.push(...warnings);
 if(!warnings.length)continue;
 const dir=`timing-final-${mode}`;
 await run('focused.mjs',[dir],{CASES:JSON.stringify(warnings.map(r=>[mode,r.name])),LABELS:'base,before,final',SAMPLES:'24',BENCHTIME:'1s'});
 await run('focused-analysis.mjs',[dir]);
 await run('focused-report.mjs',[dir]);
}
fs.writeFileSync(`${root}/timing-diagnostic-screen.json`,JSON.stringify(selected,null,2)+'\n');
