//go:build (linux || darwin) && arm64 && !tinygo

package wago

import (
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

type tailSlotFixture struct {
	slot                                         uint32
	wrongSig, earlierDivision, mutable, exported bool
}

func tailSlotModule(o tailSlotFixture) []byte {
	types := [][]byte{wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, nil)}
	body := []byte{0x20, 0, 0x45, 0x04, 0x7f, 0x20, 1, 0x05, 0x20, 0}
	if o.earlierDivision {
		body = append(body, 0x20, 0, 0x20, 0, 0x6b, 0x6d)
	} else {
		body = append(body, 0x41, 1, 0x6b)
	}
	typ := byte(0)
	if o.wrongSig {
		typ = 1
	} else {
		body = append(body, 0x20, 1, 0x41, 17, 0x6a)
	}
	body = append(body, 0x41)
	body = append(body, wasmtest.SLEB32(int32(o.slot))...)
	body = append(body, 0x13, typ, 0, 0x0b, 0x0b)
	funcs := [][]byte{wasmtest.ULEB(0)}
	codes := [][]byte{wasmtest.Code(body)}
	exports := [][]byte{wasmtest.ExportEntry("run", 0, 0)}
	if o.mutable {
		funcs = append(funcs, wasmtest.ULEB(2), wasmtest.ULEB(2))
		codes = append(codes, wasmtest.Code([]byte{0x41, 2, 0xd0, 0x70, 0x26, 0, 0x0b}), wasmtest.Code([]byte{0x41, 2, 0xd2, 0, 0x26, 0, 0x0b}))
		exports = append(exports, wasmtest.ExportEntry("clear", 0, 1), wasmtest.ExportEntry("restore", 0, 2))
	}
	if o.exported {
		exports = append(exports, wasmtest.ExportEntry("t", byte(wasm.ExternTable), 0))
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, wasmtest.Vec(funcs...)), wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0, 4})), wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(9, wasmtest.Vec([]byte{0, 0x41, 2, 0x0b, 1, 0})), wasmtest.Section(10, wasmtest.Vec(codes...)))
}

func TestTailConstantPrivateTableExecutionAndTraps(t *testing.T) {
	for _, tc := range []struct {
		name string
		o    tailSlotFixture
		trap TrapCode
	}{
		{name: "private", o: tailSlotFixture{slot: 2}},
		{name: "exported", o: tailSlotFixture{slot: 2, exported: true}},
		{name: "null", o: tailSlotFixture{slot: 1}, trap: TrapIndirectOutOfBounds},
		{name: "OOB", o: tailSlotFixture{slot: 4}, trap: TrapIndirectOutOfBounds},
		{name: "unsigned-index", o: tailSlotFixture{slot: 0xffffffff}, trap: TrapIndirectOutOfBounds},
		{name: "wrong-signature", o: tailSlotFixture{slot: 2, wrongSig: true}, trap: TrapIndirectWrongSig},
		{name: "division-before-known-target", o: tailSlotFixture{slot: 2, earlierDivision: true}, trap: TrapDivZero},
		{name: "division-before-null", o: tailSlotFixture{slot: 1, earlierDivision: true}, trap: TrapDivZero},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, enabled := range []bool{false, true} {
				c, err := NewRuntimeConfig().WithOptimization("tail-self-loop", enabled).Compile(tailSlotModule(tc.o))
				if err != nil {
					t.Fatal(err)
				}
				if c.directPreparedBoundedAt(0) || c.boundedNativeScalarLeaf() {
					t.Fatal("indirect self tails acquired bounded scheduler admission")
				}
				in, err := Instantiate(c)
				if err != nil {
					c.Close()
					t.Fatal(err)
				}
				fn, err := in.WasmFunc("run")
				if err != nil {
					t.Fatal(err)
				}
				for repeat := 0; repeat < 3; repeat++ {
					for _, n := range []uint64{0, 1, 2, 31, 4096, 100000} {
						a := uint64(0xfffffff0)
						got, err := fn.Invoke(n|0xdeadbeef00000000, a|0x1234567800000000)
						if n > 0 && tc.trap != TrapNone {
							var trap *TrapError
							if !errors.As(err, &trap) || trap.Code != tc.trap {
								t.Fatalf("enabled=%t n=%d err=%v want trap=%v", enabled, n, err, tc.trap)
							}
						} else {
							want := uint64(uint32(a + n*17))
							if err != nil || len(got) != 1 || got[0] != want {
								t.Fatalf("enabled=%t n=%d got=%v err=%v want=%d", enabled, n, got, err, want)
							}
						}
					}
				}
				in.Close()
				c.Close()
			}
		})
	}
}

func TestTailConstantTableMutationRemainsObservable(t *testing.T) {
	for _, exported := range []bool{false, true} {
		c, err := NewRuntimeConfig().Compile(tailSlotModule(tailSlotFixture{slot: 2, mutable: true, exported: exported}))
		if err != nil {
			t.Fatal(err)
		}
		in, err := Instantiate(c)
		if err != nil {
			c.Close()
			t.Fatal(err)
		}
		fn, err := in.WasmFunc("run")
		if err != nil {
			t.Fatal(err)
		}
		for repeat := 0; repeat < 3; repeat++ {
			got, err := fn.Invoke(31, 7)
			if err != nil || len(got) != 1 || got[0] != 7+31*17 {
				t.Fatalf("before mutation: %v,%v", got, err)
			}
			if _, err = in.Invoke("clear"); err != nil {
				t.Fatal(err)
			}
			_, err = fn.Invoke(31, 7)
			var trap *TrapError
			if !errors.As(err, &trap) || trap.Code != TrapIndirectOutOfBounds {
				t.Fatalf("after clear: %v", err)
			}
			if _, err = in.Invoke("restore"); err != nil {
				t.Fatal(err)
			}
		}
		in.Close()
		c.Close()
	}
}
