//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"math/bits"
	"testing"
)

func clearBitFixture(t testing.TB, steps []uint32) *wasm.Module {
	b := []byte{0, 0x03, 0x40}
	for i, step := range steps {
		b = append(b, 0x20, byte(i+1), 0x41)
		b = append(b, wasmtest.SLEB32(int32(step))...)
		b = append(b, 0x6a, 0x21, byte(i+1))
	}
	b = append(b, 0x20, 0, 0x41, 0x7f, 0x6a, 0x20, 0, 0x71, 0x22, 0, 0x0d, 0, 0x0b)
	params := []wasm.ValType{wasm.I32}
	for range steps {
		params = append(params, wasm.I32)
	}
	for i := range params {
		b = append(b, 0x41)
		b = append(b, wasmtest.SLEB32(int32(i*4))...)
		b = append(b, 0x20, byte(i), 0x36, 2, 0)
	}
	b = append(b, 0x41, 0, 0x0b)
	return modMem(t, 1, params, []wasm.ValType{wasm.I32}, b)
}
func clearBitExpected(args []uint64, steps []uint32) []byte {
	want := make([]byte, 65536)
	iterations := max(1, bits.OnesCount32(uint32(args[0])))
	for i, step := range steps {
		binary.LittleEndian.PutUint32(want[(i+1)*4:], uint32(args[i+1])+uint32(iterations)*step)
	}
	return want
}
func TestClearBitLoopExactState(t *testing.T) {
	old := clearBitLoopEnabled
	defer func() { clearBitLoopEnabled = old }()
	steps := []uint32{1, ^uint32(0), 4, 0x80000001}
	seed := uint32(0x1342abcd)
	for i := 0; i < 96; i++ {
		x := seed
		seed = seed*1664525 + 1013904223
		if i < 32 {
			x = uint32(1) << i
		}
		if i == 32 {
			x = 0
		}
		if i == 33 {
			x = ^uint32(0)
		}
		for n := 0; n <= 3; n++ {
			args := []uint64{uint64(x)}
			for j := 0; j < n; j++ {
				args = append(args, uint64(uint32(0xfffffff0)+uint32(j)*0x71234567))
			}
			m := clearBitFixture(t, steps[:n])
			want := clearBitExpected(args, steps[:n])
			for _, on := range []bool{false, true} {
				clearBitLoopEnabled = on
				for _, compact := range []bool{false, true} {
					got, err := runScalarUnrollMemory(t, m, CompileOptions{CompactNative: compact}, args...)
					if err != nil || !bytes.Equal(got, want) {
						t.Fatalf("x%x n%d on%v compact%v err%v equal%v", x, n, on, compact, err, bytes.Equal(got, want))
					}
				}
			}
		}
	}
}
func TestClearBitLoopAdmission(t *testing.T) {
	requireCompilerDiagnostics(t)
	old := clearBitLoopEnabled
	defer func() { clearBitLoopEnabled = old }()
	for _, on := range []bool{false, true} {
		clearBitLoopEnabled = on
		stats := &ModuleStats{}
		cm, err := CompileModuleWith(clearBitFixture(t, []uint32{1, 4}), CompileOptions{Stats: stats})
		if err != nil {
			t.Fatal(err)
		}
		cm.CodeImage.Close()
		if got := stats.Funcs[0].Peephole["clear-bit-loop-prefix"]; (got != 0) != on {
			t.Fatalf("on%v count%d", on, got)
		}
	}
}
func TestClearBitLoopCallerFPAndVectors(t *testing.T) {
	steps := []uint32{1, ^uint32(0), 4, 0x80000001}
	args := []uint64{0xfedc1234, 0xfffffff0, 7, 0x80000000, 0xdeadbeef}
	callee := clearBitFixture(t, steps)
	want := clearBitExpected(args, steps)
	b := []byte{2, 12, 0x7c, 4, 0x7b}
	for i := byte(0); i < 12; i++ {
		v := uint64(0x7ff0000012345678) + uint64(i)*0x100000001
		b = append(b, 0x44)
		b = binary.LittleEndian.AppendUint64(b, v)
		b = append(b, 0x21, i)
		binary.LittleEndian.PutUint64(want[32768+int(i)*8:], v)
	}
	for i := byte(0); i < 4; i++ {
		b = append(b, 0xfd, 0x0c)
		for lane := byte(0); lane < 16; lane++ {
			b = append(b, i*32+lane+1)
			want[33024+int(i)*16+int(lane)] = i*32 + lane + 1
		}
		b = append(b, 0x21, i+12)
	}
	for _, v := range args {
		b = append(b, 0x41)
		b = append(b, wasmtest.SLEB32(int32(v))...)
	}
	b = append(b, 0x10, 1, 0x1a)
	for i := byte(0); i < 12; i++ {
		b = append(b, 0x41)
		b = append(b, wasmtest.SLEB32(32768+int32(i)*8)...)
		b = append(b, 0x20, i, 0x39, 0, 0)
	}
	for i := byte(0); i < 4; i++ {
		b = append(b, 0x41)
		b = append(b, wasmtest.SLEB32(33024+int32(i)*16)...)
		b = append(b, 0x20, i+12, 0xfd, 0x0b, 0, 0)
	}
	b = append(b, 0x41, 0, 0x0b)
	m := modMem(t, 1, nil, []wasm.ValType{wasm.I32}, b)
	m.Types = append(m.Types, callee.Types[0])
	m.FuncTypes = append(m.FuncTypes, wasm.TypeIdx{Index: 1})
	m.Code = append(m.Code, callee.Code[0])
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	for _, compact := range []bool{false, true} {
		for _, stackReg := range []bool{false, true} {
			stats := &ModuleStats{}
			opts := CompileOptions{CompactNative: compact, Optimizations: map[string]bool{"inline": false, "stack-reg": stackReg}}
			if diagnosticsEnabled {
				opts.Stats = stats
			}
			got, err := runScalarUnrollMemory(t, m, opts)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("compact%v stackReg%v err%v equal%v", compact, stackReg, err, bytes.Equal(got, want))
			}
			if diagnosticsEnabled && stats.Funcs[1].Peephole["clear-bit-loop-prefix"] != 1 {
				t.Fatal("callee prefix missing")
			}
		}
	}
}

func TestClearBitProbeBudgetEffectsAndAllocations(t *testing.T) {
	m := clearBitFixture(t, []uint32{1, 4})
	body := m.Code[0].BodyBytes
	at := bytes.Index(body, []byte{0x03, 0x40})
	if at < 0 {
		t.Fatal("missing loop")
	}
	code := body[at+2:]
	probe := fn{nLocals: 3, localType: []machineType{mtI32, mtI32, mtI32}}
	var admitted bool
	allocs := testing.AllocsPerRun(100, func() {
		probe.integerResidualWork = 0
		r := wasm.ReaderFrom(code)
		_, admitted = probe.inspectClearBitLoop(&r)
		if r.Offset() != 0 {
			panic("candidate advanced original reader")
		}
	})
	if !admitted || allocs != 0 {
		t.Fatalf("admitted%v allocations%v", admitted, allocs)
	}
	for _, prefix := range [][]byte{{0x10, 0}, {0x28, 2, 0}, {0x36, 2, 0}, {0x02, 0x40}, {0x00}} {
		probe.integerResidualWork = 0
		r := wasm.ReaderFrom(append(append([]byte{}, prefix...), code...))
		if _, ok := probe.inspectClearBitLoop(&r); ok || r.Offset() != 0 {
			t.Fatalf("unsafe prefix%x admitted", prefix)
		}
	}
	probe.integerResidualWork = integerResidualWorkLimit - 3
	r := wasm.ReaderFrom(code)
	if _, ok := probe.inspectClearBitLoop(&r); ok || probe.integerResidualWork != integerResidualWorkLimit || r.Offset() != 0 {
		t.Fatal("budget rejection changed original reader")
	}
}
