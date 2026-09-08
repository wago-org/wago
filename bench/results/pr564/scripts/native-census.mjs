import fs from 'node:fs';
import {spawnSync} from 'node:child_process';
const out='/tmp/wago-pr564-HbN430',repo='/home/jtenner/Projects/wago';
for(const label of ['base','candidate']) {
  const cwd=label==='base'?`${out}/main`:repo;
  const binary=`${out}/native-census-${label}`;
  const args=['build','-o',binary,`${out}/native-census.go`];
  const build=spawnSync('go',args,{cwd,env:{...process.env,GOMAXPROCS:'8',GOFLAGS:'-buildvcs=false'},encoding:'utf8'});
  fs.writeFileSync(`${out}/test-native-census-${label}-build.txt`,build.stdout+build.stderr);
  if(build.status!==0)throw new Error(`native census ${label} build failed`);
  const run=spawnSync(binary,[`${repo}/bench/corpus`],{cwd,env:{...process.env,GOMAXPROCS:'8',GOGC:'100',WAGO_BOUNDS:'explicit'},encoding:'utf8'});
  fs.writeFileSync(`${out}/test-native-census-${label}.txt`,run.stdout+run.stderr);
  if(run.status!==0)throw new Error(`native census ${label} failed`);
}
const a=JSON.parse(fs.readFileSync(`${out}/test-native-census-base.txt`)),b=JSON.parse(fs.readFileSync(`${out}/test-native-census-candidate.txt`));
const equal=JSON.stringify(a)===JSON.stringify(b);
fs.writeFileSync(`${out}/test-native-census-parity.json`,JSON.stringify({equal,cases:a.length,fields:['codeSHA256','entry','internalEntry','preparedFlags'],bounds:'explicit',entryRepresentation:'decimal strings preserve exact signed machine-word offsets and flag bits',note:'This does not attribute timing changes or establish complete runtime equivalence.'},null,2)+'\n');
if(!equal)throw new Error('native bytes, entries, or prepared-call flags differ; inspect captures');
console.log(`Same native code hashes, entries, and prepared-call flags for ${a.length} module/configuration cases. This does not attribute the timing change or establish complete runtime equivalence.`);
