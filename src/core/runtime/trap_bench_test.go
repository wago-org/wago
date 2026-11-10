// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package runtime

import "testing"

var trapMessageBenchSink string

func BenchmarkTrapMessageLookup(b *testing.B) {
	var message string
	b.ReportMetric(256, "lookups/op")
	for i := 0; i < b.N; i++ {
		for j := 0; j < 256; j++ {
			message = TrapCode(j % len(trapMessages)).String()
		}
	}
	trapMessageBenchSink = message
}
