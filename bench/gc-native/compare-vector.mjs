#!/usr/bin/env node
// Exit nonzero when a checked loop still exceeds the V8 timing target.
import { readFileSync } from 'node:fs';
const [wagoPath, v8Path] = process.argv.slice(2);
if (!wagoPath || !v8Path) throw new Error('usage: compare-vector.mjs wago.txt v8.json');
const wago = readFileSync(wagoPath, 'utf8');
const v8 = JSON.parse(readFileSync(v8Path, 'utf8'));
const median = values => {
  if (!values.length || values.some(value => !Number.isFinite(value) || value <= 0)) {
    throw new Error('missing or invalid timing samples');
  }
  const sorted = [...values].sort((a, b) => a - b);
  const middle = Math.floor(sorted.length / 2);
  return sorted.length % 2 ? sorted[middle] : (sorted[middle - 1] + sorted[middle]) / 2;
};
let failed = false;
for (const name of ['read', 'write']) {
  const matches = [...wago.matchAll(new RegExp(`BenchmarkGCVectorArrayAccess/${name}(?:-\\d+)?\\s+\\d+\\s+([\\d.]+) ns/op`, 'g'))];
  const wagoNS = median(matches.map(match => Number(match[1])));
  const v8NS = median(v8.rows.filter(row => row.name === name && row.iterations === 8192).map(row => row.ns_per_op));
  const ratio = wagoNS / v8NS;
  console.log(`${name}: Wago ${wagoNS.toFixed(1)} ns/op; V8 ${v8NS.toFixed(1)} ns/op; ratio ${ratio.toFixed(3)}; ${ratio <= 1 ? 'PASS' : 'FAIL'}`);
  failed ||= ratio > 1;
}
process.exitCode = failed ? 1 : 0;
