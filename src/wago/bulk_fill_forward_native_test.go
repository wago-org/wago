// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build ((linux && amd64) || ((linux || darwin || windows) && arm64)) && !tinygo

package wago

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func filledCopyModule(value int32, intervening []byte, source, count byte) []byte {
	body := []byte{0x20, 1, 0x41}
	body = append(body, wasmtest.SLEB32(value)...)
	body = append(body, 0x20, 2, 0xfc, 11, 0)
	body = append(body, intervening...)
	body = append(body, 0x20, 0, 0x20, source, 0x20, count, 0xfc, 10, 0, 0, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("copy", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

// Compare the full memory, including the already completed fill when the later
// copy traps. Dirty high halves exercise memory32 operand canonicalization.
func TestBulkFillCopyForward(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, value := range []int32{0, 7, 255, 0x12345678, -1} {
			t.Run(fmt.Sprintf("on=%t/value=%d", enabled, value), func(t *testing.T) {
				c, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).
					WithOptimization("bulk-fill-copy-forward", enabled).Compile(filledCopyModule(value, nil, 1, 2))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				in, err := Instantiate(c)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				mem := in.Memory().UnsafeBytes()
				before := make([]byte, len(mem))
				want := make([]byte, len(mem))
				for i := range before {
					before[i] = byte(i*29 + i>>8)
				}
				for _, n := range []int{0, 1, 15, 16, 31, 32, 63, 64, 65, 127, 128, 255, 256, 257, 511, 512, 513, 1023, 1024, 1025, 4096, 65535, 65536} {
					for align := 0; align < 32; align++ {
						for _, pair := range [][2]int{{256 + align, 255}, {255, 256 + align}, {256 + align, 256 + align}, {32768 + align, 128}, {65536 - n, 0}, {0, 65536 - n}, {65537 - n, 0}, {0, 65537 - n}, {65536, 65536}} {
							dst, src := pair[0], pair[1]
							copy(mem, before)
							copy(want, before)
							validFill := src+n <= len(mem)
							validCopy := validFill && dst+n <= len(mem)
							if validFill {
								for i := src; i < src+n; i++ {
									want[i] = byte(value)
								}
							}
							if validCopy {
								copy(want[dst:dst+n], want[src:src+n])
							}
							_, err := in.Invoke("copy", uint64(dst)|0xaabbccdd00000000, uint64(src)|0xaabbccdd00000000, uint64(n)|0xaabbccdd00000000, 0)
							var trap *TrapError
							if validCopy && err != nil || !validCopy && (!errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds) || !bytes.Equal(mem, want) {
								t.Fatalf("n=%d src=%d dst=%d valid=%t err=%v memoryEqual=%t", n, src, dst, validCopy, err, bytes.Equal(mem, want))
							}
						}
					}
				}
			})
		}
	}
}

func TestBulkFillCopyForwardRejectsChangedSource(t *testing.T) {
	// An intervening byte store invalidates uniformity; a changed source/count
	// invalidates the span proof even when the first three leaf operands match.
	for _, tc := range []struct {
		name           string
		between        []byte
		source, count  byte
		copySrc, copyN int
	}{
		{"store", []byte{0x20, 1, 0x41, 42, 0x3a, 0, 0}, 1, 2, 128, 512},
		{"source", nil, 3, 2, 256, 512},
		{"count", nil, 1, 3, 128, 513},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewRuntimeConfig().Compile(filledCopyModule(7, tc.between, tc.source, tc.count))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			mem := in.Memory().UnsafeBytes()
			for i := range mem {
				mem[i] = byte(i)
			}
			want := append([]byte(nil), mem...)
			for i := 128; i < 640; i++ {
				want[i] = 7
			}
			if tc.name == "store" {
				want[128] = 42
			}
			copy(want[2048:2048+tc.copyN], want[tc.copySrc:tc.copySrc+tc.copyN])
			arg3 := tc.copySrc
			if tc.name == "count" {
				arg3 = tc.copyN
			}
			if _, err := in.Invoke("copy", 2048, 128, 512, uint64(arg3)); err != nil || !bytes.Equal(mem, want) {
				t.Fatalf("err=%v memoryEqual=%t", err, bytes.Equal(mem, want))
			}
		})
	}
}

func TestBulkFillCopyForwardAcrossMemories(t *testing.T) {
	for _, source := range []byte{0, 1} {
		destination := 1 - source
		body := []byte{0x20, 1, 0x41, 7, 0x20, 2, 0xfc, 11, source, 0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 10, destination, source, 0x0b}
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 1}, []byte{1, 1, 1})),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("copy", 0, 0), wasmtest.ExportEntry("source", 2, uint32(source)), wasmtest.ExportEntry("destination", 2, uint32(destination)))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
		)
		c, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureMultiMemory).Compile(module)
		if !SupportedFeatures().IsEnabled(CoreFeatureMultiMemory) {
			var unsupported *UnsupportedFeatureError
			if !errors.As(err, &unsupported) || unsupported.Requested != CoreFeatureMultiMemory {
				t.Fatalf("multi-memory admission = %v; want unsupported multi-memory", err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		in, err := Instantiate(c)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		src, err := in.ExportedMemory("source")
		if err != nil {
			t.Fatal(err)
		}
		dst, err := in.ExportedMemory("destination")
		if err != nil {
			t.Fatal(err)
		}
		for i := range dst.UnsafeBytes() {
			dst.UnsafeBytes()[i] = 0x5a
		}
		if _, err := in.Invoke("copy", 129, 256, 4096); err != nil {
			t.Fatal(err)
		}
		wantSource := make([]byte, 65536)
		wantDestination := bytes.Repeat([]byte{0x5a}, 65536)
		for i := 0; i < 4096; i++ {
			wantSource[256+i] = 7
			wantDestination[129+i] = 7
		}
		if !bytes.Equal(src.UnsafeBytes(), wantSource) || !bytes.Equal(dst.UnsafeBytes(), wantDestination) {
			t.Fatalf("source memory %d: wrong cross-memory contents", source)
		}
	}
}

func TestBulkFillCopyForwardInline(t *testing.T) {
	helper := []byte{0x20, 1, 0x41, 7, 0x20, 2, 0xfc, 11, 0, 0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 10, 0, 0, 0x0b}
	caller := []byte{0x20, 0, 0x20, 1, 0x20, 2, 0x10, 0, 0x0b}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("copy", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(helper), wasmtest.Code(caller))),
	)
	c, err := NewRuntimeConfig().Compile(module)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	mem := in.Memory().UnsafeBytes()
	for _, tc := range [][3]int{{1, 0, 65535}, {17, 16, 1024}, {0, 0, 65536}, {61441, 128, 4096}} {
		dst, src, n := tc[0], tc[1], tc[2]
		for i := range mem {
			mem[i] = 0x5a
		}
		want := bytes.Clone(mem)
		for i := src; i < src+n; i++ {
			want[i] = 7
		}
		valid := dst+n <= len(mem)
		if valid {
			copy(want[dst:dst+n], want[src:src+n])
		}
		_, err := in.Invoke("copy", uint64(dst), uint64(src), uint64(n))
		var trap *TrapError
		if valid && err != nil || !valid && (!errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds) || !bytes.Equal(mem, want) {
			t.Fatalf("%v: err=%v memoryEqual=%t", tc, err, bytes.Equal(mem, want))
		}
	}
}
