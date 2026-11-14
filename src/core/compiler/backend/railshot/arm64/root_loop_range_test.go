//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func rootRangeFixture(t testing.TB, stride, limit int32, priorWrite, nested, extraWrite bool) *wasm.Module {
	b := []byte{1, 1, 0x7f}
	if priorWrite {
		b = append(b, 0x20, 0, 0x21, 1)
	}
	if nested {
		b = append(b, 0x02, 0x40)
	}
	b = append(b, 0x03, 0x40, 0x20, 1, 0x41)
	b = append(b, wasmtest.SLEB32(65516)...)
	b = append(b, 0x6a, 0x20, 1, 0x41, 17, 0x6a, 0x3a, 0, 0)
	if extraWrite {
		b = append(b, 0x20, 1, 0x41, 32, 0x6a, 0x21, 1)
	}
	b = append(b, 0x20, 1, 0x41)
	b = append(b, wasmtest.SLEB32(stride)...)
	b = append(b, 0x6a, 0x22, 1, 0x41)
	b = append(b, wasmtest.SLEB32(limit)...)
	b = append(b, 0x47, 0x0d, 0, 0x0b)
	if nested {
		b = append(b, 0x0b)
	}
	// Reuse the counter in a later root loop. Its old upper bound cannot apply.
	b = append(b, 0x20, 0, 0x21, 1, 0x03, 0x40, 0x20, 1, 0x41)
	b = append(b, wasmtest.SLEB32(65516)...)
	b = append(b, 0x6a, 0x41)
	b = append(b, wasmtest.SLEB32(99)...)
	b = append(b, 0x3a, 0, 0, 0x0b, 0x41, 0, 0x0b)
	return modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, b)
}

func TestRootLoopRangeMemoryAndLifetime(t *testing.T) {
	for _, shape := range []struct {
		stride, limit        int32
		prior, nested, extra bool
	}{{4, 4, false, false, false}, {4, 16, false, false, false}, {4, 24, false, false, false}, {6, 16, false, false, false}, {4, 16, true, false, false}, {4, 16, false, true, false}, {4, 16, false, false, true}, {4, 0, false, false, false}} {
		m := rootRangeFixture(t, shape.stride, shape.limit, shape.prior, shape.nested, shape.extra)
		for _, enabled := range []bool{false, true} {
			for _, arg := range []uint64{0, 1, 4, 19, 20, 0xfffffffc, 0x8877665500000001} {
				want := make([]byte, 65536)
				counter := uint32(0)
				if shape.prior {
					counter = uint32(arg)
				}
				traps := false
				ended := false
				for steps := 0; steps < 32; steps++ {
					address := counter + 65516
					if uint64(address) >= uint64(len(want)) {
						traps = true
						ended = true
						break
					}
					want[address] = byte(counter + 17)
					if shape.extra {
						counter += 32
					}
					counter += uint32(shape.stride)
					if counter == uint32(shape.limit) {
						ended = true
						break
					}
				}
				if !ended {
					t.Fatal("test oracle did not terminate")
				}
				if !traps {
					address := uint32(arg) + 65516
					if uint64(address) >= uint64(len(want)) {
						traps = true
					} else {
						want[address] = 99
					}
				}
				var stats ModuleStats
				opts := CompileOptions{Optimizations: map[string]bool{"memory-min-bounds": enabled}}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				got, err := runAlignedStoreMemory(t, m, opts, arg)
				if (err != nil) != traps || !bytes.Equal(got, want) {
					t.Fatalf("shape=%+v on=%v arg=%x trap=%v want=%v memoryEqual=%v", shape, enabled, arg, err, traps, bytes.Equal(got, want))
				}
				if diagnosticsEnabled {
					wantHit := enabled && !shape.prior && !shape.nested && !shape.extra && shape.stride > 0 && shape.limit > 0 && shape.limit%shape.stride == 0
					if hits := stats.Funcs[0].Peephole["root-loop-address-range"]; (hits != 0) != wantHit {
						t.Fatalf("shape=%+v on=%v hits=%d want=%v", shape, enabled, hits, wantHit)
					}
				}
			}
		}
	}
}

func TestRootLoopRangeLatchProofBoundaries(t *testing.T) {
	f := fn{nParams: 1, nLocals: 3, localType: []machineType{mtI32, mtI32, mtI32}, classifier: wasm.NewModuleInstructionClassifier(&wasm.Module{}, true)}
	tail := func(index uint32, stride, limit int32) []byte {
		b := []byte{0x20}
		b = append(b, wasmtest.ULEB(index)...)
		b = append(b, 0x41)
		b = append(b, wasmtest.SLEB32(stride)...)
		b = append(b, 0x6a, 0x22)
		b = append(b, wasmtest.ULEB(index)...)
		b = append(b, 0x41)
		b = append(b, wasmtest.SLEB32(limit)...)
		return append(b, 0x47, 0x0d, 0, 0x0b)
	}
	for _, tc := range []struct {
		name          string
		stride, limit int32
		index         uint32
		zero          uint64
		prefix        []byte
		want          bool
	}{
		{"positive", 4, 16, 1, 2, nil, true},
		{"maximum-positive", 1, 0x7fffffff, 1, 2, nil, true},
		{"one-iteration", 16, 16, 1, 2, nil, true},
		{"zero-stride", 0, 16, 1, 2, nil, false},
		{"negative-stride", -4, 16, 1, 2, nil, false},
		{"zero-limit", 4, 0, 1, 2, nil, false},
		{"negative-limit", 4, -1, 1, 2, nil, false},
		{"nondivisible", 6, 16, 1, 2, nil, false},
		{"overshoot", 8, 4, 1, 2, nil, false},
		{"unknown-entry", 4, 16, 1, 0, nil, false},
		{"parameter", 4, 16, 0, 1, nil, false},
		{"extra-write", 4, 16, 1, 2, []byte{0x41, 0, 0x21, 1}, false},
		{"extra-backedge", 4, 16, 1, 2, []byte{0x41, 0, 0x0d, 0}, false},
		{"extra-control", 4, 16, 1, 2, []byte{0x02, 0x40, 0x0b}, false},
	} {
		body := append(append([]byte(nil), tc.prefix...), tail(tc.index, tc.stride, tc.limit)...)
		local, maximum := f.proveRootLoopRange(body, tc.zero)
		if (local != 0) != tc.want || tc.want && (local != uint8(tc.index+1) || maximum != uint32(tc.limit)) {
			t.Fatalf("%s: local=%d maximum=%d want=%v", tc.name, local, maximum, tc.want)
		}
	}
	body := tail(1, 4, 16)
	for _, bad := range [][]byte{body[:len(body)-1], append(append([]byte(nil), body...), 0x01), append(bytes.Repeat([]byte{0x01}, 16<<10), body...)} {
		if local, _ := f.proveRootLoopRange(bad, 2); local != 0 {
			t.Fatal("truncated, trailing or oversized body accepted")
		}
	}
}

func TestRootLoopRangeMaximumRequiresOpenRootFrame(t *testing.T) {
	f := fn{rootLoopRangeLocal: 2, rootLoopRangeMaximum: 16}
	e := elem{st: storage{kind: stLocalRef, typ: mtI32, idx: 1}}
	e.setElemKind(ekValue)
	f.ctrl = []ctrlFrame{{kind: cfFunc}, {kind: cfLoop}}
	if max, ok := f.localAddressMaximum(&e); !ok || max != 16 {
		t.Fatal("missing open-loop range")
	}
	f.ctrl = f.ctrl[:1]
	if _, ok := f.localAddressMaximum(&e); ok {
		t.Fatal("loop range survived exit")
	}
	f.ctrl = append(f.ctrl, ctrlFrame{kind: cfBlock}, ctrlFrame{kind: cfLoop})
	if _, ok := f.localAddressMaximum(&e); ok {
		t.Fatal("loop range escaped into unrelated nested loop")
	}
}
