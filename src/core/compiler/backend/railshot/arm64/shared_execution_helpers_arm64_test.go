// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func runLinearSpanMemory(t *testing.T, m *wasm.Module, opts CompileOptions, initial []byte, argsIn ...uint64) ([]byte, error) {
	t.Helper()
	return runLinearSpanMemorySize(t, m, opts, 65536, initial, argsIn...)
}

func runLinearSpanMemorySize(t *testing.T, m *wasm.Module, opts CompileOptions, memoryBytes int, initial []byte, argsIn ...uint64) ([]byte, error) {
	t.Helper()
	cm, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer cm.CodeImage.Close()
	eng, err := rt.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := rt.NewJobMemory(memoryBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	copy(jm.CurrentBytes(), initial)
	ar, err := rt.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()
	code, entry, err := rt.MapCode(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Unmap(code)
	args, result, trap := ar.Alloc(max(32, len(argsIn)*8)), ar.Alloc(16), ar.Alloc(rt.TrapBufferBytes)
	for i, v := range argsIn {
		binary.LittleEndian.PutUint64(args[i*8:], v)
	}
	err = eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, result)
	return append([]byte{}, jm.CurrentBytes()...), err
}

func runScalarUnrollMemory(t *testing.T, m *wasm.Module, opts CompileOptions, argsIn ...uint64) ([]byte, error) {
	t.Helper()
	cm, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer cm.CodeImage.Close()
	eng, err := rt.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := rt.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	clear(jm.CurrentBytes())
	ar, err := rt.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()
	code, entry, err := rt.MapCode(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Unmap(code)
	args, results, trap := ar.Alloc(32), ar.Alloc(16), ar.Alloc(rt.TrapBufferBytes)
	for i, arg := range argsIn {
		binary.LittleEndian.PutUint64(args[i*8:], arg)
	}
	err = eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results)
	return append([]byte(nil), jm.CurrentBytes()...), err
}

func nativeConstantTargets(b []byte) []uint64 {
	bits := make([]uint64, ((len(b)/4)+63)/64)
	for at := 0; at < len(b); at += 4 {
		if to, ok := branchTarget(at, rdWord(b, at)); ok {
			branchTargetAdd(bits, to, len(b))
		}
	}
	return bits
}

func brTableComputedLabelsArm64(t testing.TB, labels []uint32, def uint32) *wasm.Module {
	t.Helper()
	params := []wasm.ValType{wasm.I32, wasm.I32}
	body := []byte{0x00} // no locals
	maxLabel := def
	for _, lbl := range labels {
		if lbl > maxLabel {
			maxLabel = lbl
		}
	}
	blockN := int(maxLabel) + 1
	for i := 0; i < blockN; i++ {
		body = append(body, 0x02, 0x40) // block (void)
	}
	body = append(body, 0x20, 0x00, 0x20, 0x01, 0x6e) // local.get 0; local.get 1; i32.div_u
	body = append(body, 0x0e)
	body = append(body, wasmtest.ULEB(uint32(len(labels)))...)
	for _, lbl := range labels {
		body = append(body, wasmtest.ULEB(lbl)...)
	}
	body = append(body, wasmtest.ULEB(def)...)
	for i := 0; i < blockN; i++ {
		body = append(body, 0x0b) // end block i
		body = append(body, 0x41) // i32.const
		body = append(body, wasmtest.SLEB32(int32(1000+i))...)
		body = append(body, 0x0f) // return
	}
	body = append(body, 0x0b) // end func

	entry := append(wasmtest.ULEB(uint32(len(body))), body...)
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(entry)),
	)
	m, err := wasm.DecodeModule(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}
