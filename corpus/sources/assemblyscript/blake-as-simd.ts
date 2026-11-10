// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

import { hashUnsafe } from "./blake3/index_simd";

const input = memory.data(4096);
const output = memory.data(32);

for (let i = 0; i < 4096; i++) store<u8>(input + <usize>i, <u8>(i % 251));

export function hashN(n: i32): i32 {
  for (let i = 0; i < n; i++) hashUnsafe(input, 4096, output);
  return load<i32>(output);
}
