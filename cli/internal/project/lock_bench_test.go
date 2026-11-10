// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package project

import (
	"fmt"
	"testing"
)

func BenchmarkEncodeLock(b *testing.B) {
	for _, count := range []int{0, 1, 10, 100, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			doc := NewLockDocument()
			for i := 0; i < count; i++ {
				id := fmt.Sprintf("github.com/acme/p%d", i)
				doc.Plugins[id] = testLockEntry(true, id, map[string]string{})
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := EncodeLock(doc); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
