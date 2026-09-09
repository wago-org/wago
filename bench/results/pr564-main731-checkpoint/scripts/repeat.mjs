import fs from 'node:fs';
import {spawn} from 'node:child_process';
const root='/home/jtenner/Projects/wago/.tmp/pr564-memory/final731';
const rows=JSON.parse(fs.readFileSync(`${root}/full/comparison.json`));
const listed=JSON.parse(fs.readFileSync(`${root}/listed-cases.json`));
// Selection is recorded before new samples: every original user case, every
// positive timing screen with p<.05, and large-time increases over 3%.
// Also repeat material allocation screens: significant increases exceeding
// 0.5% for at least 64 KiB/op, or at least eight allocations per operation.
// Repeat large absolute increases even without significance: 128 KiB/op or
// 128 allocations/op. The goal is to examine the largest memory costs too.
const selected=new Map();
for(const r of listed)selected.set(`${r.mode}/${r.name}`,{mode:r.mode,name:r.name,reason:'user-listed'});
for(const r of rows)if(r.unit==='ns/op'&&!/Wazero|_wazero/.test(r.name)&&r.delta>0&&((r.p!==null&&r.p<.05)||(r.base>=1e6&&r.delta>3)))selected.set(`${r.mode}/${r.name}`,{...r,reason:selected.get(`${r.mode}/${r.name}`)?.reason||'timing-screen'});
for(const r of rows)if(!/Wazero|Exec/.test(r.name)&&r.delta>0&&((r.p!==null&&r.p<.05&&((r.unit==='B/op'&&r.base>=65536&&r.delta>0.5)||(r.unit==='allocs/op'&&r.candidate-r.base>=8)))||(r.unit==='B/op'&&r.candidate-r.base>=131072)||(r.unit==='allocs/op'&&r.candidate-r.base>=128)))selected.set(`${r.mode}/${r.name}`,{...r,reason:selected.get(`${r.mode}/${r.name}`)?.reason||'allocation-screen'});
fs.writeFileSync(`${root}/repeat-selection.json`,JSON.stringify([...selected.values()],null,2)+'\n');
for(const mode of ['explicit','signals']) {
 const cases=[...selected.values()].filter(r=>r.mode===mode).map(r=>[mode,r.name]);
 if(!cases.length)continue;
 const child=spawn(process.execPath,[`${root}/focused.mjs`,`confirm-${mode}`],{env:{...process.env,LABELS:'base,final',SAMPLES:'12',BENCHTIME:'500ms',CASES:JSON.stringify(cases)},stdio:'inherit'});
 if(await new Promise(r=>child.on('exit',r))!==0)throw Error(mode);
 const report=spawn(process.execPath,[`${root}/focused-report.mjs`,`confirm-${mode}`],{stdio:'inherit'});
 if(await new Promise(r=>report.on('exit',r))!==0)throw Error('report');
}
