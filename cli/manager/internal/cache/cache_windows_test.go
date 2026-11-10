// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build windows

package cache

import "testing"

func TestCanonicalCachePathContainedRejectsCaseDistinctSibling(t *testing.T) {
	root := `\\?\C:\wago\versions`
	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "root", path: root, want: true},
		{name: "descendant", path: root + `\v1\standard\normal\plugins`, want: true},
		{name: "textual prefix", path: root + `-backup\v1`},
		{name: "case distinct sibling", path: `\\?\C:\wago\VERSIONS\v1\standard\normal\plugins`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := canonicalCachePathContained(test.path, root); got != test.want {
				t.Fatalf("canonicalCachePathContained(%q, %q) = %v, want %v", test.path, root, got, test.want)
			}
		})
	}
}
