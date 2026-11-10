// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && amd64 && !tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func bulkCopyAppendModule(dst, src, n, address, offset uint32, before, between, expression []byte) []byte {
	body := append([]byte(nil), before...)
	for _, x := range []uint32{dst, src, n} {
		body = append(body, 0x41)
		body = append(body, wasmtest.SLEB32(int32(x))...)
	}
	body = append(body, 0xfc, 10, 0, 0)
	body = append(body, between...)
	body = append(body, 0x41)
	body = append(body, wasmtest.SLEB32(int32(address))...)
	body = append(body, expression...)
	body = append(body, 0x3a, 0)
	body = append(body, wasmtest.ULEB(offset)...)
	if len(before) != 0 {
		body = append(body, 0x1a)
	}
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("shift", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestBulkCopyAppendMemoryAndTrapOrder(t *testing.T) {
	expression := []byte{0x20, 0, 0x41, 11, 0x6c, 0x20, 1, 0x6a}
	for _, enabled := range []bool{false, true} {
		for n := uint32(0); n <= 65; n++ {
			for _, dst := range []uint32{0, 1, 15, 31, 65536 - n - 1, 65536 - n} {
				for _, variant := range []int{0, 1, 2} {
					src, address, offset := dst+1, dst+n, uint32(0)
					if variant == 1 {
						src++
					}
					if variant == 2 {
						address, offset = 0xffffffff, 1
					}
					c, err := NewRuntimeConfig().WithOptimization("bulk-copy-vector", enabled).Compile(bulkCopyAppendModule(dst, src, n, address, offset, nil, nil, expression))
					if err != nil {
						t.Fatal(err)
					}
					in, err := Instantiate(c)
					if err != nil {
						c.Close()
						t.Fatal(err)
					}
					mem := in.Memory().UnsafeBytes()
					for j := range mem {
						mem[j] = byte(j*29 + 7)
					}
					want := bytes.Clone(mem)
					for iteration := uint32(0); iteration < 3; iteration++ {
						validCopy := uint64(src)+uint64(n) <= uint64(len(mem)) && uint64(dst)+uint64(n) <= uint64(len(mem))
						if validCopy {
							copy(want[dst:dst+n], want[src:src+n])
						}
						valid := validCopy && uint64(address)+uint64(offset) < uint64(len(mem))
						v, bias := uint32(0xfffffff0)+iteration, uint32(0x87654321)
						if valid {
							want[uint64(address)+uint64(offset)] = byte(v*11 + bias)
						}
						_, err = in.Invoke("shift", uint64(v)|0xabcdabcd00000000, uint64(bias)|0xffeeddcc00000000)
						var trap *TrapError
						equal := bytes.Equal(mem, want)
						if valid && err != nil || !valid && (!errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds) || !equal {
							in.Close()
							c.Close()
							t.Fatalf("on=%t n=%d dst=%d variant=%d iteration=%d err=%v memoryEqual=%t", enabled, n, dst, variant, iteration, err, equal)
						}
					}
					in.Close()
					c.Close()
				}
			}
		}
	}
}

func TestBulkCopyAppendEarlierTraps(t *testing.T) {
	for index, before := range [][]byte{
		{0x41, 7, 0x20, 0, 0x6d},
		append(append([]byte{0x41}, wasmtest.SLEB32(65536)...), 0x2d, 0, 0),
	} {
		c, err := NewRuntimeConfig().Compile(bulkCopyAppendModule(0, 1, 31, 31, 0, before, nil, []byte{0x20, 1}))
		if err != nil {
			t.Fatal(err)
		}
		in, err := Instantiate(c)
		if err != nil {
			c.Close()
			t.Fatal(err)
		}
		mem := in.Memory().UnsafeBytes()
		for j := range mem {
			mem[j] = byte(j*17 + 5)
		}
		want := bytes.Clone(mem)
		_, err = in.Invoke("shift", 0, 99)
		var trap *TrapError
		wantTrap := TrapDivZero
		if index == 1 {
			wantTrap = TrapLinMemOutOfBounds
		}
		if !errors.As(err, &trap) || trap.Code != wantTrap || !bytes.Equal(mem, want) {
			t.Fatalf("earlier=%x err=%v memoryEqual=%t", before, err, bytes.Equal(mem, want))
		}
		in.Close()
		c.Close()
	}
}

func TestBulkCopyAppendExpressions(t *testing.T) {
	for _, tc := range []struct {
		expression []byte
		oracle     func(uint32, uint32) uint32
	}{
		{[]byte{0x41, 0x7f}, func(a, b uint32) uint32 { return 0xffffffff }},
		{[]byte{0x20, 0}, func(a, b uint32) uint32 { return a }},
		{[]byte{0x20, 0, 0x20, 1, 0x6a}, func(a, b uint32) uint32 { return a + b }},
		{[]byte{0x20, 0, 0x20, 1, 0x6b}, func(a, b uint32) uint32 { return a - b }},
		{[]byte{0x20, 0, 0x20, 1, 0x6c}, func(a, b uint32) uint32 { return a * b }},
		{[]byte{0x20, 0, 0x20, 1, 0x71}, func(a, b uint32) uint32 { return a & b }},
		{[]byte{0x20, 0, 0x20, 1, 0x72}, func(a, b uint32) uint32 { return a | b }},
		{[]byte{0x20, 0, 0x20, 1, 0x73}, func(a, b uint32) uint32 { return a ^ b }},
		{[]byte{0x20, 0, 0x41, 11, 0x20, 1, 0x41, 3, 0x6a, 0x6c, 0x6b}, func(a, b uint32) uint32 { return a - 11*(b+3) }},
	} {
		for _, n := range []uint32{15, 31, 47, 63} {
			c, err := NewRuntimeConfig().Compile(bulkCopyAppendModule(17, 18, n, 17+n, 0, nil, nil, tc.expression))
			if err != nil {
				t.Fatal(err)
			}
			in, err := Instantiate(c)
			if err != nil {
				c.Close()
				t.Fatal(err)
			}
			mem := in.Memory().UnsafeBytes()
			for j := range mem {
				mem[j] = byte(j*13 + 17)
			}
			want := bytes.Clone(mem)
			for _, values := range [][2]uint32{{0, 0}, {0xffffffff, 3}, {7, 0xfffffffe}, {0x80000000, 0x87654321}} {
				copy(want[17:17+n], want[18:18+n])
				want[17+n] = byte(tc.oracle(values[0], values[1]))
				_, err = in.Invoke("shift", uint64(values[0])|0xffeeddcc00000000, uint64(values[1])|0xaabbccdd00000000)
				if err != nil || !bytes.Equal(mem, want) {
					t.Fatalf("n=%d expression=%x values=%v err=%v", n, tc.expression, values, err)
				}
			}
			in.Close()
			c.Close()
		}
	}
}

func TestBulkCopyAppendPreservesLiveVectors(t *testing.T) {
	for _, live := range []int{1, 8, 12, 16, 20} {
		for _, n := range []uint32{15, 31, 47, 63} {
			body := []byte{1, 1, 0x7e} // one i64 local after the i32 parameter
			for i := 0; i < live; i++ {
				body = append(body, 0x41)
				body = append(body, wasmtest.SLEB32(int32(512+16*i))...)
				body = append(body, 0xfd, 0, 0, 0)
			}
			body = append(body, 0x41, 0, 0x41, 1, 0x41)
			body = append(body, wasmtest.SLEB32(int32(n))...)
			body = append(body, 0xfc, 10, 0, 0, 0x41)
			body = append(body, wasmtest.SLEB32(int32(n))...)
			body = append(body, 0x20, 0, 0x3a, 0, 0)
			for i := 0; i < live; i++ {
				body = append(body, 0xfd, 0x1d, 0)
				if i != 0 {
					body = append(body, 0x20, 1, 0x7c)
				}
				body = append(body, 0x21, 1)
			}
			body = append(body, 0x20, 1, 0x0b)
			module := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("shift", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
			c, err := NewRuntimeConfig().Compile(module)
			if err != nil {
				t.Fatal(err)
			}
			in, err := Instantiate(c)
			if err != nil {
				c.Close()
				t.Fatal(err)
			}
			mem := in.Memory().UnsafeBytes()
			for j := range mem {
				mem[j] = byte(j*37 + j/7)
			}
			want := bytes.Clone(mem)
			copy(want[:n], want[1:n+1])
			want[n] = 0xab
			var sum uint64
			for i := 0; i < live; i++ {
				sum += binary.LittleEndian.Uint64(mem[512+16*i:])
			}
			got, err := in.Invoke("shift", 0xffffffff000000ab)
			if err != nil || len(got) != 1 || got[0] != sum || !bytes.Equal(mem, want) {
				t.Fatalf("live=%d n=%d got=%v want=%d err=%v memoryEqual=%t", live, n, got, sum, err, bytes.Equal(mem, want))
			}
			in.Close()
			c.Close()
		}
	}
}
