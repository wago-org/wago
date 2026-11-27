// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_standalone_no_collector || (wago_no_gc && (!linux || !riscv64 || tinygo || wago_target_tinygo || wago_no_exception_handling))

package wago

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

type instanceCollectorMetadata struct{}

func newInstanceCollectorMetadata(*gcTypeMapping, *gc.NativeInstanceView) instanceCollectorMetadata {
	return instanceCollectorMetadata{}
}

func (*instanceCollectorMetadata) gcTypeMap() *gcTypeMapping { return nil }

func (*instanceCollectorMetadata) gcNativeView() *gc.NativeInstanceView { return nil }

func (*Instance) hasNativeCollector() bool { return false }

func (*Instance) revokePrivateGCEntry() {}
