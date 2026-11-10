// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package main

import "testing"

func TestGuests(t *testing.T) {
	rt := newRuntime()
	defer rt.Close()
	for name, guest := range map[string]struct {
		source     []byte
		initialize bool
	}{
		"wat":            {watGuest, false},
		"assemblyscript": {assemblyScriptGuest, false},
		"tinygo":         {tinyGoGuest, true},
	} {
		if got := run(rt, guest.source, guest.initialize); got != 42 {
			t.Fatalf("%s answer = %d", name, got)
		}
	}
}
