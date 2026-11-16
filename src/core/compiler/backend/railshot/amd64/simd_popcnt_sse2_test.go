// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && amd64 && !tinygo

package amd64

import (
	"fmt"
	"math/bits"
	"os"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	x86 "github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func popcntModule(tb testing.TB, body []byte) *wasm.Module {
	tb.Helper()
	raw := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
	m, err := wasm.DecodeModule(raw)
	if err != nil {
		tb.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		tb.Fatal(err)
	}
	return m
}

// Each owner, including the original native code image, is closed at cleanup.
func popcntRunner(tb testing.TB, cm *x86.CompiledModule) func([16]byte) [16]byte {
	tb.Helper()
	if cm.CodeImage != nil {
		tb.Cleanup(func() { cm.CodeImage.Close() })
	}
	eng, err := runtime.NewEngine()
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { eng.Close() })
	jm, err := runtime.NewJobMemory(65536)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { jm.Close() })
	ar, err := runtime.NewArena(4096)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { ar.Close() })
	mem, entry, err := runtime.MapCode(cm.Code)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { runtime.Unmap(mem) })
	args, results, trap := ar.Alloc(256), ar.Alloc(256), ar.Alloc(runtime.TrapBufferBytes)
	return func(in [16]byte) (out [16]byte) {
		copy(args, in[:])
		if err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results); err != nil {
			tb.Fatal(err)
		}
		copy(out[:], results[:16])
		return
	}
}

func TestPackedPopcntProfiles(t *testing.T) {
	for _, features := range []shared.AMD64Features{0, shared.AMD64SSSE3, shared.AMD64SSSE3 | shared.AMD64SSE41, shared.AMD64ModernBaseline} {
		t.Run(fmt.Sprintf("features=%x", features), func(t *testing.T) {
			// A second read of the argument must retain its original value, even when pinned.
			for _, preserve := range []bool{false, true} {
				body := []byte{0, 0x20, 0, 0xfd, 0x62}
				if preserve {
					body = append(body, 0x20, 0, 0xfd, 0x51)
				} // v128.xor
				body = append(body, 0x0b)
				cm, err := CompileModuleWith(popcntModule(t, body), CompileOptions{AMD64FeaturesSet: true, AMD64Features: features})
				if err != nil {
					t.Fatal(err)
				}
				if features == 0 && cm.RequiredAMD64Features != 0 {
					t.Fatalf("baseline requires %x", cm.RequiredAMD64Features)
				}
				run := popcntRunner(t, cm)
				for pair := 0; pair < 65536; pair++ {
					var in, want [16]byte
					for lane := 0; lane < 16; lane += 2 {
						in[lane] = byte(pair + lane*17)
						in[lane+1] = byte(pair >> 8)
					}
					for lane, v := range in {
						want[lane] = byte(bits.OnesCount8(v))
						if preserve {
							want[lane] ^= v
						}
					}
					if got := run(in); got != want {
						t.Fatalf("preserve=%v input=%x got=%x want=%x", preserve, in, got, want)
					}
				}
			}
		})
	}
}

func TestPackedPopcntPressure(t *testing.T) {
	for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
		// Twenty independent live results exceed the XMM file. Consume every result.
		body := []byte{0}
		for i := 0; i < 20; i++ {
			body = append(body, 0x20, 0)
			body = append(body, v128ConstBytes(i8x16Bytes(int8(i)))...)
			body = append(body, 0xfd, 0x51, 0xfd, 0x62)
		}
		for i := 1; i < 20; i++ {
			body = append(body, 0xfd, 0x6e)
		} // i8x16.add
		body = append(body, 0x0b)
		cm, err := CompileModuleWith(popcntModule(t, body), CompileOptions{AMD64FeaturesSet: true, AMD64Features: features})
		if err != nil {
			t.Fatal(err)
		}
		run := popcntRunner(t, cm)
		for v := 0; v < 256; v++ {
			var in, want [16]byte
			for lane := range in {
				in[lane] = byte(v + lane)
				for i := 0; i < 20; i++ {
					x := in[lane]
					if lane == 0 {
						x ^= byte(i)
					}
					want[lane] += byte(bits.OnesCount8(x))
				}
			}
			if got := run(in); got != want {
				t.Fatalf("features=%x input=%x got=%x want=%x", features, in, got, want)
			}
		}
	}
}

func BenchmarkPackedPopcnt(b *testing.B) {
	for _, profile := range []struct {
		name     string
		features shared.AMD64Features
	}{{"SSE2", 0}, {"SSSE3", shared.AMD64SSSE3}, {"Modern", shared.AMD64ModernBaseline}} {
		for _, n := range []int{1, 64} {
			b.Run(fmt.Sprintf("%s/n%d", profile.name, n), func(b *testing.B) {
				body := []byte{0, 0x20, 0}
				for i := 0; i < n; i++ {
					body = append(body, 0xfd, 0x62)
				}
				body = append(body, 0x0b)
				m := popcntModule(b, body)
				opts := CompileOptions{AMD64FeaturesSet: true, AMD64Features: profile.features}
				b.Run("Compile", func(b *testing.B) {
					b.ReportAllocs()
					for i := 0; i < b.N; i++ {
						cm, err := CompileModuleWith(m, opts)
						if err != nil {
							b.Fatal(err)
						}
						if cm.CodeImage != nil {
							cm.CodeImage.Close()
						}
					}
				})
				b.Run("Execute", func(b *testing.B) {
					cm, err := CompileModuleWith(m, opts)
					if err != nil {
						b.Fatal(err)
					}
					run := popcntRunner(b, cm)
					codeBytes := len(cm.Code)
					input := [16]byte{255, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
					want := input
					for k := 0; k < n; k++ {
						for lane, v := range want {
							want[lane] = byte(bits.OnesCount8(v))
						}
					}
					if got := run(input); got != want {
						b.Fatalf("preflight: got %x want %x", got, want)
					}
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						benchSIMDV128Sink = run(input)
					}
					b.ReportMetric(float64(codeBytes), "code-bytes")
				})
			})
		}
	}
}

func TestPackedPopcntWithoutCache(t *testing.T) {
	body := []byte{0, 0x20, 0, 0xfd, 0x62, 0x20, 0, 0xfd, 0x51, 0x0b}
	cm, err := CompileModuleWith(popcntModule(t, body), CompileOptions{AMD64FeaturesSet: true, Optimizations: map[string]bool{"v128-const-cache": false}})
	if err != nil {
		t.Fatal(err)
	}
	run := popcntRunner(t, cm)
	for pair := 0; pair < 65536; pair++ {
		var in, want [16]byte
		for lane := 0; lane < 16; lane += 2 {
			in[lane] = byte(pair + lane*17)
			in[lane+1] = byte(pair >> 8)
		}
		for lane, v := range in {
			want[lane] = v ^ byte(bits.OnesCount8(v))
		}
		if got := run(in); got != want {
			t.Fatalf("input=%x got=%x want=%x", in, got, want)
		}
	}
}

func BenchmarkPackedPopcntCorpusCompile(b *testing.B) {
	for _, name := range []string{"json-as-simd", "blake-as-simd", "utf-as-simd"} {
		data, err := os.ReadFile("../../../../../../corpus/workloads/assemblyscript/" + name + ".wasm")
		if err != nil {
			b.Fatal(err)
		}
		m, err := wasm.DecodeModule(data)
		if err != nil {
			b.Fatal(err)
		}
		for _, profile := range []struct {
			name     string
			features shared.AMD64Features
		}{{"SSE2", 0}, {"Modern", shared.AMD64ModernBaseline}} {
			b.Run(name+"/"+profile.name, func(b *testing.B) {
				opts := CompileOptions{AMD64FeaturesSet: true, AMD64Features: profile.features}
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					cm, err := CompileModuleWith(m, opts)
					if err != nil {
						b.Fatal(err)
					}
					if cm.CodeImage != nil {
						cm.CodeImage.Close()
					}
				}
			})
		}
	}
}
