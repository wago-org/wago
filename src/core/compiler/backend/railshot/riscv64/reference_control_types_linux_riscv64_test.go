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

func TestReferenceIndexedControlRV64MixedTransfers(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, indexed := range []bool{false, true} {
			for _, nullable := range []bool{false, true} {
				for _, edge := range []string{"br-if", "br-null", "br-nonnull"} {
					t.Run(fmt.Sprintf("vector%t-indexed%t-nullable%t-%s", vector, indexed, nullable, edge), func(t *testing.T) {
						typ, pad := wasm.I64, uint32(252)
						if vector {
							typ, pad = wasm.V128, 124
						}
						heap := wasm.AbsHeap(wasm.HeapFunc)
						if indexed {
							heap = wasm.IndexedHeap(wasm.TypeIdx{Index: 0})
						}
						ref := wasm.RefVal(wasm.Ref(nullable, heap, false))
						counter, saved := pad+2, pad+3
						body := append([]byte{0x41, 3, 0x21}, wasmtest.ULEB(counter)...)
						body = append(body, 0x20, 0, 0x20, 1, 0xd2, 0, 0x20, 1, 4, 3, 0x1a, 0xd2, 1, 5, 0x0b)
						// All three values survive each real loop backedge. The global proves
						// that the loop executes three iterations instead of merely falling out.
						body = append(body, 0x21)
						body = append(body, wasmtest.ULEB(saved)...)
						body = append(body, 0x20)
						body = append(body, wasmtest.ULEB(saved)...)
						body = append(body, 3, 3, 0x23, 1, 0x41, 1, 0x6a, 0x24, 1)
						if edge == "br-nonnull" {
							body = append(body, 0x21)
							body = append(body, wasmtest.ULEB(saved)...)
						}
						body = append(body, 0x20)
						body = append(body, wasmtest.ULEB(counter)...)
						body = append(body, 0x41, 1, 0x6b, 0x22)
						body = append(body, wasmtest.ULEB(counter)...)
						heapByte := byte(0x70)
						if indexed {
							heapByte = 0
						}
						switch edge {
						case "br-if":
							body = append(body, 0x0d, 0)
						case "br-null":
							body = append(body, 4, 0x63, heapByte, 0xd0, heapByte, 5, 0xd2, 0, 0x0b, 0xd5, 0, 0x1a)
						case "br-nonnull":
							body = append(body, 4, 0x63, heapByte, 0x20)
							body = append(body, wasmtest.ULEB(saved)...)
							body = append(body, 5, 0xd0, heapByte, 0x0b, 0xd6, 0, 0x20)
							body = append(body, wasmtest.ULEB(saved)...)
						}
						body = append(body, 0x0b, 0x21)
						body = append(body, wasmtest.ULEB(saved)...)
						// Preserve the tuple that actually arrived over the loop edge;
						// reloading original inputs here would hide transfer corruption.
						body = append(body, 0x21, 1, 0x21, 0, 2, 4, 2, 4, 0x42, 9, 0x20, 0, 0x20, 1, 0x20)
						body = append(body, wasmtest.ULEB(saved)...)
						// An extra lower operand forces overlapping source/destination homes.
						// Both the inner target and outer/default target transport the same tuple.
						body = append(body, 0x20, 1, 0x0e, 1, 0, 1, 0x0b, 0x0b, 0x24, 0, 0x0b)
						m, err := wasm.DecodeModule(wasmtest.Module(
							wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{typ, wasm.I32}, []wasm.ValType{typ, wasm.I32}), wasmtest.FuncType([]wasm.ValType{typ, wasm.I32, wasm.FuncRef}, []wasm.ValType{typ, wasm.I32, wasm.FuncRef}), wasmtest.FuncType(nil, []wasm.ValType{typ, wasm.I32, wasm.FuncRef}))),
							wasmtest.Section(3, []byte{3, 0, 1, 2}),
							wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.FuncRef, true, []byte{0xd0, 0x70, 0x0b}), wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b}))),
							wasmtest.Section(9, []byte{1, 3, 0, 2, 0, 1}),
							wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}), wasmtest.Code([]byte{0x0b}), wasmtest.Code(body))),
						))
						if err != nil {
							t.Fatal(err)
						}
						m.Types[3].SubTypes[0].Comp.Params[2] = ref
						m.Types[3].SubTypes[0].Comp.Results[2] = ref
						m.Types[4].SubTypes[0].Comp.Results[2] = ref
						fn := &m.Code[2]
						fn.Locals.Runs = []wasm.LocalRun{{Count: pad, Type: typ}, {Count: 1, Type: wasm.I32}, {Count: 1, Type: ref}}
						if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
							t.Fatal("mixed control frontend proof", err)
						}
						types, specs := referenceControlDirectory(m)
						globals := []wasm.GlobalType{{Type: wasm.FuncRef, Mutable: true}, {Type: wasm.I32, Mutable: true}}
						mode := emissionMode{production: true, foreignTableCalls: true, referenceCalls: true, localMutableReferences: true, interruptible: true}
						var code []byte
						var info FunctionInfo
						if vector {
							code, info, _, err = compileSWARPairsWithGlobals(fn, types[2], nil, types, types[:3], globals, mode)
						} else {
							state := moduleState{production: true, importReferences: true, referenceCalls: true, localMutableReferences: true, types: specs, functions: specs[:3], globals: []globalSpec{{typ: wasm.FuncRef, wide: true, mutable: true}, {typ: wasm.I32, mutable: true}}}
							summary, e := walkInteger(fn, types[2], &state, nil)
							if e != nil {
								t.Fatal(e)
							}
							code, info, _, err = compileIntegerMode(fn, types[2], &state, summary, mode)
						}
						if err != nil || info.FrameBytes <= 2048 {
							t.Fatal("mixed large control frame", info, err)
						}
						entry := thunkImageRV64(t, code)
						e, job, args, results, trap, _ := thunkFixtureRV64(t, 4)
						arena, err := coreruntime.NewArena(64)
						if err != nil {
							t.Fatal(err)
						}
						dir, cell, count := arena.Alloc(16), arena.Alloc(16), arena.Alloc(16)
						binary.LittleEndian.PutUint64(dir, uint64(thunkPointerRV64(cell)))
						binary.LittleEndian.PutUint64(dir[8:], uint64(thunkPointerRV64(count)))
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
						// Private ref.func address arithmetic creates opaque handles. No target
						// or descriptor bytes at these high-bit bases are ever dereferenced.
						for _, base := range []uint64{1 << 32, 1 << 63, 0xcafebabe00000000} {
							job.SetFuncRefDesc(uintptr(base))
							for _, condition := range []uint64{0xdeadbeef00000000, 0xdeadbeef00000001, 0x80000000, 0xffffffff00000000} {
								binary.LittleEndian.PutUint64(args[8*slots:], condition)
								for i := range cell {
									cell[i] = 0xa5
								}
								for i := range count {
									count[i] = 0xa5
								}
								for i := range results {
									results[i] = 0xa5
								}
								binary.LittleEndian.PutUint64(count, 0)
								clear(trap)
								beforeArgs, beforeDir, wantCell, wantCount := bytes.Clone(args), bytes.Clone(dir), bytes.Clone(cell), bytes.Clone(count)
								wantRef := base + 40
								if uint32(condition) != 0 {
									wantRef = base + 80
								}
								binary.LittleEndian.PutUint64(wantCell, wantRef)
								binary.LittleEndian.PutUint64(wantCount, 3)
								wantResults := bytes.Clone(results)
								copy(wantResults[:8*slots], args[:8*slots])
								binary.LittleEndian.PutUint64(wantResults[8*slots:], uint64(uint32(condition)))
								if err := e.CallPrepared(entry, args, job.LinMemBase(), trap, results); err != nil || !bytes.Equal(cell, wantCell) || !bytes.Equal(count, wantCount) || !bytes.Equal(dir, beforeDir) || !bytes.Equal(args, beforeArgs) || !bytes.Equal(results, wantResults) || !bytes.Equal(trap, make([]byte, len(trap))) {
									t.Fatal("control backedge/join/branch/full carrier/sentinels", base, condition, err, cell, count, results)
								}
							}
						}
						if a := testing.AllocsPerRun(20, func() {
							if err := e.CallPrepared(entry, args, job.LinMemBase(), trap, results); err != nil {
								panic(err)
							}
						}); a != 0 {
							t.Fatal("warm control allocation", a)
						}
					})
				}
			}
		}
	}
}
