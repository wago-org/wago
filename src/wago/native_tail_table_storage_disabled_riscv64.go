// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && wago_no_tail_call

package wago

type rv64NativeTailTableStorage struct{}

func (*rv64NativeTailTableStorage) get() *rv64NativeTailTableState { return nil }

func railshotRetireNativeTableTargets(railshotTableEntryLease) error { return nil }
func railshotRetryNativeTableRetirement(*Instance) error             { return nil }
func railshotRetryClosedNativeTableRetirement(*Instance) error       { return nil }
