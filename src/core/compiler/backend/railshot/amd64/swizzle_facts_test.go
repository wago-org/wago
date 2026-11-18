// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestSwizzleFactMetadataAndTransfer(t *testing.T) {
	var st storage
	st.setGCRoot(true)
	st.setEHRoot(true)
	st.setLogicalRoot(true)
	st.setValueFacts(factBytesBelow16)
	if !st.valueFacts().has(factBytesBelow16) || !st.hasGCRoot() || !st.hasEHRoot() || !st.hasLogicalRoot() {
		t.Fatal("fact overlaps root metadata")
	}
	st.setValueFacts(0)
	if st.valueFacts() != 0 || !st.hasGCRoot() || !st.hasEHRoot() || !st.hasLogicalRoot() {
		t.Fatal("clearing fact damaged root metadata")
	}
	for _, op := range []simdBinaryOp{opVPand, opVPor, opVPxor, opVPaddb} {
		for _, a := range []valueFacts{0, factBytesBelow16} {
			for _, b := range []valueFacts{0, factBytesBelow16} {
				got := vectorBinaryFacts(op, a, b)
				for x := 0; x < 256; x++ {
					for y := 0; y < 256; y++ {
						if a != 0 && x >= 16 || b != 0 && y >= 16 {
							continue
						}
						var z int
						switch op {
						case opVPand:
							z = x & y
						case opVPor:
							z = x | y
						case opVPxor:
							z = x ^ y
						default:
							z = (x + y) & 255
						}
						if got.has(factBytesBelow16) && z >= 16 {
							t.Fatalf("unsound transfer op%x facts%x/%x x%d y%d z%d", op, a, b, x, y, z)
						}
					}
				}
			}
		}
	}
}

func TestSwizzleFactsByteBoundaries(t *testing.T) {
	requireCompilerDiagnostics(t)
	var table [16]byte
	for i := range table {
		table[i] = byte(17*i + 3)
	}
	literal := func(v byte) []byte {
		b := []byte{0xfd, 12}
		for i := 0; i < 16; i++ {
			b = append(b, v)
		}
		return b
	}
	mask := func(v byte) []byte { return append(literal(v), 0xfd, 78) }
	type testCase struct {
		name    string
		build   func() []byte
		eval    func(byte) byte
		bounded bool
	}
	get := func() []byte { return []byte{0x20, 0} }
	cases := []testCase{
		{"unknown", get, func(x byte) byte { return x }, false},
		{"constant", func() []byte { return literal(15) }, func(x byte) byte { return 15 }, true},
		{"constant-high", func() []byte { return literal(16) }, func(x byte) byte { return 16 }, false},
		{"and", func() []byte { return append(get(), mask(15)...) }, func(x byte) byte { return x & 15 }, true},
		{"and-reversed", func() []byte { return append(append(literal(15), get()...), 0xfd, 78) }, func(x byte) byte { return x & 15 }, true},
		{"and-too-wide", func() []byte { return append(get(), mask(31)...) }, func(x byte) byte { return x & 31 }, false},
		{"xor", func() []byte { return append(append(append(get(), mask(15)...), literal(7)...), 0xfd, 81) }, func(x byte) byte { return (x & 15) ^ 7 }, true},
		{"or", func() []byte { return append(append(append(get(), mask(7)...), literal(8)...), 0xfd, 80) }, func(x byte) byte { return (x & 7) | 8 }, true},
		{"or-wide", func() []byte { return append(append(append(get(), mask(15)...), literal(16)...), 0xfd, 80) }, func(x byte) byte { return (x & 15) | 16 }, false},
		{"add-clears", func() []byte { return append(append(append(get(), mask(15)...), literal(16)...), 0xfd, 110) }, func(x byte) byte { return (x & 15) + 16 }, false},
		{"not-clears", func() []byte { return append(append(get(), mask(15)...), 0xfd, 77) }, func(x byte) byte { return ^(x & 15) }, false},
		{"shr3", func() []byte { return append(get(), 0x41, 3, 0xfd, 109) }, func(x byte) byte { return x >> 3 }, false},
		{"shr4", func() []byte { return append(get(), 0x41, 4, 0xfd, 109) }, func(x byte) byte { return x >> 4 }, true},
		{"shr12", func() []byte { return append(get(), 0x41, 12, 0xfd, 109) }, func(x byte) byte { return x >> 4 }, true},
		{"signed-shr", func() []byte { return append(get(), 0x41, 4, 0xfd, 108) }, func(x byte) byte { return byte(int8(x) >> 4) }, false},
		{"shift-zero", func() []byte { return append(append(get(), mask(15)...), 0x41, 0, 0xfd, 107) }, func(x byte) byte { return x & 15 }, true},
		{"shl-clears", func() []byte { return append(append(get(), mask(15)...), 0x41, 1, 0xfd, 107) }, func(x byte) byte { return (x & 15) << 1 }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte{0, 0xfd, 12}
			body = append(body, table[:]...)
			body = append(body, tc.build()...)
			body = append(body, 0xfd, 14, 0x0b)
			m := mod1(t, []wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}, body)
			for _, enabled := range []bool{false, true} {
				for _, pins := range []bool{false, true} {
					for _, features := range []shared.AMD64Features{0, shared.AMD64SSSE3, shared.AMD64ModernBaseline} {
						opts := CompileOptions{AMD64FeaturesSet: true, AMD64Features: features, Optimizations: map[string]bool{"value-facts": enabled, "v128-pins": pins}}
						var stats ModuleStats
						opts.Stats = &stats
						cm, err := CompileModuleWith(m, opts)
						if err != nil {
							t.Fatal(err)
						}
						cm.CodeImage.Close()
						opts.Stats = nil
						if got, want := stats.Funcs[0].Peephole["simd-swizzle-in-range"] != 0, enabled && tc.bounded; got != want {
							t.Fatalf("enabled%v pins%v features%x fact=%v want%v", enabled, pins, features, got, want)
						}
						for base := 0; base < 256; base += 16 {
							var input, want [16]byte
							for i := range input {
								input[i] = byte(base + i)
								idx := tc.eval(input[i])
								if idx < 16 {
									want[i] = table[idx]
								}
							}
							if got := runAmd64V128WithOptions(t, m, &input, opts); got != want {
								t.Fatalf("enabled%v pins%v features%x base%d got%x want%x", enabled, pins, features, base, got, want)
							}
						}
					}
				}
			}
		})
	}
}

func TestSwizzleFactsTeeAndOverwrite(t *testing.T) {
	requireCompilerDiagnostics(t)
	var table [16]byte
	for i := range table {
		table[i] = byte(19*i + 7)
	}
	literal := func(v [16]byte) []byte { return append([]byte{0xfd, 12}, v[:]...) }
	var mask [16]byte
	for i := range mask {
		mask[i] = 15
	}
	for _, alias := range []bool{false, true} {
		for _, overwrite := range []bool{false, true} {
			if overwrite && !alias {
				continue
			}
			body := []byte{1, 1, 0x7b}
			if !alias {
				body = append(body, literal(table)...)
			}
			body = append(body, 0x20, 0)
			body = append(body, literal(mask)...)
			body = append(body, 0xfd, 78, 0x22, 1)
			if alias {
				body = append(body, literal(table)...)
				if overwrite {
					body = append(body, 0x20, 0, 0x21, 1)
				}
				body = append(body, 0x20, 1)
			}
			body = append(body, 0xfd, 14)
			if alias {
				body = append(body, 0xfd, 81)
			} else {
				body = append(body, 0x20, 1, 0x1a)
			}
			body = append(body, 0x0b)
			m := mod1(t, []wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}, body)
			for _, pins := range []bool{false, true} {
				for _, enabled := range []bool{false, true} {
					opts := CompileOptions{Optimizations: map[string]bool{"value-facts": enabled, "v128-pins": pins}}
					var stats ModuleStats
					opts.Stats = &stats
					cm, err := CompileModuleWith(m, opts)
					if err != nil {
						t.Fatal(err)
					}
					cm.CodeImage.Close()
					opts.Stats = nil
					fast := stats.Funcs[0].Peephole["simd-swizzle-in-range"] != 0
					if overwrite || !enabled {
						if fast {
							t.Fatal("stale or disabled fact authorized normalization elision")
						}
					} else if !alias || stats.Funcs[0].Peephole["simd-local-forward"] != 0 {
						if !fast {
							t.Fatalf("copy lost fact pins%v alias%v", pins, alias)
						}
					}
					for base := 0; base < 256; base += 16 {
						var input, want [16]byte
						for i := range input {
							input[i] = byte(base + i)
							idx := input[i] & 15
							if overwrite {
								idx = input[i]
							}
							if idx < 16 {
								want[i] = table[idx]
							}
							if alias {
								want[i] ^= input[i] & 15
							}
						}
						if got := runAmd64V128WithOptions(t, m, &input, opts); got != want {
							t.Fatalf("alias%v overwrite%v pins%v facts%v base%d got%x want%x", alias, overwrite, pins, enabled, base, got, want)
						}
					}
				}
			}
		}
	}
}
