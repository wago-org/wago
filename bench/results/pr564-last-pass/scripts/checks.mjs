import fs from 'node:fs';
import {spawn} from 'node:child_process';
const repo='/home/jtenner/Projects/wago',out=`${repo}/.tmp/pr564-last-pass`;
const env={...process.env,GOMAXPROCS:'8',PATH:`${repo}/.tools/wabt-1.0.41-linux-x64/bin:${process.env.PATH}`,WAGO_SPEC_INTERPRETER:`${repo}/.tools/spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa/wasm`,WAGO_SPEC_INTERPRETER_REVISION:'9d36019973201a19f9c9ebb0f10828b2fe2374aa'};delete env.GOROOT;
const qemu=`${repo}/.tools/qemu/qemu-aarch64-static`,go122='/home/jtenner/.local/share/mise/installs/go/1.22.12/bin/go';
const rows=[];
const commands=[
 ['native','go',['test','./src/...'],repo,{}],
 ['guard','go',['test','-tags','wago_guardpage','./src/...'],repo,{}],
 ['race','go',['test','-race','./src/core/compiler/backend/railshot/amd64','-run','TestCompileWorkers|TestParallel|TestMemoryCopyPatchScratch','-count=1'],repo,{}],
 ['arm64',go122,['test','-exec',qemu,'./src/core/compiler/backend/railshot/arm64','-count=1'],repo,{GOOS:'linux',GOARCH:'arm64',CGO_ENABLED:'0'}],
 ['arm64-guard',go122,['test','-tags','wago_guardpage','-exec',qemu,'./src/core/compiler/backend/railshot/arm64','-count=1'],repo,{GOOS:'linux',GOARCH:'arm64',CGO_ENABLED:'0'}],
 ['bench-tests','go',['test','./...'],`${repo}/bench`,{}],
 ...['explicit','signals'].map(mode=>[`audit-${mode}`,'go',['test',`-overlay=${repo}/.tmp/pr564-memory/final-closures/audit-final.json`,...(mode==='signals'?['-tags','wago_guardpage']:[]),'-run','^Test(AllExecutableCodeAudit|AbsoluteCostCodeAudit)$','-count=1','-v','-wago.bench.isa'],`${repo}/bench`,{WAGO_BOUNDS:mode}]),
];
for(const[name,command,args,cwd,extra]of commands){
 const fd=fs.openSync(`${out}/${name}.txt`,'w'),start=Date.now();
 const child=spawn(command,args,{cwd,env:{...env,...extra},stdio:['ignore',fd,fd]});
 const code=await new Promise(r=>child.on('exit',(c,s)=>r(c??s)));fs.closeSync(fd);
 rows.push({name,command,args,cwd,code,start:new Date(start).toISOString(),seconds:(Date.now()-start)/1000});
 fs.writeFileSync(`${out}/checks.json`,JSON.stringify(rows,null,2)+'\n');console.log(name,code,rows.at(-1).seconds);
 if(code!==0)throw Error(name);
}
const audit=[];
for(const mode of ['explicit','signals']){
 const parse=p=>Object.fromEntries([...fs.readFileSync(p,'utf8').matchAll(/(?:EXEC_CODE|CODE) (\S+) bytes=(\d+) sha256=([a-f0-9]{64})/g)].map(m=>[m[1],{bytes:+m[2],sha256:m[3]}]));
 const base=parse(`${repo}/.tmp/pr564-memory/final-closures/audit-compact-base-${mode}.txt`),after=parse(`${out}/audit-${mode}.txt`);
 if(Object.keys(base).length!==56||Object.keys(after).length!==56)throw Error('Code audit coverage');
 for(const[name,b]of Object.entries(base)){
  const a=after[name],equal=JSON.stringify(b)===JSON.stringify(a);audit.push({mode,name,base:b,after:a,equal});
 }
}
fs.writeFileSync(`${out}/code-audit.json`,JSON.stringify(audit,null,2)+'\n');
if(audit.some(r=>!r.equal))throw Error('Native output changed');
console.log('112 native-code pairs identical to main');
