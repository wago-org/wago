// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_no_gc

package wago

import (
	"errors"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

type gcArrayConstructorState struct{}

func (*gcArrayConstructorState) arrayInitializerScratch() *gc.ArrayInitializerRootScratch { return nil }

func (*gcArrayConstructorState) constructorValues(uint32) ([]gc.Value, error) {
	return nil, errors.New("GC array constructors are disabled in this build")
}
