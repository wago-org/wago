// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

if (arguments.length !== 1) {
  throw new Error("usage: v8 v8-runner.js -- <module.wasm>");
}

const bytes = readbuffer(arguments[0]);
const mod = new WebAssembly.Module(bytes);
const instance = new WebAssembly.Instance(mod, {});

if (typeof instance.exports._start !== "function") {
  throw new Error("module does not export _start");
}

instance.exports._start();
