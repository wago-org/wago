import fs from 'node:fs';
import path from 'node:path';
import {spawn,execFileSync} from 'node:child_process';
const out='/tmp/wago-pr564-HbN430',repo='/home/jtenner/Projects/wago';
const go=process.env.QUALIFY_GO_BIN??'/home/jtenner/.local/share/mise/installs/go/1.22.12/bin/go';
const tinygo=process.env.QUALIFY_TINYGO_BIN??`${out}/pinned-tinygo/bin/tinygo`;
const dir=`${out}/build-size`;fs.mkdirSync(dir,{recursive:true});
const env={...process.env,PATH:`${path.dirname(go)}:${path.dirname(tinygo)}:${process.env.PATH}`,GOMAXPROCS:'8',GOTOOLCHAIN:'local',GOFLAGS:'-buildvcs=false',CGO_ENABLED:'0',GOOS:'linux',GOARCH:'amd64',TINYGOROOT:path.dirname(path.dirname(tinygo)),TINYGOCACHE:`${out}/pinned-tinygo-cache`};
delete env.GOROOT;
const budgets=Object.fromEntries(fs.readFileSync(`${repo}/scripts/release-size-budgets.tsv`,'utf8').trim().split('\n').filter(l=>!l.startsWith('#')).map(l=>{const[a,b]=l.split('\t');return[a,Number(b)];}));
const profiles=[['manager','',false],['runtime-standard','wago_runtime',false],['runtime-minimal','wago_runtime,wago_minimal',false],['runtime-minimal-tiny','wago_runtime,wago_lean,wago_minimal',true]];
if(fs.existsSync(`${out}/build-size.json`)&&!fs.existsSync(`${out}/build-size-initial.json`)) {
  fs.copyFileSync(`${out}/build-size.json`,`${out}/build-size-initial.json`);
  for(const name of fs.readdirSync(dir).filter(n=>n.endsWith('.txt')&&!n.startsWith('initial-')))fs.copyFileSync(`${dir}/${name}`,`${dir}/initial-${name}`);
}
const rows=[];
for(const [name,tags,tiny]of profiles)for(const label of ['base','candidate']) {
  const cwd=label==='base'?`${out}/main`:repo;
  const target=`${dir}/${label}-${name}`;
  const args=tiny?['build','-scheduler=tasks','-no-debug','-opt=z','-gc=conservative','-ldflags','-X main.version=0.0.0']:['build','-buildvcs=false','-trimpath','-ldflags=-s -w -X main.version=0.0.0'];
  if(tags)args.push('-tags',tags);
  args.push('-o',target,'./cli/wago');
  const fd=fs.openSync(`${target}.txt`,'w'),start=Date.now();
  console.log(`${new Date().toISOString()} ${label}/${name}`);
  const child=spawn(tiny?tinygo:go,args,{cwd,env,stdio:['ignore',fd,fd]});
  const code=await new Promise(resolve=>{child.on('error',err=>resolve(String(err)));child.on('exit',(c,s)=>resolve(c??s));});fs.closeSync(fd);
  let bytes=null,stripError=null;
  if(code===0) {
    if(tiny) {
      try {
        const help=execFileSync('strip',['--help'],{encoding:'utf8'});
        if(help.includes('--strip-section-headers'))execFileSync('strip',['-s','--strip-section-headers','--remove-section=.eh_frame','--remove-section=.eh_frame_hdr','--remove-section=.comment',target]);
        else execFileSync('llvm-strip',['--strip-sections',target]);
      } catch(error){stripError=String(error);process.exitCode=1;}
    }
    bytes=fs.statSync(target).size;
  }else process.exitCode=1;
  const r={label,name,tool:tiny?'tinygo':'go',cmd:tiny?tinygo:go,args,code,stripError,bytes,budget:budgets[name],seconds:(Date.now()-start)/1000};rows.push(r);
  console.log(r);
  fs.writeFileSync(`${out}/build-size.json`,JSON.stringify({go:execFileSync(go,['version'],{env,encoding:'utf8'}).trim(),tinygo:fs.existsSync(tinygo)?execFileSync(tinygo,['version'],{env,encoding:'utf8'}).trim():'missing',envOverrides:{GOTOOLCHAIN:env.GOTOOLCHAIN,GOFLAGS:env.GOFLAGS,CGO_ENABLED:env.CGO_ENABLED,TINYGOROOT:env.TINYGOROOT,TINYGOCACHE:env.TINYGOCACHE},rows},null,2)+'\n');
}
fs.writeFileSync(`${out}/build-size.tsv`,'profile\tbase_bytes\tcandidate_bytes\tdelta_bytes\tdelta_percent\tbudget_bytes\tbase_status\tcandidate_status\n'+profiles.map(([name])=>{
  const a=rows.find(r=>r.label==='base'&&r.name===name),b=rows.find(r=>r.label==='candidate'&&r.name===name);
  const have=a?.bytes!==null&&b?.bytes!==null;
  return[name,a?.bytes,b?.bytes,have?b.bytes-a.bytes:null,have?100*(b.bytes/a.bytes-1):null,b?.budget,a?.code,b?.code].map(v=>v??'').join('\t');
}).join('\n')+'\n');
