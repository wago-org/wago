// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package command

import "testing"

func TestFlagTableFindsLongAndShortSpellings(t *testing.T) {
	table := flagLookup([]Flag{{Name: "invoke", Short: "e"}, {Name: "core"}, {Name: "x", Short: ""}})
	for arg, want := range map[string]string{"--invoke": "invoke", "-e": "invoke", "--core": "core"} {
		if got := table.get(arg); got == nil || got.Name != want {
			t.Fatalf("get(%q) = %v, want %s", arg, got, want)
		}
	}
	for _, arg := range []string{"--e", "-invoke", "-", "--", "core", "--missing", "-x"} {
		if got := table.get(arg); got != nil {
			t.Fatalf("get(%q) = %s, want nil", arg, got.Name)
		}
	}
}

func TestAllFlagsTracksLateAppends(t *testing.T) {
	cmd := &Cmd{Name: "run", Flags: []Flag{{Name: "a"}}}
	first := len(cmd.AllFlags())
	cmd.Flags = append(cmd.Flags, Flag{Name: "b"})
	cmd.Knobs = append(cmd.Knobs, Flag{Name: "k"})
	if got := len(cmd.AllFlags()); got != first+2 {
		t.Fatalf("AllFlags after append has %d flags, want %d", got, first+2)
	}
}

func TestFlagHelpFuncIsLazy(t *testing.T) {
	calls := 0
	flag := Flag{Name: "opt", Bool: true, HelpFunc: func() string { calls++; return "lazy" }}
	cmd := &Cmd{Name: "run", Flags: []Flag{flag}}
	cmd.AllFlags()
	if calls != 0 {
		t.Fatalf("HelpFunc ran %d times before help was rendered", calls)
	}
	if got := flag.HelpText(); got != "lazy" || calls != 1 {
		t.Fatalf("HelpText() = %q after %d calls", got, calls)
	}
}
