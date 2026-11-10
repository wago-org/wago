#!/usr/bin/env node
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

// Run from any directory. Each sample times 8192 guest loop iterations per call,
// like BenchmarkGCVectorArrayAccess; compilation and instantiation are excluded.
import { readFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { performance } from 'node:perf_hooks';

const bytes = readFileSync(new URL('../../tests/fixtures/wasm/gc-array-vector-access.wasm', import.meta.url));
const module = await WebAssembly.compile(bytes);
const { exports: guest } = await WebAssembly.instantiate(module);
const iterations = 8192;
const rows = [];
let consumed = 0n;
for (const name of ['read', 'write']) {
  guest.init();
  const want = BigInt(name === 'read' ? 7 * iterations : iterations - 1);
  const invoke = () => {
    const result = guest[name](iterations);
    if (result !== want) throw new Error(`${name}: ${result}, expected ${want}`);
    consumed ^= result;
  };
  // Let V8 tier up before measuring sustained execution.
  const warm = performance.now() + 250;
  do { invoke(); } while (performance.now() < warm);
  for (let sample = 0; sample < 5; sample++) {
    let calls = 0;
    const start = performance.now();
    let elapsed;
    do {
      invoke();
      calls++;
      elapsed = performance.now() - start;
    } while (elapsed < 200);
    rows.push({ name, sample, calls, ns_per_op: elapsed * 1e6 / calls, iterations });
  }
}
console.log(JSON.stringify({
  engine: 'v8', node: process.version, v8: process.versions.v8,
  module_sha256: createHash('sha256').update(bytes).digest('hex'),
  consumed: consumed.toString(), rows,
}, null, 2));
