// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && amd64 && !tinygo

package amd64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoderamd64 "github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/src/core/runtime"
)

func extmulWordBody(op uint32, same bool, pressure, count int) []byte {
	body := []byte{0}
	for i := 0; i < pressure; i++ {
		body = append(body, 0x20, 0)
		body = append(body, simdOp(77)...)
	}
	body = append(body, 0x20, 0)
	for i := 0; i < count; i++ {
		other := byte(1)
		if same {
			other = 0
		}
		body = append(body, 0x20, other)
		body = append(body, simdOp(op)...)
	}
	for i := 0; i < pressure; i++ {
		body = append(body, simdOp(81)...)
	}
	return append(body, 0x0b)
}

func extmulWordRunner(t testing.TB, m *wasm.Module, features shared.AMD64Features) (func([16]byte, [16]byte) [16]byte, int) {
	t.Helper()
	cm, err := CompileModuleWith(m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: features})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		t.Cleanup(func() { _ = cm.CodeImage.Close() })
	}
	if cm.RequiredAMD64Features & ^uint32(features) != 0 {
		t.Fatalf("unexpected CPU requirements: %x", cm.RequiredAMD64Features)
	}
	eng, err := runtime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { eng.Close() })
	jm, err := runtime.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { jm.Close() })
	ar, err := runtime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ar.Close() })
	mem, entry, err := runtime.MapCode(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.Unmap(mem) })
	args, results, trap := ar.Alloc(256), ar.Alloc(256), ar.Alloc(runtime.TrapBufferBytes)
	return func(a, b [16]byte) (out [16]byte) {
		copy(args, a[:])
		copy(args[16:], b[:])
		if err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results); err != nil {
			t.Fatal(err)
		}
		copy(out[:], results[:16])
		return
	}, len(cm.Code)
}

func TestExtmulWordProducts(t *testing.T) {
	// The scalar oracle is independent of both native and SSE2 lowering.
	for _, features := range []shared.AMD64Features{0, shared.AMD64SSSE3 | shared.AMD64SSE41, shared.AMD64ModernBaseline} {
		for op := uint32(188); op <= 191; op++ {
			for _, same := range []bool{false, true} {
				for _, pressure := range []int{0, 20} {
					t.Run(fmt.Sprintf("features_%x/op_%d/same_%v/pressure_%d", features, op, same, pressure), func(t *testing.T) {
						m := mod1(t, []wasm.ValType{wasm.V128, wasm.V128}, []wasm.ValType{wasm.V128}, extmulWordBody(op, same, pressure, 1))
						run, _ := extmulWordRunner(t, m, features)
						rng := rand.New(rand.NewSource(912))
						bounds := []uint16{0, 1, 2, 0x7fff, 0x8000, 0x8001, 0xfffe, 0xffff}
						for n := 0; n < 1088; n++ {
							var a, b, want [16]byte
							rng.Read(a[:])
							rng.Read(b[:])
							if n < 64 {
								for i := 0; i < 8; i++ {
									binary.LittleEndian.PutUint16(a[i*2:], bounds[(n/8+i)%8])
									binary.LittleEndian.PutUint16(b[i*2:], bounds[(n%8+i)%8])
								}
							}
							if same {
								b = a
							}
							first := 0
							if op&1 != 0 {
								first = 4
							}
							for i := 0; i < 4; i++ {
								x := binary.LittleEndian.Uint16(a[(first+i)*2:])
								y := binary.LittleEndian.Uint16(b[(first+i)*2:])
								product := uint32(x) * uint32(y)
								if op < 190 {
									product = uint32(int32(int16(x)) * int32(int16(y)))
								}
								binary.LittleEndian.PutUint32(want[i*4:], product)
							}
							if got := run(a, b); got != want {
								t.Fatalf("n=%d a=%x b=%x got=%x want=%x", n, a, b, got, want)
							}
						}
					})
				}
			}
		}
	}
}

func BenchmarkExtmulWordProducts(b *testing.B) {
	for _, features := range []shared.AMD64Features{0, shared.AMD64SSSE3 | shared.AMD64SSE41, shared.AMD64ModernBaseline} {
		for _, op := range []uint32{188, 191} {
			for _, count := range []int{1, 64} {
				b.Run(fmt.Sprintf("features_%x/op_%d/count_%d", features, op, count), func(b *testing.B) {
					m := mod1(b, []wasm.ValType{wasm.V128, wasm.V128}, []wasm.ValType{wasm.V128}, extmulWordBody(op, false, 0, count))
					b.Run("compile", func(b *testing.B) {
						b.ReportAllocs()
						for i := 0; i < b.N; i++ {
							cm, err := CompileModuleWith(m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: features})
							if err != nil {
								b.Fatal(err)
							}
							if cm.CodeImage != nil {
								if err := cm.CodeImage.Close(); err != nil {
									b.Fatal(err)
								}
							}
						}
					})
					b.Run("execute", func(b *testing.B) {
						run, size := extmulWordRunner(b, m, features)
						a := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
						c := a
						b.ReportAllocs()
						b.ResetTimer()
						for i := 0; i < b.N; i++ {
							run(a, c)
						}
						b.ReportMetric(float64(size), "native-bytes")
					})
				})
			}
		}
	}
}

func TestExtmulWordInstructionEncoding(t *testing.T) {
	for _, tc := range []struct {
		op     simdBinaryOp
		opcode byte
	}{{opVPmulhw, 0xe5}, {opVPmulhuw, 0xe4}} {
		for _, avx := range []bool{false, true} {
			var a encoderamd64.Asm
			sc := scratch{}
			if avx {
				sc.amd64Features = shared.AMD64AVX
			}
			f := fn{a: &a, sc: &sc}
			tc.op.emit(&f, 0, 0, 1)
			want := []byte{0x66, 0x0f, tc.opcode, 0xc1}
			if avx {
				want = []byte{0xc4, 0xe1, 0x79, tc.opcode, 0xc1}
			}
			if !bytes.Equal(a.B, want) {
				t.Fatalf("op=%x avx=%v got=%x want=%x", tc.opcode, avx, a.B, want)
			}
		}
	}
}

func TestExtmulWordCachedConstants(t *testing.T) {
	before := v128ConstCacheEnabled
	defer func() { v128ConstCacheEnabled = before }()
	a := i16x8Bytes(-32768, -1, 32767, 1234, 32767, -32768, 1, -1)
	b := i16x8Bytes(-1, -32768, 1234, 32767, -32768, 2, -1, 32767)
	for _, cache := range []bool{false, true} {
		v128ConstCacheEnabled = cache
		for _, features := range []shared.AMD64Features{0, shared.AMD64SSSE3 | shared.AMD64SSE41, shared.AMD64ModernBaseline} {
			for op := uint32(188); op <= 191; op++ {
				body := append([]byte{0}, v128ConstBytes(b)...)
				body = append(body, v128ConstBytes(a)...)
				body = append(body, v128ConstBytes(b)...)
				body = append(body, simdOp(op)...)
				body = append(body, simdOp(81)...)
				body = append(body, 0x0b)
				m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
				run, _ := extmulWordRunner(t, m, features)
				first := 0
				if op&1 != 0 {
					first = 4
				}
				var want [16]byte
				for i := 0; i < 4; i++ {
					x := binary.LittleEndian.Uint16(a[(first+i)*2:])
					y := binary.LittleEndian.Uint16(b[(first+i)*2:])
					p := uint32(x) * uint32(y)
					if op < 190 {
						p = uint32(int32(int16(x)) * int32(int16(y)))
					}
					binary.LittleEndian.PutUint32(want[i*4:], p)
				}
				for i := range want {
					want[i] ^= b[i]
				}
				if got := run(a, b); got != want {
					t.Fatalf("cache=%v profile=%x op=%d got=%x want=%x", cache, features, op, got, want)
				}
			}
		}
	}
}
