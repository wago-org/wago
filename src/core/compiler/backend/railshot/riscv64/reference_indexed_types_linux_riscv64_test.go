//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestReferenceIndexedTypesRV64FullCarrier(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, form := range []byte{0x63, 0x64} {
			t.Run(fmt.Sprintf("vector%t-form%x", vector, form), func(t *testing.T) {
				typ, pad := wasm.I64, uint32(253)
				if vector {
					typ, pad = wasm.V128, 125
				}
				refType := wasm.RefVal(wasm.Ref(form == 0x63, wasm.IndexedHeap(wasm.TypeIdx{Index: 64}), false))
				body := []byte{2, form, 0xc0, 0, 0xd2, 0, 0xd2, 1, 0x20, 1, 0x1c, 1, form, 0xc0, 0, 0x22}
				body = append(body, wasmtest.ULEB(pad+2)...)
				// Observe the reloaded local's full bits, not local.tee's stack
				// copy; either a truncated store or load must change the oracle.
				body = append(body, 0x1a, 0x20)
				body = append(body, wasmtest.ULEB(pad+2)...)
				body = append(body, 0x0c, 0, 0x0b, 0x24, 0, 0x20)
				body = append(body, wasmtest.ULEB(pad+2)...)
				body = append(body, 0xd1, 4, 0x40, 0, 0x0b, 0x20, 0, 0x0b)
				initial := append([]byte{0xd2, 0, 0x21}, wasmtest.ULEB(pad+2)...)
				if form == 0x63 {
					// Reused frames contain the preceding call's nonzero high
					// bits. Default initialization must clear the entire carrier.
					check := append([]byte{0x20}, wasmtest.ULEB(pad+2)...)
					check = append(check, 0xd1, 0x45, 4, 0x40, 0, 0x0b)
					initial = append(check, initial...)
				}
				body = append(initial, body...)
				typeBytes := make([][]byte, 65)
				for i := range typeBytes {
					typeBytes[i] = wasmtest.FuncType(nil, nil)
				}
				typeBytes[2] = wasmtest.FuncType([]wasm.ValType{typ, wasm.I32}, []wasm.ValType{typ})
				m, err := wasm.DecodeModule(wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(typeBytes...)), wasmtest.Section(3, []byte{3, 0, 1, 2}), wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.FuncRef, true, []byte{0xd0, 0x70, 0x0b}))), wasmtest.Section(9, []byte{1, 3, 0, 2, 0, 1}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}), wasmtest.Code([]byte{0x0b}), wasmtest.Code(body)))))
				if err != nil {
					t.Fatal(err)
				}
				fn := &m.Code[2]
				fn.Locals.Runs = []wasm.LocalRun{{Count: pad, Type: typ}, {Count: 1, Type: refType}}
				if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
					t.Fatal("typed local/block/select fixture", err)
				}
				types := make([]*wasm.CompType, len(m.Types))
				specs := make([]functionSpec, len(types))
				for i := range types {
					types[i] = &m.Types[i].SubTypes[0].Comp
					specs[i].signature = types[i]
				}
				ft := types[2]
				mode := emissionMode{production: true, foreignTableCalls: true, referenceCalls: true, localMutableReferences: true}
				var code []byte
				var info FunctionInfo
				if vector {
					code, info, _, err = compileSWARPairsWithGlobals(fn, ft, nil, types, types[:3], []wasm.GlobalType{{Type: wasm.FuncRef, Mutable: true}}, mode)
				} else {
					state := moduleState{production: true, importReferences: true, referenceCalls: true, localMutableReferences: true, types: specs, functions: specs[:3], globals: []globalSpec{{typ: wasm.FuncRef, wide: true, mutable: true}}}
					summary, e := walkInteger(fn, ft, &state, nil)
					if e != nil {
						t.Fatal(e)
					}
					code, info, _, err = compileIntegerMode(fn, ft, &state, summary, mode)
				}
				if err != nil || info.FrameBytes <= 2048 {
					t.Fatal("indexed references in large frame", info, err)
				}
				entry := thunkImageRV64(t, code)
				e, job, args, results, trap, _ := thunkFixtureRV64(t, 4)
				arena, err := coreruntime.NewArena(32)
				if err != nil {
					t.Fatal(err)
				}
				dir, cell := arena.Alloc(8), arena.Alloc(16)
				binary.LittleEndian.PutUint64(dir, uint64(thunkPointerRV64(cell)))
				job.SetGlobalsPtr(thunkPointerRV64(dir))
				t.Cleanup(func() {
					job.SetFuncRefDesc(0)
					job.SetGlobalsPtr(0)
					if err := arena.Close(); err != nil {
						t.Error(err)
					}
				})
				slots := 1
				if vector {
					slots = 2
				}
				binary.LittleEndian.PutUint64(args, 0xfedcba9876543210)
				binary.LittleEndian.PutUint64(args[8:], 0x123456789abcdef)
				// ref.func forms descriptor handles by bounded address arithmetic.
				// These private opaque bases are never dereferenced or exposed
				// through a public ABI; their high bits test carrier preservation.
				for _, base := range []uint64{1 << 32, 1 << 63, 0xcafebabe00000000} {
					job.SetFuncRefDesc(uintptr(base))
					for _, condition := range []uint64{0xdeadbeef00000000, 0xdeadbeef00000001, 0x80000000, 0xffffffff00000000} {
						binary.LittleEndian.PutUint64(args[8*slots:], condition)
						for i := range cell {
							cell[i] = 0xa5
						}
						for i := range results {
							results[i] = 0xa5
						}
						clear(trap)
						beforeArgs, beforeDir, wantCell := bytes.Clone(args), bytes.Clone(dir), bytes.Clone(cell)
						want := base + 80
						if uint32(condition) != 0 {
							want = base + 40
						}
						binary.LittleEndian.PutUint64(wantCell, want)
						if err := e.CallPrepared(entry, args, job.LinMemBase(), trap, results); err != nil || !bytes.Equal(cell, wantCell) || !bytes.Equal(dir, beforeDir) || !bytes.Equal(args, beforeArgs) || !bytes.Equal(results[:8*slots], args[:8*slots]) || !bytes.Equal(results[8*slots:], bytes.Repeat([]byte{0xa5}, len(results)-8*slots)) || !bytes.Equal(trap, make([]byte, len(trap))) {
							t.Fatal("indexed reference carrier/condition/sentinels", base, condition, err)
						}
					}
				}
				if a := testing.AllocsPerRun(20, func() {
					if err := e.CallPrepared(entry, args, job.LinMemBase(), trap, results); err != nil {
						panic(err)
					}
				}); a != 0 {
					t.Fatal("warm indexed reference allocation", a)
				}
			})
		}
	}
}
