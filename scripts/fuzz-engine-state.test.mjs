// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

import assert from "node:assert/strict";
import test from "node:test";

import { WorkerClient } from "./fuzz-engine-state.mjs";

test("WorkerClient kills a worker that exceeds the case timeout", { timeout: 2_000 }, async (t) => {
  const worker = new WorkerClient(
    process.execPath,
    50,
    ["-e", "process.stdin.resume(); setInterval(() => {}, 1000)"],
  );
  t.after(() => {
    if (worker.process.exitCode === null && worker.process.signalCode === null) {
      worker.process.kill("SIGKILL");
    }
  });

  await assert.rejects(
    worker.request({ id: 7 }),
    /Railshot case 7 exceeded 50 ms/,
  );
  await worker.close();
  assert.notEqual(worker.process.signalCode, null);
});
