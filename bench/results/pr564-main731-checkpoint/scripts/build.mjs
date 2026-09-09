import fs from 'node:fs';
import {spawn,execFileSync} from 'node:child_process';
import {createHash} from 'node:crypto';
const repo='/home/jtenner/Projects/wago',root=`${repo}/.tmp/pr564-memory/final731`,main=`${repo}/.tmp/pr564-memory/main-latest`;
const env={...process.env,GOMAXPROCS:'8'};delete env.GOROOT;
const identity={created:new Date().toISOString(),main:execFileSync('git',['rev-parse','HEAD'],{cwd:main,encoding:'utf8'}).trim(),head:execFileSync('git',['rev-parse','HEAD'],{cwd:repo,encoding:'utf8'}).trim(),go:execFileSync('go',['version'],{env,encoding:'utf8'}).trim(),binaries:{}};
for(const [label,cwd]of [['base',main],['final',repo]])for(const mode of ['explicit','signals']) {
 const bin=`${root}/${label}-${mode}.test`,args=['test','-c','-o',bin];if(mode==='signals')args.push('-tags','wago_guardpage');args.push('.');
 const fd=fs.openSync(`${bin}.build.txt`,'w'),start=Date.now();
 const child=spawn('go',args,{cwd:`${cwd}/bench`,env,stdio:['ignore',fd,fd]});
 const code=await new Promise(r=>child.on('exit',r));fs.closeSync(fd);if(code!==0)throw Error(`${label}/${mode}: ${code}`);
 identity.binaries[`${label}-${mode}`]={args,cwd,seconds:(Date.now()-start)/1000,sha256:createHash('sha256').update(fs.readFileSync(bin)).digest('hex')};
 fs.writeFileSync(`${root}/metadata.json`,JSON.stringify(identity,null,2)+'\n');console.log(label,mode);
}
