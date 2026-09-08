import fs from 'node:fs';
import {spawn} from 'node:child_process';
const root='/tmp/wago-pr564-regressions-zHTLww';
async function run(file,args=[],env={}) {
 const child=spawn(process.execPath,[`${root}/${file}`,...args],{env:{...process.env,...env},stdio:'inherit'});
 const code=await new Promise(r=>child.on('exit',(c,s)=>r(c??s)));
 if(code!==0)throw Error(`${file}: ${code}`);
}
// Start only after the complete full suite has drained; never overlap timers.
for(const mode of ['explicit','signals'])for(const label of ['base','final']) {
 if(!fs.existsSync(`${root}/full/${label}-${mode}.bench`))throw Error('Full suite has not completed');
}
await run('analyze.mjs');
const rows=JSON.parse(fs.readFileSync(`${root}/full/comparison.json`));
const warnings=rows.filter(r=>r.unit==='ns/op'&&r.delta>0&&(r.p!==null&&r.p<.05||r.delta>5)&&!/Wazero|_wazero/.test(r.name));
fs.writeFileSync(`${root}/full/confirmation-screen.json`,JSON.stringify(warnings,null,2)+'\n');
for(const mode of ['explicit','signals']) {
 const cases=warnings.filter(r=>r.mode===mode).map(r=>[mode,r.name]);
 if(!cases.length)continue;
 console.log(`Confirming ${cases.length} ${mode} cases`);
 await run('focused.mjs',[`confirm-${mode}`],{CASES:JSON.stringify(cases),LABELS:'base,final',SAMPLES:'12',BENCHTIME:'500ms'});
}
const dirs=['explicit','signals'].map(mode=>`confirm-${mode}`).filter(dir=>fs.existsSync(`${root}/${dir}`));
if(dirs.length)await run('focused-analysis.mjs',dirs);
