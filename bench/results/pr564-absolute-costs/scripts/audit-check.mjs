import fs from 'node:fs';
import assert from 'node:assert/strict';
const root='/home/jtenner/Projects/wago/.tmp/pr564-absolute-costs',rows=[];
for(const mode of ['explicit','signals']) {
 const parsed={};
 for(const label of ['base','final']) {
  const text=fs.readFileSync(`${root}/audit-${label}-${mode}.txt`,'utf8');
  parsed[label]=Object.fromEntries([...text.matchAll(/CODE (\S+) bytes=(\d+) sha256=([a-f0-9]{64})/g)].map(m=>[m[1],{bytes:+m[2],sha256:m[3]}]));
  assert.deepEqual(Object.keys(parsed[label]).sort(),['coremark','isa_bulk_mem','ruby','wasm3']);
 }
 for(const name of Object.keys(parsed.base)) {
  assert.deepEqual(parsed.base[name],parsed.final[name],`${mode}/${name}: native code differs`);
  rows.push({mode,name,...parsed.base[name],equal:true});
 }
}
fs.writeFileSync(`${root}/audit-summary.json`,JSON.stringify(rows,null,2)+'\n');
console.log('PASS: main and after have equal native-code bytes for all eight selected module/build pairs');
