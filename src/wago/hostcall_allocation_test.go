// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build ((linux || darwin || windows) && (amd64 || arm64)) || (linux && riscv64 && !tinygo && !wago_target_tinygo)

package wago

import "testing"

func TestScalarSyncHostCallUsesOneScopedHandleAllocation(t *testing.T) {
	compiled := MustCompile(benchReturningImportModule())
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{Imports: testImports("env.f", slotHostFunc(func(_ HostModule, params, results []uint64) {
		results[0] = params[0] + 1
	}))})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("g", I32(1)); err != nil || len(got) != 1 || got[0] != 2 {
		t.Fatalf("warm host call = %v, %v", got, err)
	}
	allocs := testing.AllocsPerRun(100, func() {
		if _, err := instance.Invoke("g", I32(1)); err != nil {
			panic(err)
		}
	})
	if allocs > 1 {
		t.Fatalf("scalar synchronous host-call allocations = %.1f, want at most 1", allocs)
	}
}

func TestTypedScalarSyncHostCallAllocatesNothing(t *testing.T) {
	compiled := MustCompile(benchReturningImportModule())
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{Imports: testImports("env.f", func(v int32) int32 { return v + 1 })})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("g", I32(1)); err != nil || len(got) != 1 || got[0] != 2 {
		t.Fatalf("warm typed host call = %v, %v", got, err)
	}
	allocs := testing.AllocsPerRun(100, func() {
		if _, err := instance.Invoke("g", I32(1)); err != nil {
			panic(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("typed scalar synchronous host-call allocations = %.1f, want 0", allocs)
	}
}
