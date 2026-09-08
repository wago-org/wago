import fs from 'node:fs';
import assert from 'node:assert/strict';
const out='/tmp/wago-pr564-HbN430';
const read=name=>JSON.parse(fs.readFileSync(`${out}/${name}`));
const comparison=read('comparison.json'),confirmation=read('confirmation-summary.json');
assert.equal(comparison.length,16144);
assert.equal(new Set(comparison.map(r=>`${r.mode}/${r.name}/${r.unit}`)).size,comparison.length);
assert(comparison.every(r=>!r.status&&r.baseSamples===6&&r.candidateSamples===6&&r.p>=0&&r.p<=1));
assert.equal(new Set(confirmation.map(r=>`${r.mode}/${r.name}`)).size,139);
assert(confirmation.every(r=>r.baseSamples===12&&r.candidateSamples===12&&r.p>=0&&r.p<=1));
for(const [dir,count]of [['paired',1020],['confirmations',3336],['single-worker',96]]) {
  const rows=fs.readFileSync(`${out}/${dir}/status.jsonl`,'utf8').trim().split('\n').map(JSON.parse);
  assert.equal(rows.length,count);assert.equal(new Set(rows.map(r=>r.key)).size,count);assert(rows.every(r=>r.code===0));
}
assert.equal(read('metric-inventory.json').entries.length,1036);
const builds=read('build-size.json').rows;
assert.equal(builds.length,8);assert(builds.every(r=>r.code===0&&r.stripError===null&&r.bytes>0&&r.bytes<r.budget));
assert(read('test-build-smoke.json').every(r=>r.ok));
assert.equal(read('test-native-census-parity.json').equal,true);
const arm=fs.readFileSync(`${out}/arm64-code-size.tsv`,'utf8').trim().split('\n').slice(1).map(l=>l.split('\t'));
assert.equal(arm.length,60);assert(arm.every(r=>r[1]!==''&&r[2]!==''&&+r[3]<=0));
const summaries=read('summary.json').filter(r=>r.stage==='ExecParallel'&&r.unit==='ns/op'&&r.corpus==='default');
assert.equal(summaries.length,2);assert(summaries.every(r=>r.rows===72));
console.log('Data checks pass: 16,144 paired metrics, 139 twelve-pair repeat groups, 4,452 successful command processes, 1,036 inventory lines, and complete size/call-path checks.');
