// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

import { UTF8 } from "./index";

// The unsafe length kernel takes the SIMD path without allocating an
// intermediate AssemblyScript string on every host invocation.
const text = "SIMD UTF-8 benchmark: Καλημέρα 世界 🚀 ".repeat(96);
const input = UTF8.encode(text);

export function convertN(n: i32): i32 {
  let sum = 0;
  const ptr = changetype<usize>(input);
  for (let i = 0; i < n; i++) sum += UTF8.utf16LengthUnsafe(ptr, input.byteLength);
  return sum;
}

export function validateN(n: i32): i32 {
  let sum = 0;
  const ptr = changetype<usize>(input);
  for (let i = 0; i < n; i++) sum += UTF8.validateUnsafe(ptr, input.byteLength);
  return sum;
}
