// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import (
	"testing"
	"unsafe"
)

func TestSharingScratchLayout(t *testing.T) {
	t.Logf("scratch=%d fn=%d node=%d control=%d", unsafe.Sizeof(scratch{}), unsafe.Sizeof(fn{}), unsafe.Sizeof(elem{}), unsafe.Sizeof(ctrlFrame{}))
}
