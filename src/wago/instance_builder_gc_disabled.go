// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_standalone_no_collector || (wago_no_gc && (!linux || !riscv64 || tinygo || wago_target_tinygo || wago_no_exception_handling))

package wago

import (
	"errors"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

type instanceBuilderGCState struct{}

func (*instanceBuilderGCState) collectorBuildState() *instanceCollectorState { return nil }

func (*instanceBuilder) prepareCollector() error { return nil }

func (*instanceBuilder) rollbackCollector(*resourceCleanup) {}

func (b *instanceBuilder) prepareNativeGCView() (*gc.NativeInstanceView, error) {
	return b.buildNativeGCInstanceView(nil)
}

func (*instanceBuilder) buildNativeGCInstanceView([]gc.TypeID) (*gc.NativeInstanceView, error) {
	return nil, errors.New("wago: native collector is disabled")
}
