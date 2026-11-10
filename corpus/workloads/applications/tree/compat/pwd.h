// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

#ifndef WAGO_TREE_WASI_PWD_H
#define WAGO_TREE_WASI_PWD_H

#include <sys/types.h>

struct passwd { char *pw_name; };
static inline struct passwd *getpwuid(uid_t uid) {
  (void)uid;
  return 0;
}

#endif
