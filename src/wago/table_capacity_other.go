// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !linux || !riscv64 || tinygo || wago_target_tinygo

package wago

import "github.com/wago-org/wago/src/core/compiler/frontend"

func (*Compiled) rv64ExecutionTableCapacities() []uint32 { return nil }

func railshotStoreTableCapacities(*Compiled, []frontend.TableRuntimeShape) error { return nil }
func railshotValidateTableCapacities(*Compiled) error                            { return nil }
func railshotTableReservedCapacity(*Compiled, int) (int, bool)                   { return 0, false }
func railshotTableCapacitySnapshotBytes(*Compiled) uintptr                       { return 0 }
