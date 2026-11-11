// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !linux || !riscv64 || tinygo || wago_target_tinygo

package wago

import "github.com/wago-org/wago/src/core/compiler/wasm"

func installRV64StorageFreeProof(*Compiled, *wasm.Module) {}
func rv64StorageFreeProof(*Compiled) bool                 { return false }

func (*Compiled) rv64ExecutionContract() *rv64ExecutionContract   { return nil }
func (*Compiled) setRV64ExecutionContract(*rv64ExecutionContract) {}

func rv64SourceFrameOwnership(*Compiled) bool   { return false }
func rv64FixedRootlessGCProduct(*Compiled) bool { return false }
