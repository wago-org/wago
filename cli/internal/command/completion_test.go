// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package command

import (
	"slices"
	"testing"
)

func TestCompleteTraversesCommandsAndFlags(t *testing.T) {
	root := &Cmd{Name: "wago", Children: []*Cmd{
		{Name: "version", Aliases: []string{"versions"}, Children: []*Cmd{
			{Name: "install", Aliases: []string{"add"}, Flags: []Flag{
				{Name: "profile", Short: "p", Arg: "<profile>"},
				{Name: "use", Short: "u", Bool: true},
			}},
		}},
	}}

	tests := []struct {
		args []string
		want []string
	}{
		{[]string{"ver"}, []string{"version", "versions"}},
		{[]string{"versions", "a"}, []string{"add"}},
		{[]string{"version", "install", "--p"}, []string{"--profile"}},
		{[]string{"version", "install", "-"}, []string{"--profile", "-p", "--use", "-u", "--no-input", "--locked", "--offline", "--help", "-h"}},
		{[]string{"version", "install", "--profile", ""}, nil},
		{[]string{"--j"}, []string{"--json"}},
	}
	for _, test := range tests {
		if got := Complete(root, test.args); !slices.Equal(got, test.want) {
			t.Errorf("Complete(%q) = %q, want %q", test.args, got, test.want)
		}
	}
}
