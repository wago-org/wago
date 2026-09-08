import fs from 'node:fs';
import {spawn} from 'node:child_process';
const out='/tmp/wago-pr564-HbN430',repo='/home/jtenner/Projects/wago';
const go='/home/jtenner/.local/share/mise/installs/go/1.22.12/bin/go';
const ref='9d36019973201a19f9c9ebb0f10828b2fe2374aa';
const wine=JSON.parse(fs.readFileSync(`${out}/wine-tools.json`));
const env={...process.env,PATH:`/home/jtenner/.local/share/mise/installs/go/1.22.12/bin:${out}/pinned-tinygo/bin:${repo}/.tools/wabt-1.0.41-linux-x64/bin:${process.env.PATH}`,GOMAXPROCS:'8',GOTOOLCHAIN:'local',GOFLAGS:'-buildvcs=false',TINYGOROOT:`${out}/pinned-tinygo`,TINYGOCACHE:`${out}/pinned-tinygo-cache`,WAGO_CONFIG:`${out}/isolated-settings.json`,WAGO_SPEC_INTERPRETER:`${repo}/.tools/spec-interpreter-${ref}/wasm`,WAGO_SPEC_INTERPRETER_REVISION:ref,WINEPATH:wine.WINEPATH};
delete env.WAGO_HOME;delete env.GOROOT;
const tasks=[
  ['pinned-standalone',go,['test','./cli/manager/internal/standalone','-count=1','-timeout=15m','-v']],
  ['wine-bootstrap-curl',go,['test','.','-run','^TestWineCmdBootstrapDownloadsVerifiesAndExecutesInstaller$','-count=1','-timeout=5m','-v']],
  ['full-pinned',go,['test','./...','-count=1','-timeout=20m']],
];
fs.writeFileSync(`${out}/test-tool-commands.json`,JSON.stringify({tasks,envOverrides:Object.fromEntries(['PATH','GOMAXPROCS','GOTOOLCHAIN','GOFLAGS','TINYGOROOT','TINYGOCACHE','WAGO_CONFIG','WAGO_SPEC_INTERPRETER','WAGO_SPEC_INTERPRETER_REVISION','WINEPATH'].map(k=>[k,env[k]])),unset:['WAGO_HOME','GOROOT']},null,2)+'\n');
for(const[label,cmd,args]of tasks) {
  const fd=fs.openSync(`${out}/test-${label}.txt`,'w'),start=Date.now();
  console.log(`${new Date().toISOString()} ${label}`);
  const child=spawn(cmd,args,{cwd:repo,env,stdio:['ignore',fd,fd]});
  const code=await new Promise(resolve=>child.on('exit',(c,s)=>resolve(c??s)));fs.closeSync(fd);
  const status={label,cmd,args,code,seconds:(Date.now()-start)/1000};
  fs.writeFileSync(`${out}/test-${label}.status.json`,JSON.stringify(status,null,2)+'\n');
  console.log(status);if(code!==0)process.exitCode=1;
}
