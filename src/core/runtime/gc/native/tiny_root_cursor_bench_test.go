// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package gc

import (
	"fmt"
	"testing"
)

func BenchmarkTinyStepPersistentRoots(b *testing.B) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		b.Fatal(err)
	}
	for _, roots := range []int{256, 4096, 65536} {
		b.Run(fmt.Sprintf("roots=%d", roots), func(b *testing.B) {
			c, err := NewCollector(Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf})
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			for i := 0; i < roots; i++ {
				c.NewGlobalSlot(Null())
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c.tinyGC.state = tinyMark
				c.tinyGC.rootPhase = tinyRootsGlobals
				c.tinyGC.sweep = 0
				if _, err := c.tinyDrainRootBudget(nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
