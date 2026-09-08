import fs from 'node:fs';
import path from 'node:path';
import zlib from 'node:zlib';
import {execFileSync} from 'node:child_process';
const src='/tmp/wago-pr564-HbN430';
const dst='/home/jtenner/Projects/wago/bench/results/pr564';
fs.mkdirSync(dst,{recursive:true});
const plain=['pr.json','pr-description.md','original-REPORT.md','pr-commit-messages.txt','inventory-summary.md','metric-inventory.json','metric-inventory.tsv','environment.txt','provenance.json','comparison.json','comparison.tsv','summary.json','regression-screen.json','run-summary.json','benchstat-explicit.txt','benchstat-signals.txt','confirmation-summary.json','regressions.json','regressions.tsv','confirmed-timing.md','arm64-code-size.tsv','single-worker-summary.json','benchstat-single-worker.txt','test-summary.md'];
for(const name of plain)if(fs.existsSync(`${src}/${name}`))fs.copyFileSync(`${src}/${name}`,`${dst}/${name}`);
// Normalize display-only whitespace in derived tables and the git-log wrapper.
// Benchmark captures, PR description, and original report remain byte-exact.
for(const name of ['benchstat-explicit.txt','benchstat-signals.txt','benchstat-single-worker.txt'])if(fs.existsSync(`${dst}/${name}`))fs.writeFileSync(`${dst}/${name}`,fs.readFileSync(`${dst}/${name}`,'utf8').replace(/[ \t]+$/gm,''));
fs.writeFileSync(`${dst}/pr-commit-messages.txt`,fs.readFileSync(`${dst}/pr-commit-messages.txt`,'utf8').replace(/\n+$/,'\n'));
for(const name of ['build-size.json','build-size-initial.json','build-size.tsv','pinned-tinygo-release.json','wine-tools.json','post-test-commands.json','confirmation-pause.json','resource-diagnostics.json','reviewed-head-checks.json','reviewed-ci-size.md','reviewed-ci-size-profiles.tsv','reviewed-ci-size-symbols.tsv','reviewed-ci-size.zip'])if(fs.existsSync(`${src}/${name}`))fs.copyFileSync(`${src}/${name}`,`${dst}/${name}`);
const raw=fs.existsSync(`${src}/host-load.jsonl`)?['host-load.jsonl']:[];
for(const name of fs.readdirSync(src)) {
  if(/^(?:base|candidate|initial-base|initial-candidate)-(?:explicit|signals)\.(?:txt|status\.json)$/.test(name)||/^test-.*\.(?:txt|json)$/.test(name)||/^confirmation-.*\.txt$/.test(name)||/^single-worker-.*\.txt$/.test(name)||/^arm64-.*\.txt$/.test(name)||/^reviewed-ci-.*\.txt$/.test(name))raw.push(name);
}
for(const name of raw) {
  const data=fs.readFileSync(`${src}/${name}`);
  fs.writeFileSync(`${dst}/${name}.gz`,zlib.gzipSync(data,{level:9}));
}
// The archive retains per-process logs and GNU time measurements, including
// unsuccessful attempts. Aggregate result files are easier to use with benchstat.
const dirs=['paired','confirmations','single-worker','arm64-size','resource-diagnostics'].filter(name=>fs.existsSync(`${src}/${name}`));
if(dirs.length)execFileSync('tar',['-czf',`${dst}/process-captures.tar.gz`,'-C',src,...dirs]);
if(fs.existsSync(`${src}/build-size`))for(const name of fs.readdirSync(`${src}/build-size`).filter(n=>n.endsWith('.txt')))fs.writeFileSync(`${dst}/build-size-${name}.gz`,zlib.gzipSync(fs.readFileSync(`${src}/build-size/${name}`),{level:9}));
const scripts=['capture.mjs','checks.mjs','inventory.mjs','paired.mjs','analyze.mjs','summarize.mjs','confirm.mjs','regressions.mjs','render.mjs','qualify.mjs','post-tests.mjs','pinned-tools.mjs','wine-tools.mjs','tool-tests.mjs','wine-diagnostic.mjs','test-summary.mjs','build-size.mjs','build-smoke.mjs','arm64-size.mjs','resource-diagnostics.mjs','resume-confirmations.mjs','native-census.mjs','scaling.mjs','provenance.mjs','host.mjs','export.mjs'];
scripts.push('verify-data.mjs');
fs.mkdirSync(`${dst}/scripts`,{recursive:true});
for(const name of scripts)if(fs.existsSync(`${src}/${name}`))fs.copyFileSync(`${src}/${name}`,`${dst}/scripts/${name}`);
fs.copyFileSync(`${src}/native-census.go`,`${dst}/scripts/native-census.go.txt`);
const files=fs.readdirSync(dst).filter(n=>fs.statSync(path.join(dst,n)).isFile()&&n!=='SHA256SUMS');
files.push(...fs.readdirSync(`${dst}/scripts`).map(n=>`scripts/${n}`));
const sums=execFileSync('sha256sum',files.sort(),{cwd:dst,encoding:'utf8'});
fs.writeFileSync(`${dst}/SHA256SUMS`,sums);
console.log(`Exported qualification evidence to ${dst}`);
