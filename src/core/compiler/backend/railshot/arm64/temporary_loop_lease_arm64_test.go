//go:build arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func temporaryLoopLeaseFixture(t testing.TB) *wasm.Module {
	// Seven heavily used anchors compete for permanent GP pins. The target loop
	// has four other mutable locals, a captured old accumulator, and an early
	// exit before updating the counter/pointer. Calls require canonical homes.
	b := []byte{2, 12, 0x7f, 1, 0x7e, 0x42, 0, 0x10, 1, 0x1a, 0x41, 2, 0x21, 11, 0x03, 0x40}
	for local := byte(4); local <= 10; local++ {
		for j := 0; j < 8; j++ {
			b = append(b, 0x20, local, 0x41, local, 0x6a, 0x21, local)
		}
	}
	b = append(b, 0x20, 11, 0x41, 1, 0x6b, 0x22, 11, 0x0d, 0, 0x0b, 0x42, 0, 0x10, 1, 0x1a)
	b = append(b,
		0x41, 48, 0x41, 42, 0x3a, 0, 0,
		0x20, 3, 0x21, 12,
		0x20, 0, 0x41, 1, 0x6b, 0x21, 13,
		0x41, 55, 0x21, 14, 0x41)
	b = append(b, wasmtest.SLEB32(77)...)
	b = append(b, 0x21, 15, 0x42, 0, 0x21, 16,
		0x02, 0x40, 0x20, 0, 0x45, 0x0d, 0, 0x03, 0x40,
		0x20, 12, 0x41, 1, 0x6a, 0x21, 14,
		0x20, 16, 0x20, 16, 0x42, 3, 0x7e, 0x20, 12, 0x31, 0, 0, 0x7c, 0x42, 1, 0x7c, 0x22, 16, 0x85, 0x21, 16,
		0x20, 16, 0x20, 1, 0x56, 0x0d, 1,
		0x20, 13, 0x41, 0, 0x47, 0x21, 15,
		0x20, 13, 0x41, 1, 0x6b, 0x21, 13,
		0x20, 14, 0x21, 12, 0x20, 15, 0x0d, 0, 0x0b, 0x0b,
		0x20, 16, 0x10, 1, 0x21, 16)
	for i := byte(0); i < 4; i++ {
		b = append(b, 0x41, i*4, 0x20, 12+i, 0x36, 0, 0)
	}
	b = append(b, 0x41, 16, 0x20, 16, 0x37, 0, 0,
		0x41, 24, 0x20, 2, 0xbd, 0x37, 0, 0, 0x41, 40, 0x20, 4)
	for i := byte(5); i <= 10; i++ {
		b = append(b, 0x20, i, 0x6a)
	}
	b = append(b, 0x36, 0, 0, 0x41, 0, 0x0b)
	helper := []byte{0, 0x20, 0, 0x0b}
	raw := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64, wasm.F64, wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(b))), b...), append(wasmtest.ULEB(uint32(len(helper))), helper...))),
	)
	m, err := wasm.DecodeModule(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestTemporaryLoopLeaseHomesAndEarlyExit(t *testing.T) {
	old := temporaryLoopLeaseEnabled
	defer func() { temporaryLoopLeaseEnabled = old }()
	m := temporaryLoopLeaseFixture(t)
	initial := make([]byte, 65536)
	for i := range initial {
		initial[i] = byte(i*17 + 9)
	}
	cases := []struct{ n, base, threshold, marker uint64 }{
		{0, 0xffffffff, 0, 0x8000000000000000},
		{1, 256, ^uint64(0), 0x7ff8000000000123},
		{2, 256, ^uint64(0), 0x3ff0000000000000},
		{7, 256, ^uint64(0), 0x8000000000000000},
		{31, 256, ^uint64(0), 0x7ff8000000000123},
		{63, 256, ^uint64(0), 0x3ff0000000000000},
		{127, 256, ^uint64(0), 0x8000000000000000},
		{7, 256, 0, 0x7ff8000000000123},
		{7, 256, 5, 0x3ff0000000000000},
		{7, 256, 1000, 0x8000000000000000},
		{7, 48, ^uint64(0), 0x7ff8000000000123},
		{1, 65535, ^uint64(0), 0x3ff0000000000000},
		{2, 65535, ^uint64(0), 0x8000000000000000},
		{7, 65536, 0, 0x7ff8000000000123},
		{7, 0xffffffff, ^uint64(0), 0x3ff0000000000000},
		{1<<32 | 7, 1<<32 | 256, ^uint64(0), 0x8000000000000000},
	}
	for _, on := range []bool{false, true} {
		temporaryLoopLeaseEnabled = on
		for _, compact := range []bool{false, true} {
			for _, cold := range []bool{false, true} {
				for _, c := range cases {
					want := append([]byte{}, initial...)
					want[48] = 42
					ptr, counter, next, flag := uint32(c.base), uint32(c.n)-1, uint32(55), uint32(77)
					acc, traps := uint64(0), false
					if uint32(c.n) != 0 {
						for {
							next = ptr + 1
							if uint64(ptr) >= uint64(len(want)) {
								traps = true
								break
							}
							acc ^= acc*3 + uint64(want[ptr]) + 1
							if acc > c.threshold {
								break
							}
							flag = 0
							if counter != 0 {
								flag = 1
							}
							counter--
							ptr = next
							if flag == 0 {
								break
							}
						}
					}
					if !traps {
						for i, v := range []uint32{ptr, counter, next, flag} {
							binary.LittleEndian.PutUint32(want[i*4:], v)
						}
						binary.LittleEndian.PutUint64(want[16:], acc)
						binary.LittleEndian.PutUint64(want[24:], c.marker)
						binary.LittleEndian.PutUint32(want[40:], 784)
					}
					opts := CompileOptions{CompactNative: compact, Optimizations: map[string]bool{"inline": false, "callfree-loop-cold-exit": cold}}
					got, err := runLinearSpanMemory(t, m, opts, initial, c.n, c.threshold, c.marker, c.base)
					if (err != nil) != traps || !bytes.Equal(got, want) {
						first := -1
						for i := range want {
							if got[i] != want[i] {
								first = i
								break
							}
						}
						t.Fatalf("on%v compact%v cold%v case%+v err%v trap%v firstDifference%d got%x want%x", on, compact, cold, c, err, traps, first, got[:56], want[:56])
					}
				}
			}
		}
	}
}

func TestTemporaryLoopLeaseAdmission(t *testing.T) {
	requireCompilerDiagnostics(t)
	old := temporaryLoopLeaseEnabled
	defer func() { temporaryLoopLeaseEnabled = old }()
	for _, on := range []bool{false, true} {
		temporaryLoopLeaseEnabled = on
		stats := &ModuleStats{}
		cm, err := CompileModuleWith(temporaryLoopLeaseFixture(t), CompileOptions{Stats: stats, Optimizations: map[string]bool{"inline": false}})
		if err != nil {
			t.Fatal(err)
		}
		cm.CodeImage.Close()
		if hit := stats.Funcs[0].Peephole["temporary-loop-local-lease"]; (hit != 0) != on {
			t.Fatalf("on%v hit%d", on, hit)
		}
	}
}

func TestTemporaryLoopLeaseBoundedProbe(t *testing.T) {
	old := temporaryLoopLeaseEnabled
	temporaryLoopLeaseEnabled = true
	defer func() { temporaryLoopLeaseEnabled = old }()
	f := fn{s: newStack(), nLocals: 4, usesCalls: true, locals: make([]localDef, 4), localType: []machineType{mtI32, mtI32, mtI64, mtI32}, classifier: wasm.NewModuleInstructionClassifier(temporaryLoopLeaseFixture(t), true), ctrl: []ctrlFrame{{kind: cfBlock}}}
	for i := range f.locals {
		f.locals[i].reg = regNone
	}
	f.locals[0].reg = X19
	body := []byte{0x20, 0, 0x1a, 0x20, 0, 0x0d, 1, 0x20, 1, 0x20, 1, 0x6a, 0x21, 1, 0x20, 1, 0x0d, 0, 0x0b}
	if allocs := testing.AllocsPerRun(100, func() {
		f.loopLocalLeaseWork = 0
		r := wasm.ReaderFrom(body)
		plan, ok := f.planLoopLocalLease(&r)
		if !ok || !plan.temporary || plan.count != 1 || plan.local[0] != 1 || r.Offset() != 0 {
			panic("invalid temporary probe")
		}
	}); allocs != 0 {
		t.Fatalf("probe allocations%v", allocs)
	}
	for _, prefix := range [][]byte{{0x02, 0x40}, {0x03, 0x40}, {0x04, 0x40}, {0x0c, 1}, {0x0d, 2}, {0x10, 1}, {0x11, 0, 0}, {0x25, 0}, {0xfc, 10, 0, 0}, {0xfd, 0}, {0xfe, 0}, {0xfb, 0}, bytes.Repeat([]byte{1}, 129)} {
		f.loopLocalLeaseWork = 0
		r := wasm.ReaderFrom(append(append([]byte{}, prefix...), body...))
		if _, ok := f.planLoopLocalLease(&r); ok || r.Offset() != 0 {
			t.Fatalf("admitted barrier%x", prefix)
		}
	}
	f.loopLocalLeaseWork = loopLocalLeaseWorkLimit - 1
	r := wasm.ReaderFrom(body)
	if _, ok := f.planLoopLocalLease(&r); ok || f.loopLocalLeaseWork != loopLocalLeaseWorkLimit || r.Offset() != 0 {
		t.Fatal("budget not enforced")
	}
	f.loopLocalLeaseWork = 0
	f.ctrl[0].branchN = 1
	r = wasm.ReaderFrom(body)
	if _, ok := f.planLoopLocalLease(&r); ok {
		t.Fatal("admitted value-carrying exit")
	}
}
