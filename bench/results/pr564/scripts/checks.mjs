import fs from 'node:fs';
import {execFileSync} from 'node:child_process';
const out='/tmp/wago-pr564-HbN430';
const raw=execFileSync('gh',['api','repos/wago-org/wago/commits/ab29bf3a9ad215833b5138220de6cd7190461a78/check-runs','--paginate'],{encoding:'utf8'});
fs.writeFileSync(`${out}/reviewed-head-checks.json`,raw);
const checks=JSON.parse(raw).check_runs;
console.log(JSON.stringify(checks.map(r=>({name:r.name,conclusion:r.conclusion,url:r.details_url,summary:r.output?.summary})),null,2));
for(const [name,id]of [['build-size','101347287030'],['linux-amd64','101347287134']]) {
  const log=execFileSync('gh',['api',`repos/wago-org/wago/actions/jobs/${id}/logs`],{encoding:'utf8',maxBuffer:64*1024*1024});
  fs.writeFileSync(`${out}/reviewed-ci-${name}.txt`,log);
  console.log(`${name}: ${log.split('\n').length} log lines`);
}
const archive=execFileSync('gh',['api','repos/wago-org/wago/actions/artifacts/9973917549/zip'],{maxBuffer:8*1024*1024});
fs.writeFileSync(`${out}/reviewed-ci-size.zip`,archive);
const entries=execFileSync('unzip',['-Z1',`${out}/reviewed-ci-size.zip`],{encoding:'utf8'}).trim().split('\n');
for(const name of ['size.md','size-profiles.tsv','size-symbols.tsv']) {
  const entry=entries.find(e=>e===name||e.endsWith('/'+name));
  if(!entry)throw new Error(`missing ${name} in CI size archive`);
  fs.writeFileSync(`${out}/reviewed-ci-${name}`,execFileSync('unzip',['-p',`${out}/reviewed-ci-size.zip`,entry]));
}
console.log(fs.readFileSync(`${out}/reviewed-ci-size.md`,'utf8'));
