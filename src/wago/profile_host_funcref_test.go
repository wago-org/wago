// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_profile && (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"testing"
)

func TestProfileRetainedHostReferenceExecutionLease(t *testing.T) {
	for _, independent := range []bool{false, true} {
		t.Run(fmt.Sprintf("independent%t", independent), func(t *testing.T) {
			session := NewCodeProfile(CodeProfileOptions{TraceBoundaries: true})
			defer session.Close()
			cfg := NewRuntimeConfig().WithCodeProfile(session).WithIndependentInstanceExecution(independent)
			runRetainedNumericHostReference(t, WithRuntimeConfig(cfg))
			callbacks := 0
			for _, span := range session.Spans() {
				if span.End == 0 {
					t.Fatalf("unfinished profile boundary: %+v", span)
				}
				if span.Kind == "host-callback" {
					callbacks++
				}
			}
			if callbacks < 2 {
				t.Fatalf("host callbacks=%d, want direct and retained dispatch", callbacks)
			}
		})
	}
}
