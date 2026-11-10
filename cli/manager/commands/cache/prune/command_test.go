// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package prune

import "testing"

func TestParseDaysRejectsDurationOverflow(t *testing.T) {
	for _, test := range []struct {
		value string
		want  int
		ok    bool
	}{
		{value: "0", want: 0, ok: true},
		{value: "30", want: 30, ok: true},
		{value: "106751", want: 106751, ok: true},
		{value: "106752"},
		{value: "9223372036854775807"},
		{value: "-1"},
		{value: "invalid"},
	} {
		got, err := parseDays(test.value)
		if test.ok {
			if err != nil || got != test.want {
				t.Errorf("parseDays(%q) = %d, %v; want %d", test.value, got, err, test.want)
			}
		} else if err == nil {
			t.Errorf("parseDays(%q) = %d, want error", test.value, got)
		}
	}
}
