import fs from 'node:fs';
import {execFileSync} from 'node:child_process';
import {createHash} from 'node:crypto';
const repo='/home/jtenner/Projects/wago',root=`${repo}/.tmp/pr564-absolute-costs`;
const read=p=>JSON.parse(fs.readFileSync(`${root}/${p}`));
const assert=(ok,message)=>{if(!ok)throw Error(message);};
const metadata=read('metadata.json'),q=read('full/qualification.json');
assert(metadata.main==='a07de0973191efab1d32677eff527952c7f9cdd2','Wrong main baseline');
assert(metadata.after==='3a84fa628ac16ac5502a7de986f2fe732007583d','Wrong production checkpoint');
assert(q.pairedMetrics===16144&&!q.unpaired.length&&!q.badSampleCounts.length,'Full metric coverage');
assert(q.processSummary.reduce((n,r)=>n+r.processes,0)===1020&&q.processSummary.every(r=>r.failed===0),'Full process coverage');
assert(q.confirmationCases===348&&q.codeSize.rows===298&&q.codeSize.changed===0,'Full confirmation/code-size coverage');
// The original driver stopped at a smoke failure. Verify each completed stage
// below instead of treating a driver completion marker as a release verdict.
execFileSync('git',['diff','--exit-code',metadata.after,'--','src','cli','internal','wago.go','go.mod','go.sum','bench/*.go','bench/go.mod','bench/go.sum'],{cwd:repo});
const runs=[];
for(const run of fs.readdirSync(root).filter(s=>fs.existsSync(`${root}/${s}/identity.json`))) {
 const identity=read(`${run}/identity.json`);
 const status=fs.readFileSync(`${root}/${run}/status.jsonl`,'utf8').trim().split('\n').filter(Boolean).map(JSON.parse);
 const expected=identity.names.length*identity.labels.length*identity.samples;
 assert(status.length===expected&&new Set(status.map(r=>r.key)).size===expected,`${run}: process coverage`);
 assert(status.every(r=>r.code===0&&r.boot===metadata.boot),`${run}: failure or changed boot`);
 for(const [file,hash]of Object.entries(identity.binaries))assert(createHash('sha256').update(fs.readFileSync(file)).digest('hex')===hash,`${run}: changed binary`);
 const comparison=read(`${run}/comparison.json`);
 assert(comparison.length>0&&comparison.every(r=>r.baseSamples===identity.samples&&r.candidateSamples===identity.samples),`${run}: sample counts`);
 runs.push({run,processes:expected,metrics:comparison.length,benchtime:identity.benchtime,samples:identity.samples});
}
for(const name of ['native','guard','race','bench','go122'])assert(read(`test-${name}.status.json`).code===0,`Failed ${name} test`);
for(const name of ['test-arm64.status','test-arm64-signals.status'])assert(fs.readFileSync(`${root}/${name}`,'utf8').trim()==='0',`Failed ${name}`);
const expectedFailures=fs.readFileSync(`${root}/test-before-expected-failures.txt`,'utf8');
assert((expectedFailures.match(/^--- FAIL:/gm)||[]).length===2&&expectedFailures.includes('TestFuncSigIntRegABINoAlloc')&&expectedFailures.includes('TestReferenceStoreTypeKeyCapacity'),'Missing expected pre-fix test failures');
const audit=read('audit-all-summary.json');
assert(audit.length>0,'Empty executable code audit');
for(const mode of ['explicit','signals'])for(const label of ['base','final'])assert(read(`audit-all-${label}-${mode}.status.json`).code===0,'Failed code audit');
const size=read('size.json');
assert(size.rows.length===12&&size.rows.every(r=>r.code===0&&Number.isFinite(r.budget)&&r.bytes>0),'Release build coverage');
const smokes=fs.readdirSync(`${root}/size`).filter(n=>n.endsWith('.smoke.json')).map(name=>({name,...read(`size/${name}`)}));
assert(smokes.length===9,'Release smoke coverage');
const summary={verified:new Date().toISOString(),main:metadata.main,production:metadata.after,fullProcesses:1020,fullMetrics:16144,confirmationCases:348,runs,codeAuditPairs:audit.length,codeAuditChanges:audit.filter(r=>!r.equal),releaseBudgetFailures:size.rows.filter(r=>r.bytes>r.budget),releaseSmokeFailures:smokes.filter(r=>!r.passed),notes:['Three small main-relative timing costs remain; see final timing diagnostics.','Existing Wine/root-suite and native ARM64 timing limits are not cleared.','This verifies evidence coverage, not universal correctness or timing equivalence.']};
fs.writeFileSync(`${root}/verification.json`,JSON.stringify(summary,null,2)+'\n');
console.log(summary);
