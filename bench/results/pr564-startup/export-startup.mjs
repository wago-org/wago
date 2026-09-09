import fs from 'node:fs';
import {createHash} from 'node:crypto';
const repo='/home/jtenner/Projects/wago',root=`${repo}/.tmp/pr564-memory`,out=`${repo}/bench/results/pr564-startup`;
for(const name of ['namecheck-smoke.json','namecheck-standard-smoke.json','namecheck-project-full.txt','namecheck-tests.txt','namecheck-tiny-tests.txt','namecheck-bench.txt','tiny-smoke.json','tiny-debug-smoke.txt','namecheck-smoke.mjs','tiny-smoke.mjs','export-startup.mjs'])fs.copyFileSync(`${root}/${name}`,`${out}/${name}`);
const files=fs.readdirSync(out).filter(n=>n!=='SHA256SUMS').sort();
fs.writeFileSync(`${out}/SHA256SUMS`,files.map(n=>`${createHash('sha256').update(fs.readFileSync(`${out}/${n}`)).digest('hex')}  ${n}`).join('\n')+'\n');
