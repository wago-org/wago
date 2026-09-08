import fs from 'node:fs';
import {spawnSync} from 'node:child_process';
const out='/tmp/wago-pr564-HbN430';
const cases=[['wine',['--version']],['wine',['certutil.exe','-hashfile',`Z:${out.replaceAll('/','\\')}\\build-size\\base-manager`,'SHA256']],['sha256sum',[`${out}/build-size/base-manager`]]];
const results=cases.map(([cmd,args])=>{const r=spawnSync(cmd,args,{encoding:'utf8'});return{cmd,args,code:r.status,stdout:r.stdout,stderr:r.stderr};});
fs.writeFileSync(`${out}/test-wine-certutil-diagnostic.json`,JSON.stringify({note:'Read-only local tool check. Wine certutil emits no SHA-256 output for a known existing binary; checksum admission is not weakened to make the bootstrap pass.',results},null,2)+'\n');
console.log(results);
