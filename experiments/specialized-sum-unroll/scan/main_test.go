package main

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestExactAndInstructionBoundaries(t *testing.T) {
	// Noncanonical legal local indexes must still match. These are expression
	// bytes, not a module byte-pattern search; load alignment is an immediate.
	b := []byte{2, 0x40, 3, 0x40, 0x20, 0x80, 0, 0x45, 0x0d, 1,
		0x20, 2, 0x20, 1, 0x29, 3, 0, 0x7c, 0x21, 2,
		0x20, 1, 0x41, 8, 0x6a, 0x21, 1, 0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x0c, 0, 0x0b, 0x0b, 0x0b}
	ts, ls, err := decode(&wasm.Module{Memories: []wasm.MemType{{}}}, b)
	if err != nil || len(ls) != 1 {
		t.Fatalf("decode: %v %v", ls, err)
	}
	params := []wasm.ValType{wasm.I32, wasm.I32, wasm.I64}
	if !exact(ts, ls[0], params, nil) {
		t.Fatal("missed noncanonical exact match")
	}
	if len(screen(ts, ls[0], params, nil)) != 1 {
		t.Fatal("missed recurrence hypothesis")
	}
	withNops := append([]byte{2, 0x40, 3, 0x40, 1, 0x20, 0x80, 0, 1, 0x45, 1, 0x0d, 1}, b[10:]...)
	nt, nl, err := decode(&wasm.Module{Memories: []wasm.MemType{{}}}, withNops)
	if err != nil || !exact(nt, nl[0], params, nil) {
		t.Fatal("missed header nops", err)
	}
	withBodyNop := append([]byte{2, 0x40, 3, 0x40, 0x20, 0x80, 0, 0x45, 0x0d, 1, 1}, b[10:]...)
	nt, nl, err = decode(&wasm.Module{Memories: []wasm.MemType{{}}}, withBodyNop)
	if err != nil || exact(nt, nl[0], params, nil) || !bodyMatch(nt, nl[0], params, nil) {
		t.Fatal("body-nop distinction", err)
	}
	params[2] = wasm.F64
	if exact(ts, ls[0], params, nil) {
		t.Fatal("accepted wrong accumulator type")
	}
	params[2] = wasm.I64
	for i := range ts {
		if ts[i].Op == 0x41 && ts[i].Constant == 8 {
			ts[i].Constant = 16
		}
	}
	if exact(ts, ls[0], params, nil) {
		t.Fatal("accepted wrong stride")
	}
	if len(screen(ts, ls[0], params, nil)) != 1 {
		t.Fatal("lost near-match hypothesis")
	}
}

func TestCorpusPositiveControl(t *testing.T) {
	r := scan("../../../corpus/workloads/synthetic/memory.wasm")
	if r.Error != "" || r.Exact != 1 {
		t.Fatalf("positive control: %+v", r)
	}
	r = scan("../../../corpus/workloads/polybench/gemm.wasm")
	if r.Error != "" || r.Exact != 0 || r.Loops == 0 {
		t.Fatalf("floating-point control: %+v", r)
	}
}
