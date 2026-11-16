const fs = require('fs');
const crypto = require('crypto');
const [path, name, ns, ws, sha, warmupArg, samplesArg, batchArg] = process.argv.slice(2);
const n = Number(ns), want = Number(ws), warmup = Number(warmupArg), count = Number(samplesArg), batch = Number(batchArg);
const bytes = fs.readFileSync(path);
if (crypto.createHash('sha256').update(bytes).digest('hex') !== sha) throw Error('artifact hash mismatch');
const instance = new WebAssembly.Instance(new WebAssembly.Module(bytes));
const fn = instance.exports[name];
const check = () => { const x = fn(n) >>> 0; if (x !== want) throw Error(`${x} != ${want}`); };
for (let j = 0; j < warmup; j++) check();
const samples = [];
for (let s = 0; s < count; s++) {
  const t = process.hrtime.bigint();
  for (let j = 0; j < batch; j++) check();
  samples.push(Number(process.hrtime.bigint() - t) / batch);
}
const status = fs.readFileSync('/proc/self/status', 'utf8');
const memory = {};
for (const key of ['VmHWM', 'VmRSS']) memory[key + '_kib'] = Number(status.match(new RegExp('^' + key + ':\\s+(\\d+)', 'm'))[1]);
console.log(JSON.stringify({samples_ns: samples, v8: process.versions.v8, node: process.version, artifact_sha256: sha, checksum: want, warmup_calls: warmup, samples: count, batch, memory, v8_flags: process.execArgv, collection_counters: 'not instrumented'}));
