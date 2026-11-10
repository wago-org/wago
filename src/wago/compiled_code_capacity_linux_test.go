// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux && (amd64 || arm64) && !tinygo) || (linux && riscv64 && !tinygo && !wago_target_tinygo)

package wago

import "testing"

func TestCompiledOnlyImagesDoNotReserveExecutionRanges(t *testing.T) {
	const modules = 4097 // one more than the fixed Linux execution registry
	compiled := make([]*Compiled, 0, modules)
	defer func() {
		for _, c := range compiled {
			if err := c.Close(); err != nil {
				t.Error(err)
			}
		}
	}()
	for i := 0; i < modules; i++ {
		staged := newCompilerCompiled()
		staged.code = []byte{0}
		installCompilerCompiledFinalizer(staged)
		c, err := publishCompilerCompiled(staged)
		if err != nil {
			t.Fatalf("publish compiled-only image %d: %v", i, err)
		}
		compiled = append(compiled, c)
	}
	// Real activation still gets a registry slot and executes correctly.
	c := MustCompile(benchAddOneModule())
	defer c.Close()
	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	got, err := in.Invoke("f", 41)
	if err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("activation after compiled-only cache: %v, %v", got, err)
	}
}
