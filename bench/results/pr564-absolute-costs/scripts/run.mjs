import fs from 'node:fs';
import {spawn,execFileSync} from 'node:child_process';
const repo='/home/jtenner/Projects/wago',root=`${repo}/.tmp/pr564-absolute-costs`;
async function run(file,args=[],env={}) {
 const child=spawn(process.execPath,[`${root}/${file}`,...args],{cwd:repo,env:{...process.env,...env},stdio:'inherit'});
 const code=await new Promise(r=>child.on('exit',(c,s)=>r(c??s)));
 if(code!==0)throw Error(`${file}: ${code}`);
}
const command=(cmd,args,cwd=repo)=>execFileSync(cmd,args,{cwd,encoding:'utf8'}).trim();
const metadata={started:new Date().toISOString(),main:command('git',['rev-parse','HEAD'],`${root}/main`),prior:command('git',['rev-parse','HEAD'],`${root}/prior`),after:command('git',['rev-parse','HEAD']),kernel:command('uname',['-a']),cpu:command('lscpu',[]),go:command('go',['version']),env:command('go',['env','GOOS','GOARCH','CGO_ENABLED','GOAMD64','GOFLAGS']),boot:fs.readFileSync('/proc/sys/kernel/random/boot_id','utf8').trim(),binaries:{},note:'Fresh post-reboot run. Incomplete pre-reboot raw logs in /tmp were lost and are excluded.'};
for(const label of ['base','before','final'])for(const mode of ['explicit','signals'])metadata.binaries[`${label}-${mode}`]=command('go',['version','-m',`${root}/${label}-${mode}.test`]);
if(!fs.existsSync(`${root}/metadata.json`))fs.writeFileSync(`${root}/metadata.json`,JSON.stringify(metadata,null,2));
const cases=[['explicit','BenchmarkExec/coremark.coremark_run'],['signals','BenchmarkPluginExec/wasm3'],['signals','BenchmarkPluginInstantiate/ruby'],['signals','BenchmarkPluginInstantiate/wasm3']];
await run('focused.mjs',['largest'],{CASES:JSON.stringify(cases),LABELS:'base,before,final',SAMPLES:'12',BENCHTIME:'500ms'});
await run('focused-analysis.mjs',['largest']);
await run('full.mjs');
await run('repeats.mjs');
