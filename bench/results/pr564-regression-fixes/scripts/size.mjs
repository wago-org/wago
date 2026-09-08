import fs from 'node:fs';
import path from 'node:path';
import {spawn,execFileSync} from 'node:child_process';
const out='/tmp/wago-pr564-regressions-zHTLww',old='/tmp/wago-pr564-HbN430',repo='/home/jtenner/Projects/wago';
const go='/home/jtenner/.local/share/mise/installs/go/1.22.12/bin/go',tinygo=`${old}/pinned-tinygo/bin/tinygo`;
const env={...process.env,PATH:`${path.dirname(go)}:${path.dirname(tinygo)}:${process.env.PATH}`,GOMAXPROCS:'8',GOTOOLCHAIN:'local',GOFLAGS:'-buildvcs=false',CGO_ENABLED:'0',GOOS:'linux',GOARCH:'amd64',TINYGOROOT:`${old}/pinned-tinygo`,TINYGOCACHE:`${old}/pinned-tinygo-cache`}; delete env.GOROOT;
const prior=JSON.parse(fs.readFileSync(`${old}/build-size.json`));
const rows=[];
if(fs.existsSync(`${out}/size.json`))fs.copyFileSync(`${out}/size.json`,`${out}/size-initial.json`);
for(const [name,tags,tiny] of [['manager','',false],['runtime-standard','wago_runtime',false],['runtime-minimal','wago_runtime,wago_minimal',false],['runtime-minimal-tiny','wago_runtime,wago_lean,wago_minimal',true]]) {
 const cmd=tiny?tinygo:go, target=`${out}/size-${name}`;
 const args=tiny?['build','-scheduler=tasks','-no-debug','-opt=z','-gc=conservative','-ldflags','-X main.version=0.0.0']:['build','-buildvcs=false','-trimpath','-ldflags=-s -w -X main.version=0.0.0'];
 if(tags)args.push('-tags',tags);args.push('-o',target,'./cli/wago');
 const fd=fs.openSync(target+'.txt','w'),start=Date.now();
 const child=spawn(cmd,args,{cwd:repo,env,stdio:['ignore',fd,fd]});
 const code=await new Promise(r=>child.on('exit',(c,s)=>r(c??s)));fs.closeSync(fd);
 if(code!==0)throw Error(`${name}: ${code}`);
 if(tiny)execFileSync('strip',['-s','--strip-section-headers','--remove-section=.eh_frame','--remove-section=.eh_frame_hdr','--remove-section=.comment',target]);
 const base=prior.rows.find(r=>r.label==='base'&&r.name===name),before=prior.rows.find(r=>r.label==='candidate'&&r.name===name);
 const bytes=fs.statSync(target).size;
 const row={name,cmd,args,code,seconds:(Date.now()-start)/1000,baseBytes:base.bytes,beforeBytes:before.bytes,bytes,budget:base.budget}; rows.push(row); console.log(row);
 fs.writeFileSync(`${out}/size.json`,JSON.stringify(rows,null,2));
 if(name!=='manager') {
  const result=execFileSync(target,['run','tests/fixtures/wasm/fib.wasm','30'],{cwd:repo,env:{...env,WAGO_HOME:`${out}/isolated-cli`},encoding:'utf8'});
  fs.writeFileSync(target+'.smoke.txt',result);if(!result.includes('832040'))throw Error(`${name} wrong fib result`);
 }
}
