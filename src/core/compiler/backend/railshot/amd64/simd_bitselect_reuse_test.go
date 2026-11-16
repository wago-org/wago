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
	"math/rand"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func bitselectModule(tb testing.TB, n, live, alias int) *wasm.Module {
	tb.Helper()
	body := []byte{0}
	// Leave computed values alive across the entire bitselect chain.
	for i := 0; i < live; i++ {
		body = append(body, 0x20, 0)
		var salt [16]byte
		for j := range salt {
			salt[j] = byte(i*19 + j*7)
		}
		body = append(body, v128ConstBytes(salt)...)
		body = append(body, simdOp(81)...)
	}
	body = append(body, 0x20, 0)
	for i := 0; i < n; i++ {
		b, mask := byte(1), byte(2)
		if alias == 1 || alias == 4 {
			b = 0
		}
		if alias == 2 || alias == 4 {
			mask = 0
		}
		if alias == 3 {
			mask = 1
		}
		body = append(body, 0x20, b, 0x20, mask)
		body = append(body, simdOp(82)...)
	}
	// Include the preserved input locals and earlier live values in the result.
	if live > 0 {
		for _, local := range []byte{0, 1, 2} {
			body = append(body, 0x20, local)
			body = append(body, simdOp(81)...)
		}
	}
	for i := 0; i < live; i++ {
		body = append(body, simdOp(81)...)
	}
	body = append(body, 0x0b)
	raw := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128, wasm.V128, wasm.V128}, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
	m, err := wasm.DecodeModule(raw)
	if err != nil {
		tb.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		tb.Fatal(err)
	}
	return m
}

// Every mapped image and native execution allocation is closed, including in
// compilation benchmarks. Inputs are serialized parameters, not constants.
func bitselectRunner(tb testing.TB, m *wasm.Module, opts CompileOptions) (func([3][16]byte) [16]byte, int) {
	tb.Helper()
	cm, err := CompileModuleWith(m, opts)
	if err != nil {
		tb.Fatal(err)
	}
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
	return func(v [3][16]byte) (out [16]byte) {
		for i := range v {
			copy(args[i*16:], v[i][:])
		}
		if err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results); err != nil {
			tb.Fatal(err)
		}
		copy(out[:], results[:16])
		return
	}, len(cm.Code)
}

func TestBitselectOwnedMask(t *testing.T) {
	for _, features := range []shared.AMD64Features{0, shared.AMD64SSSE3 | shared.AMD64SSE41, shared.AMD64ModernBaseline} {
		for _, live := range []int{0, 21} {
			for _, alias := range []int{0, 1, 2, 3, 4} {
				t.Run(fmt.Sprintf("features=%x/live=%d/alias=%v", features, live, alias), func(t *testing.T) {
					opts := CompileOptions{AMD64FeaturesSet: true, AMD64Features: features, Optimizations: map[string]bool{"v128-pins": true, "reg-abi": true}}
					run, _ := bitselectRunner(t, bitselectModule(t, 1, live, alias), opts)
					rng := rand.New(rand.NewSource(95))
					for k := 0; k < 4096; k++ {
						var v [3][16]byte
						for i := range v {
							rng.Read(v[i][:])
						}
						if k < 256 {
							for i := range v[2] {
								v[2][i] = byte(k)
							}
						}
						var want [16]byte
						for i := range want {
							b := v[1][i]
							if alias == 1 || alias == 4 {
								b = v[0][i]
							}
							mask := v[2][i]
							if alias == 2 || alias == 4 {
								mask = v[0][i]
							}
							if alias == 3 {
								mask = v[1][i]
							}
							want[i] = (v[0][i] & mask) | (b &^ mask)
							if live > 0 {
								want[i] ^= v[0][i] ^ v[1][i] ^ v[2][i]
							}
							for j := 0; j < live; j++ {
								want[i] ^= v[0][i] ^ byte(j*19+i*7)
							}
						}
						if got := run(v); got != want {
							t.Fatalf("case %d got=%x want=%x", k, got, want)
						}
					}
				})
			}
		}
	}
}

var bitselectSink [16]byte

func BenchmarkBitselectOwnedMask(b *testing.B) {
	for _, profile := range []struct {
		name     string
		features shared.AMD64Features
	}{{"SSE2", 0}, {"SSE41", shared.AMD64SSSE3 | shared.AMD64SSE41}, {"AVX", shared.AMD64ModernBaseline}} {
		for _, n := range []int{1, 64} {
			b.Run(fmt.Sprintf("%s/chain%d", profile.name, n), func(b *testing.B) {
				m := bitselectModule(b, n, 0, 0)
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
					run, size := bitselectRunner(b, m, opts)
					var v [3][16]byte
					for i := range v {
						for j := range v[i] {
							v[i][j] = byte(i*71 + j*13)
						}
					}
					var want [16]byte
					for i := range want {
						want[i] = (v[0][i] & v[2][i]) | (v[1][i] &^ v[2][i])
					}
					if got := run(v); got != want {
						b.Fatalf("preflight got=%x want=%x", got, want)
					}
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						bitselectSink = run(v)
					}
					b.ReportMetric(float64(size), "code-bytes")
				})
			})
		}
	}
}
