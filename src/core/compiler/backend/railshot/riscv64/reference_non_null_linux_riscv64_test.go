//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestReferenceNonNullRV64FullCarrierAndTrap(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, escaped := range []bool{false, true} {
			t.Run(fmt.Sprintf("vector%t-escaped%t", vector, escaped), func(t *testing.T) {
				e, job, args, results, trap, _ := thunkFixtureRV64(t, 4)
				typ, padding := wasm.I64, uint32(0)
				if vector {
					typ = wasm.V128
				}
				if escaped {
					padding = 254
					if vector {
						padding = 126
					}
				}
				ft := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{typ}, Results: []wasm.ValType{typ}}
				fn := &wasm.Func{LocalDeclBytes: 7, Locals: wasm.Locals{Runs: []wasm.LocalRun{{Count: padding, Type: typ}, {Count: 1, Type: wasm.FuncRef}}}}
				// Retain a reference through a local and a reference-valued block
				// join. Numeric/vector arguments must survive both null checks.
				fn.BodyBytes = []byte{0x23, 0, 0xd4, 0x21}
				fn.BodyBytes = append(fn.BodyBytes, wasmtest.ULEB(padding+1)...)
				fn.BodyBytes = append(fn.BodyBytes, 0x02, 0x70, 0x20)
				fn.BodyBytes = append(fn.BodyBytes, wasmtest.ULEB(padding+1)...)
				fn.BodyBytes = append(fn.BodyBytes, 0x0c, 0, 0x0b, 0xd4, 0x24, 1, 0x20, 0, 0x0b)
				globals := []wasm.GlobalType{{Type: wasm.FuncRef}, {Type: wasm.FuncRef, Mutable: true}}
				mode := emissionMode{production: true, foreignTableCalls: true, referenceCalls: true, localMutableReferences: true, functionIndex: 9, entryPC: fn.LocalDeclBytes}
				var code []byte
				var info FunctionInfo
				var err error
				if vector {
					code, info, _, err = compileSWARPairsWithGlobals(fn, ft, nil, nil, nil, globals, mode)
				} else {
					state := moduleState{production: true, importReferences: true, referenceCalls: true, localMutableReferences: true, globals: []globalSpec{{typ: wasm.FuncRef, wide: true}, {typ: wasm.FuncRef, wide: true, mutable: true}}}
					summary, summaryErr := walkInteger(fn, ft, &state, nil)
					if summaryErr != nil {
						t.Fatal(summaryErr)
					}
					code, info, _, err = compileIntegerMode(fn, ft, &state, summary, mode)
				}
				if err != nil || escaped && info.FrameBytes <= 2048 {
					t.Fatal("reference lowering/frame", info, err)
				}
				entry := thunkImageRV64(t, code)
				arena, err := coreruntime.NewArena(64)
				if err != nil {
					t.Fatal(err)
				}
				dir, cells := arena.Alloc(16), arena.Alloc(32)
				binary.LittleEndian.PutUint64(dir, uint64(thunkPointerRV64(cells)))
				binary.LittleEndian.PutUint64(dir[8:], uint64(thunkPointerRV64(cells[16:])))
				job.SetGlobalsPtr(thunkPointerRV64(dir))
				t.Cleanup(func() {
					job.SetGlobalsPtr(0)
					if err := arena.Close(); err != nil {
						t.Error(err)
					}
				})
				originalDir := bytes.Clone(dir)
				binary.LittleEndian.PutUint64(args, 0x80000000deadbeef)
				binary.LittleEndian.PutUint64(args[8:], 0x7ff0000000000001)
				originalArgs := bytes.Clone(args)
				slots := 1
				if vector {
					slots = 2
				}
				// Deliberately opaque non-pointers prove the primitive only checks
				// nullness. They never cross a descriptor dereference or public ABI.
				for _, value := range []uint64{1, 1 << 32, 1 << 63, 0xdeadbeef80000001, ^uint64(0), 0, 1 << 48} {
					clear(trap)
					for i := range cells {
						cells[i] = 0xa5
					}
					binary.LittleEndian.PutUint64(cells, value)
					before := bytes.Clone(cells)
					for i := range results {
						results[i] = 0xa5
					}
					err := e.CallPrepared(entry, args, job.LinMemBase(), trap, results)
					if value == 0 {
						var tr *coreruntime.TrapError
						if !errors.As(err, &tr) || tr.Code != coreruntime.TrapNullReference || binary.LittleEndian.Uint64(trap[16:]) != uint64(fn.LocalDeclBytes+2)<<32|10 || !bytes.Equal(cells, before) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
							t.Fatal("null trap attribution/before writes", err)
						}
					} else {
						binary.LittleEndian.PutUint64(before[16:], value)
						if err != nil || !bytes.Equal(cells, before) || !bytes.Equal(results[:8*slots], args[:8*slots]) || !bytes.Equal(results[8*slots:], bytes.Repeat([]byte{0xa5}, len(results)-8*slots)) {
							t.Fatalf("opaque reference %#x or live numeric/vector carrier changed: %v", value, err)
						}
					}
					if !bytes.Equal(dir, originalDir) || !bytes.Equal(args, originalArgs) {
						t.Fatal("reference check changed directory/arguments")
					}
				}
				if a := testing.AllocsPerRun(20, func() {
					if err := e.CallPrepared(entry, args, job.LinMemBase(), trap, results); err != nil {
						panic(err)
					}
				}); a != 0 {
					t.Fatal("warm reference check allocations", a)
				}
			})
		}
	}
}
