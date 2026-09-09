import fs from 'node:fs';
import path from 'node:path';
import {createHash} from 'node:crypto';
import {spawn,spawnSync,execFileSync} from 'node:child_process';
const repo='/home/jtenner/Projects/wago',root=`${repo}/.tmp/pr564-absolute-costs`,dir=`${root}/size`;
fs.mkdirSync(dir,{recursive:true});
const go='/home/jtenner/.local/share/mise/installs/go/1.22.12/bin/go';
let tinygo='/home/jtenner/.local/share/mise/installs/tinygo/latest/tinygo/bin/tinygo';
const baseEnv={...process.env,PATH:`${path.dirname(go)}:${process.env.PATH}`,GOMAXPROCS:'8',GOTOOLCHAIN:'local',GOFLAGS:'-buildvcs=false',CGO_ENABLED:'0',GOOS:'linux',GOARCH:'amd64'};delete baseEnv.GOROOT;
let tinyVersion='';
try{tinyVersion=execFileSync(tinygo,['version'],{env:baseEnv,encoding:'utf8'}).trim();}catch{}
if(!tinyVersion.startsWith('tinygo version 0.41.1 ')) {
 const release=JSON.parse(fs.readFileSync(`${repo}/bench/results/pr564/pinned-tinygo-release.json`));
 const archive=`${root}/${release.asset.name}`;
 if(!fs.existsSync(archive))execFileSync('curl',['-fL','--retry','2','-o',archive,release.asset.browser_download_url],{stdio:'inherit'});
 const digest=createHash('sha256').update(fs.readFileSync(archive)).digest('hex');
 if(`sha256:${digest}`!==release.asset.digest)throw Error('TinyGo digest mismatch');
 const pinned=`${root}/pinned-tinygo`;
 if(!fs.existsSync(pinned)) {fs.mkdirSync(pinned);execFileSync('tar',['-xzf',archive,'-C',pinned,'--strip-components=1']);}
 tinygo=`${pinned}/bin/tinygo`;
}
const env={...baseEnv,TINYGOROOT:path.dirname(path.dirname(tinygo)),TINYGOCACHE:`${root}/pinned-tinygo-cache`};
tinyVersion=execFileSync(tinygo,['version'],{env,encoding:'utf8'}).trim();
if(!tinyVersion.startsWith('tinygo version 0.41.1 '))throw Error(tinyVersion);
const budgets=Object.fromEntries(fs.readFileSync(`${repo}/scripts/release-size-budgets.tsv`,'utf8').trim().split('\n').filter(l=>!l.startsWith('#')).map(l=>{const[a,b]=l.split('\t');return[a,+b];}));
const rows=fs.existsSync(`${root}/size.json`)?JSON.parse(fs.readFileSync(`${root}/size.json`)).rows:[];
for(const [name,tags,tiny] of [['manager','',false],['runtime-standard','wago_runtime',false],['runtime-minimal','wago_runtime,wago_minimal',false],['runtime-minimal-tiny','wago_runtime,wago_lean,wago_minimal',true]])for(const label of ['base','before','final']) {
 const cwd=label==='base'?`${root}/main`:label==='before'?`${root}/prior`:repo,target=`${dir}/${label}-${name}`;
 if(rows.some(r=>r.label===label&&r.name===name)&&fs.existsSync(target+'.smoke.json'))continue;
 const args=tiny?['build','-scheduler=tasks','-no-debug','-opt=z','-gc=conservative','-ldflags','-X main.version=0.0.0']:['build','-buildvcs=false','-trimpath','-ldflags=-s -w -X main.version=0.0.0'];
 if(tags)args.push('-tags',tags);args.push('-o',target,'./cli/wago');
 const fd=fs.openSync(target+'.txt','w'),start=Date.now();
 const child=spawn(tiny?tinygo:go,args,{cwd,env,stdio:['ignore',fd,fd]});
 const code=await new Promise(r=>child.on('exit',(c,s)=>r(c??s)));fs.closeSync(fd);
 if(code!==0)throw Error(`${label}/${name}: ${code}`);
 if(tiny)execFileSync('strip',['-s','--strip-section-headers','--remove-section=.eh_frame','--remove-section=.eh_frame_hdr','--remove-section=.comment',target]);
 const bytes=fs.statSync(target).size;
 const row={label,name,cmd:tiny?tinygo:go,args,code,seconds:(Date.now()-start)/1000,bytes,budget:budgets[name],sha256:createHash('sha256').update(fs.readFileSync(target)).digest('hex')};
 const previous=rows.findIndex(r=>r.label===label&&r.name===name);if(previous>=0)rows[previous]=row;else rows.push(row);console.log(row);
 fs.writeFileSync(`${root}/size.json`,JSON.stringify({go:execFileSync(go,['version'],{env,encoding:'utf8'}).trim(),tinygo:tinyVersion,rows},null,2));
 if(name!=='manager') {
  const result=spawnSync(target,['run','tests/fixtures/wasm/fib.wasm','30'],{cwd,env:{...env,WAGO_HOME:`${root}/isolated-cli`},encoding:'utf8',timeout:30000});
  fs.writeFileSync(target+'.smoke.txt',(result.stdout??'')+(result.stderr??''));
  const smoke={status:result.status,signal:result.signal,error:result.error?.message,passed:result.status===0&&(result.stdout??'').includes('832040')};
  fs.writeFileSync(target+'.smoke.json',JSON.stringify(smoke,null,2)+'\n');console.log({label,name,smoke});
 }
}
