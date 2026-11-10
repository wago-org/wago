//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func zeroTimeoutWaitModule(wide, shared bool, offset uint32) []byte {
	typ, sub, align := wasm.I32, byte(1), byte(2)
	if wide {
		typ, sub, align = wasm.I64, 2, 3
	}
	limits := byte(1)
	if shared {
		limits = 3
	}
	body := []byte{0x20, 0, 0x20, 1, 0x42, 0, 0xfe, sub, align}
	body = append(body, wasmtest.ULEB(offset)...)
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, typ}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(5, wasmtest.Vec([]byte{limits, 1, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("wait", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestThreadsZeroTimeoutWaitNativeMatchesHelper(t *testing.T) {
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit)
	for _, wide := range []bool{false, true} {
		for _, shared := range []bool{false, true} {
			for _, offset := range []uint32{0, 1, 4, 0xfffffff8} {
				data := zeroTimeoutWaitModule(wide, shared, offset)
				slow, err := cfg.WithOptimization("atomic-wait-zero-timeout", false).Compile(data)
				if err != nil {
					t.Fatal(err)
				}
				fast, err := cfg.WithOptimization("atomic-wait-zero-timeout", true).Compile(data)
				if err != nil {
					slow.Close()
					t.Fatal(err)
				}
				if shared && bytes.Equal(slow.code, fast.code) {
					t.Fatal("zero-timeout wait did not select native lowering")
				}
				if !shared && !bytes.Equal(slow.code, fast.code) {
					t.Fatal("unshared-memory traps changed code path")
				}
				a, err := Instantiate(slow, InstantiateOptions{})
				if err != nil {
					t.Fatal(err)
				}
				b, err := Instantiate(fast, InstantiateOptions{})
				if err != nil {
					t.Fatal(err)
				}
				for _, address := range []uint64{0, 1, 4, 8, 65528, 65532, 65536, 65537, 1 << 32, 1<<32 | 8, ^uint64(0)} {
					for _, expected := range []uint64{0, 1, 0xffffffff, 0x8000000000000000, 0x80000000ffffffff, ^uint64(0)} {
						for _, in := range []*Instance{a, b} {
							// Every byte has a nonzero pattern; valid aligned comparisons exercise
							// both words and the i32 operand's upper-bit truncation.
							mem := in.mem()
							for i := 0; i+8 <= len(mem); i += 8 {
								binary.LittleEndian.PutUint64(mem[i:], 0x80000000ffffffff)
							}
						}
						av, ae := a.Invoke("wait", address, expected)
						bv, be := b.Invoke("wait", address, expected)
						if (ae == nil) != (be == nil) {
							t.Fatalf("wide=%t shared=%t off=%x addr=%x: helper=%v native=%v", wide, shared, offset, address, ae, be)
						}
						if ae != nil {
							at, aok := ae.(*TrapError)
							bt, bok := be.(*TrapError)
							if !aok || !bok || at.Code != bt.Code {
								t.Fatalf("trap precedence: %v vs %v", ae, be)
							}
						} else if len(av) != 1 || len(bv) != 1 || av[0] != bv[0] {
							t.Fatalf("wait results: %v vs %v", av, bv)
						}
					}
				}
				a.Close()
				b.Close()
				slow.Close()
				fast.Close()
			}
		}
	}
}
