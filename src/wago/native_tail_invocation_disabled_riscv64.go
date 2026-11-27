// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && wago_no_tail_call

package wago

import "context"

type rv64NativeTailInvocationStorage struct{}

func (*rv64NativeTailInvocationStorage) get() *rv64NativeTailInvocation { return nil }

func railshotRetainedCallbackHostScope(*Instance, uintptr, *instancePluginState, context.Context) *hostCallScope {
	return nil
}
