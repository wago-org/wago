// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestModularLoadGroupMatchesScalar(t *testing.T) {
	old := modularLoadGroupEnabled
	defer func() { modularLoadGroupEnabled = old }()
	// Sum a dense group of words in descending address order inside a loop.
	body := []byte{1, 1, 0x7f, 0x03, 0x40}
	for i, off := range []byte{16, 12, 8, 4, 0} {
		body = append(body, 0x20, 0)
		if off != 0 {
			body = append(body, 0x41, off, 0x6a)
		}
		body = append(body, 0x28, 2, 0)
		if i != 0 {
			body = append(body, 0x6a)
		}
	}
	body = append(body, 0x21, 2, 0x20, 1, 0x41, 1, 0x6b, 0x22, 1, 0x0d, 0, 0x0b, 0x20, 2, 0x0b)
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, address := range []uint64{0, 1, 7, 65516, 65520, 65521, 65524, 65532, 65535, 65536, 0xfffffff0, 0xfffffff1, 0xfffffffc, 0xffffffff, 0xdeadbeef00000007} {
		var want uint64
		var wantMem []byte
		var wantErr string
		for _, enabled := range []bool{false, true} {
			modularLoadGroupEnabled = enabled
			var stats ModuleStats
			got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{Stats: optionalTestStats(&stats)}, func(mem []byte) {
				for i := range mem {
					mem[i] = byte(i*29 + 9)
				}
			}, address, 2)
			errText := strings.SplitN(fmt.Sprint(err), "\n", 2)[0]
			if !enabled {
				want, wantMem, wantErr = got, mem, errText
				continue
			}
			if got != want || errText != wantErr || !bytes.Equal(mem, wantMem) {
				t.Fatalf("address=%x got=%d want=%d err=%v wantErr=%s memoryEqual=%v", address, got, want, err, wantErr, bytes.Equal(mem, wantMem))
			}
			if diagnosticsEnabled && stats.Funcs[0].Peephole["modular-load-group"] == 0 {
				t.Fatalf("group not emitted: %v", stats.Funcs[0].Peephole)
			}
		}
	}
}

func TestModularLoadGroupDenseAdmission(t *testing.T) {
	for _, tc := range []struct {
		mask uint64
		want bool
	}{
		{0b11111, true}, {0b11110, false}, {0b10111, false}, {0b11, false},
		{^uint64(0), true}, {^uint64(0) &^ (1 << 31), false},
	} {
		if got := finishModularLoadGroup(2, 0, tc.mask).valid; got != tc.want {
			t.Fatalf("mask=%x got=%v want=%v", tc.mask, got, tc.want)
		}
	}
}

func TestModularLoadGroupUsesInlineLocalCoordinates(t *testing.T) {
	var raw []byte
	for _, off := range []byte{4, 8, 12, 16} {
		raw = append(raw, 0x20, 0, 0x41, off, 0x6a, 0x28, 2, 0, 0x1a)
	}
	f := &fn{localBase: 5}
	r := wasm.ReaderFrom(raw)
	if g := f.scanModularLoadGroup(&r, 5, 0); !g.valid || g.low != 0 || g.high != 16 {
		t.Fatalf("inlined local not recognized: %+v", g)
	}
	r = wasm.ReaderFrom(raw)
	if g := f.scanModularLoadGroup(&r, 0, 0); g.valid {
		t.Fatalf("different physical local recognized: %+v", g)
	}
	var commuted []byte
	for _, off := range []byte{4, 8, 12, 16} {
		commuted = append(commuted, 0x41, off, 0x20, 0, 0x6a, 0x28, 2, 0, 0x1a)
	}
	r = wasm.ReaderFrom(commuted)
	if g := f.scanModularLoadGroup(&r, 5, 0); !g.valid || g.low != 0 || g.high != 16 {
		t.Fatalf("commuted addresses not recognized: %+v", g)
	}
}

func TestModularLoadGroupInvalidatedByLocalWrite(t *testing.T) {
	old := modularLoadGroupEnabled
	defer func() { modularLoadGroupEnabled = old }()
	body := []byte{0, 0x03, 0x40}
	for _, off := range []byte{16, 12, 8, 4, 0} {
		body = append(body, 0x20, 0)
		if off != 0 {
			body = append(body, 0x41, off, 0x6a)
		}
		body = append(body, 0x28, 2, 0, 0x1a)
	}
	body = append(body, 0x41, 0xe4, 0, 0x41, 0xcd, 0, 0x3a, 0, 0,
		0x20, 2, 0x21, 0, 0x20, 0, 0x41, 4, 0x6a, 0x28, 2, 0, 0x1a,
		0x20, 1, 0x41, 1, 0x6b, 0x22, 1, 0x0d, 0, 0x0b, 0x41, 0, 0x0b)
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, enabled := range []bool{false, true} {
		modularLoadGroupEnabled = enabled
		var stats ModuleStats
		_, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{Stats: optionalTestStats(&stats)}, nil, 0, 1, 65532)
		if err == nil || mem[100] != 77 {
			t.Fatalf("enabled=%v err=%v earlier store=%d", enabled, err, mem[100])
		}
		if enabled && diagnosticsEnabled && stats.Funcs[0].Peephole["modular-load-group"] == 0 {
			t.Fatal("group not emitted")
		}
	}
}

func TestModularLoadGroupDoesNotCrossStores(t *testing.T) {
	old := modularLoadGroupEnabled
	defer func() { modularLoadGroupEnabled = old }()
	body := []byte{0, 0x03, 0x40}
	for _, off := range []byte{12, 8, 4, 0} {
		body = append(body, 0x20, 0)
		if off != 0 {
			body = append(body, 0x41, off, 0x6a)
		}
		body = append(body, 0x28, 2, 0, 0x1a)
	}
	body = append(body, 0x41, 0xe4, 0, 0x41, 0xcd, 0, 0x3a, 0, 0,
		0x20, 0, 0x41, 16, 0x6a, 0x28, 2, 0, 0x1a,
		0x20, 1, 0x41, 1, 0x6b, 0x22, 1, 0x0d, 0, 0x0b, 0x41, 0, 0x0b)
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, enabled := range []bool{false, true} {
		modularLoadGroupEnabled = enabled
		_, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{}, nil, 65520, 1)
		if err == nil || mem[100] != 77 {
			t.Fatalf("enabled=%v err=%v earlier store=%d", enabled, err, mem[100])
		}
	}
}

func TestModularLoadGroupSparseProbeBudget(t *testing.T) {
	var raw [300]byte
	for i := range raw {
		raw[i] = 0x01
	}
	f := &fn{}
	r := wasm.ReaderFrom(raw[:])
	if group := f.scanModularLoadGroup(&r, 0, 0); group.valid || f.boundsScanWork != 24 || r.Offset() != 0 {
		t.Fatalf("group=%+v work=%d reader offset=%d", group, f.boundsScanWork, r.Offset())
	}
}
