// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

/* The corpus runs one valid, embedded program and does not exercise recovery
 * from Lua errors. Avoid setjmp and signals so the interpreter stays a plain
 * core Wasm module; any unexpected error traps and fails the exact oracle. */
#define l_signalT int
#define _SETJMP_H
#define luai_jmpbuf int
#define LUAI_THROW(L, c) __builtin_trap()
#define LUAI_TRY(L, c, a) do { a } while (0)
