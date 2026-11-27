// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && (wago_standalone_no_collector || (wago_no_gc && wago_no_exception_handling))

package wago

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

func managedFunctionRootEpochLocked(*Instance)          {}
func managedFunctionRetainAllowedLocked(*Instance) bool { return true }
func drainManagedFunctionReclaims(*gc.Collector)        {}
