// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64 && (linux || darwin || windows)

package amd64

import (
	"testing"
)

func BenchmarkCompileInertPrefixStores(b *testing.B) {
	const n = 8192
	body := []byte{0}
	for i := 0; i < n; i++ {
		body = append(body, 0x41, 1)
	}
	for i := 0; i < n; i++ {
		body = append(body, 0x41, 0, 0x41, 1, 0x36, 2, 0)
	}
	for i := 0; i < n; i++ {
		body = append(body, 0x1a)
	}
	body = append(body, 0x0b)
	m := benchDecodeValidateModule(b, benchModuleBytes([]benchFuncDef{{body: body}}, true))
	benchmarkCompileModule(b, m)
}
