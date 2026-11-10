// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

import { JSON } from "./index";

@json
class Payload {
  id: i32 = 42;
  name: string = "wago SIMD benchmark payload";
  enabled: bool = true;
  values: i32[] = [3, 5, 8, 13, 21, 34, 55, 89];
}

const payload = new Payload();
const encoded = JSON.stringify(payload);

export function serializeN(n: i32): i32 {
  let sum = 0;
  for (let i = 0; i < n; i++) sum += JSON.stringify(payload).length;
  return sum;
}

export function deserializeN(n: i32): i32 {
  let sum = 0;
  for (let i = 0; i < n; i++) sum += JSON.parse<Payload>(encoded).id;
  return sum;
}
