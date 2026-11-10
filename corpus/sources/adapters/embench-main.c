// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

#include "support.h"

int main(void) {
  initialise_benchmark();
  int result = benchmark();
  int verified = verify_benchmark(result);
  return verified == 1 ? 0 : (verified < 0 ? 2 : 1);
}
