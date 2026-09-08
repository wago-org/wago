import fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {createHash} from 'node:crypto';
import {execFileSync} from 'node:child_process';
const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'..');
const old=path.resolve(root,'../pr564');
const prior=execFileSync('sha256sum',['-c','SHA256SUMS'],{cwd:old,encoding:'utf8'});
fs.writeFileSync(path.join(root,'verification-original.txt'),prior);
function files(dir,prefix='') {
 return fs.readdirSync(dir,{withFileTypes:true}).flatMap(e=>e.isDirectory()?files(path.join(dir,e.name),prefix+e.name+'/'):e.isFile()&&prefix+e.name!=='SHA256SUMS'?[prefix+e.name]:[]);
}
const names=files(root).sort();
const lines=names.map(name=>createHash('sha256').update(fs.readFileSync(path.join(root,name))).digest('hex')+'  '+name);
fs.writeFileSync(path.join(root,'SHA256SUMS'),lines.join('\n')+'\n');
const current=execFileSync('sha256sum',['-c','SHA256SUMS'],{cwd:root,encoding:'utf8'});
console.log(JSON.stringify({originalVerified:prior.trim().split('\n').length,newVerified:current.trim().split('\n').length}));
