import fs from 'node:fs';
import crypto from 'node:crypto';
import {execFileSync} from 'node:child_process';
const out='/tmp/wago-pr564-HbN430';
const release=JSON.parse(execFileSync('gh',['api','repos/tinygo-org/tinygo/releases/tags/v0.41.1'],{encoding:'utf8',maxBuffer:8*1024*1024}));
const asset=release.assets.find(a=>a.name==='tinygo0.41.1.linux-amd64.tar.gz');
if(!asset||!asset.browser_download_url.startsWith('https://github.com/tinygo-org/tinygo/releases/download/v0.41.1/'))throw new Error('expected TinyGo release asset not found');
fs.writeFileSync(`${out}/pinned-tinygo-release.json`,JSON.stringify({tag:release.tag_name,url:release.html_url,asset},null,2)+'\n');
if(process.argv.includes('--metadata-only')){console.log({name:asset.name,bytes:asset.size,digest:asset.digest});process.exit(0);}
const archive=`${out}/${asset.name}`;
execFileSync('curl',['-fL','--retry','2','-o',archive,asset.browser_download_url],{stdio:'inherit'});
const digest=crypto.createHash('sha256').update(fs.readFileSync(archive)).digest('hex');
if(asset.digest!==`sha256:${digest}`)throw new Error(`TinyGo digest mismatch or missing publisher digest: ${asset.digest} / sha256:${digest}`);
const dir=`${out}/pinned-tinygo`;
if(fs.existsSync(dir))throw new Error('pinned TinyGo destination already exists; inspect it before replacing');
fs.mkdirSync(dir);
execFileSync('tar',['-xzf',archive,'-C',dir,'--strip-components=1']);
console.log(`Verified and extracted TinyGo 0.41.1 (${digest}) to ${dir}`);
