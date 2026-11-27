// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_no_gc

package wago

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

type gcArrayConstructorState struct {
	gcArrayConstructorScratch
}

func (s *gcArrayConstructorState) arrayInitializerScratch() *gc.ArrayInitializerRootScratch {
	return &s.initializerRoots
}
