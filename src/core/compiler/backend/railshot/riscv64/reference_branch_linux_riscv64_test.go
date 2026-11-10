//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestReferenceBranchRV64FullCarrierAndTransfers(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, nonNull := range []bool{false, true} {
			for _, deep := range []bool{false, true} {
				t.Run(fmt.Sprintf("vector%t-nonnull%t-deep%t", vector, nonNull, deep), func(t *testing.T) {
					depth := 0
					if deep {
						depth = maxControlNesting
						if nonNull {
							depth--
						}
					}
					m, err := wasm.DecodeModule(referenceBranchSource(vector, nonNull, depth))
					if err != nil {
						t.Fatal(err)
					}
					fn := &m.Code[0]
					typ, padding := wasm.I64, uint32(254)
					if vector {
						typ, padding = wasm.V128, 126
					}
					fn.Locals.Runs = []wasm.LocalRun{{Count: padding, Type: typ}}
					if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
						t.Fatal(err)
					}
					ft, _ := m.LocalFuncType(0)
					globals := []wasm.GlobalType{{Type: wasm.FuncRef}, {Type: wasm.FuncRef, Mutable: true}}
					mode := emissionMode{production: true, foreignTableCalls: true, referenceCalls: true, localMutableReferences: true}
					var code []byte
					var info FunctionInfo
					if vector {
						code, info, _, err = compileSWARPairsWithGlobals(fn, ft, nil, nil, nil, globals, mode)
					} else {
						state := moduleState{production: true, importReferences: true, referenceCalls: true, localMutableReferences: true, globals: []globalSpec{{typ: wasm.FuncRef, wide: true}, {typ: wasm.FuncRef, wide: true, mutable: true}}}
						summary, e := walkInteger(fn, ft, &state, nil)
						if e != nil {
							t.Fatal(e)
						}
						code, info, _, err = compileIntegerMode(fn, ft, &state, summary, mode)
					}
					if err != nil || info.FrameBytes <= 2048 {
						t.Fatal("reference branch frame", info, err)
					}
					entry := thunkImageRV64(t, code)
					e, job, args, results, trap, _ := thunkFixtureRV64(t, 4)
					ar, err := coreruntime.NewArena(64)
					if err != nil {
						t.Fatal(err)
					}
					dir, cells := ar.Alloc(16), ar.Alloc(32)
					binary.LittleEndian.PutUint64(dir, uint64(thunkPointerRV64(cells)))
					binary.LittleEndian.PutUint64(dir[8:], uint64(thunkPointerRV64(cells[16:])))
					job.SetGlobalsPtr(thunkPointerRV64(dir))
					t.Cleanup(func() {
						job.SetGlobalsPtr(0)
						if err := ar.Close(); err != nil {
							t.Error(err)
						}
					})
					beforeDir := bytes.Clone(dir)
					slots := 2
					binary.LittleEndian.PutUint64(args, 0xfedcba9876543210)
					if vector {
						slots = 3
						binary.LittleEndian.PutUint64(args[8:], 0x7ff0000000000001)
					}
					binary.LittleEndian.PutUint64(args[8*(slots-1):], 0xdeadbeef80000001)
					beforeArgs := bytes.Clone(args)
					want := bytes.Clone(args[:8*slots])
					binary.LittleEndian.PutUint64(want[8*(slots-1):], 0x80000001)
					for _, value := range []uint64{0, 1, 1 << 32, 1 << 63, 0xcafebabe80000001, ^uint64(0), 0, 1 << 48} {
						for i := range cells {
							cells[i] = 0xa5
						}
						binary.LittleEndian.PutUint64(cells, value)
						before := bytes.Clone(cells)
						if nonNull || value != 0 {
							binary.LittleEndian.PutUint64(before[16:], value)
						}
						for i := range results {
							results[i] = 0xa5
						}
						clear(trap)
						err := e.CallPrepared(entry, args, job.LinMemBase(), trap, results)
						if err != nil || !bytes.Equal(cells, before) || !bytes.Equal(results[:len(want)], want) || !bytes.Equal(results[len(want):], bytes.Repeat([]byte{0xa5}, len(results)-len(want))) || !bytes.Equal(trap, make([]byte, len(trap))) {
							t.Fatalf("reference branch %#x transfer/fallthrough/trap: %v", value, err)
						}
						if !bytes.Equal(args, beforeArgs) || !bytes.Equal(dir, beforeDir) {
							t.Fatal("reference branch changed inputs/directory")
						}
					}
					if a := testing.AllocsPerRun(20, func() {
						if err := e.CallPrepared(entry, args, job.LinMemBase(), trap, results); err != nil {
							panic(err)
						}
					}); a != 0 {
						t.Fatal("warm reference branch allocation", a)
					}
				})
			}
		}
	}
}

func TestReferenceBranchRV64LoopParameters(t *testing.T) {
	for _, vector := range []bool{false, true} {
		t.Run(fmt.Sprintf("SWAR%t", vector), func(t *testing.T) {
			ft := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}
			fn := &wasm.Func{LocalDeclBytes: 1, BodyBytes: []byte{
				0x20, 0, 0x03, 0, // loop (param i32) (result i32)
				0x22, 0, 0x20, 0, 0x45, 0x04, 0x40, 0x00, 0x0b, // bound a faulty extra backedge by trapping at zero
				0x41, 1, 0x6b, 0x22, 0, // decrement retained numeric label parameter
				0x20, 0, 0x45, 0x04, 0x70, 0x23, 0, 0x05, 0xd0, 0x70, 0x0b,
				0xd5, 0, 0x24, 1, 0x0b, 0x0b,
			}}
			pad, typ := uint32(255), wasm.I64
			if vector {
				pad, typ = 127, wasm.V128
			}
			fn.Locals.Runs = []wasm.LocalRun{{Count: pad, Type: typ}}
			m, err := wasm.DecodeModule(referenceBranchSource(false, false, 0))
			if err != nil {
				t.Fatal(err)
			}
			m.Types[0].SubTypes[0].Comp, m.Code[0] = *ft, *fn
			if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
				t.Fatal("loop fixture", err)
			}
			globals := []wasm.GlobalType{{Type: wasm.FuncRef}, {Type: wasm.FuncRef, Mutable: true}}
			mode := emissionMode{production: true, interruptible: true, foreignTableCalls: true, referenceCalls: true, localMutableReferences: true}
			var code []byte
			if vector {
				code, _, _, err = compileSWARPairsWithGlobals(fn, ft, nil, []*wasm.CompType{ft}, nil, globals, mode)
			} else {
				state := moduleState{production: true, importReferences: true, referenceCalls: true, localMutableReferences: true, types: []functionSpec{{params: []bool{false}, results: []bool{false}}}, globals: []globalSpec{{typ: wasm.FuncRef, wide: true}, {typ: wasm.FuncRef, wide: true, mutable: true}}}
				summary, e := walkInteger(fn, ft, &state, nil)
				if e != nil {
					t.Fatal(e)
				}
				code, _, _, err = compileIntegerMode(fn, ft, &state, summary, mode)
			}
			if err != nil {
				t.Fatal(err)
			}
			entry := thunkImageRV64(t, code)
			e, job, args, results, trap, _ := thunkFixtureRV64(t, 2)
			_, cell := swarGlobalCellsRV64(t, job, 0x7e, 2)
			t.Cleanup(func() { job.SetGlobalsPtr(0) })
			for _, ref := range []uint64{1, 1 << 32, 1 << 63, 0xcafebabe80000001} {
				binary.LittleEndian.PutUint64(cell, ref)
				binary.LittleEndian.PutUint64(args, 0xcafebabe00000005)
				for i := range results {
					results[i] = 0xa5
				}
				if err := e.CallPrepared(entry, args, job.LinMemBase(), trap, results); err != nil || binary.LittleEndian.Uint64(results) != 0 || binary.LittleEndian.Uint64(cell) != ref || !bytes.Equal(results[8:], bytes.Repeat([]byte{0xa5}, len(results)-8)) {
					t.Fatalf("loop backedge/fallthrough reference %#x: %v", ref, err)
				}
			}
		})
	}
}
