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

func TestReferenceSelectRV64FullWidthArmsAndCondition(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, form := range []byte{0x70, 0x63, 0x64} {
			t.Run(fmt.Sprintf("vector%t-form%x", vector, form), func(t *testing.T) {
				typ, pad := wasm.I64, uint32(254)
				if vector {
					typ, pad = wasm.V128, 126
				}
				ft := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{typ, wasm.I32}, Results: []wasm.ValType{typ}}
				body := []byte{0x23, 0}
				if form == 0x64 {
					body = append(body, 0xd4)
				}
				body = append(body, 0x23, 1)
				if form == 0x64 {
					body = append(body, 0xd4)
				}
				body = append(body, 0x20, 1, 0x1c, 1, form)
				if form != 0x70 {
					body = append(body, 0x70)
				}
				body = append(body, 0x24, 2, 0x20, 0, 0x0b)
				fn := &wasm.Func{BodyBytes: body, LocalDeclBytes: 1, Locals: wasm.Locals{Runs: []wasm.LocalRun{{Count: pad, Type: typ}}}}
				globals := []wasm.GlobalType{{Type: wasm.FuncRef}, {Type: wasm.FuncRef}, {Type: wasm.FuncRef, Mutable: true}}
				gs := [][]byte{}
				for _, g := range globals {
					gs = append(gs, wasmtest.GlobalEntry(g.Type, g.Mutable, []byte{0xd0, 0x70, 0x0b}))
				}
				m, err := wasm.DecodeModule(wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(ft.Params, ft.Results))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(6, wasmtest.Vec(gs...)), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body)))))
				if err != nil {
					t.Fatal(err)
				}
				m.Code[0] = *fn
				if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
					t.Fatal("fixture", err)
				}
				mode := emissionMode{production: true, foreignTableCalls: true, referenceCalls: true, localMutableReferences: true}
				var code []byte
				var info FunctionInfo
				if vector {
					code, info, _, err = compileSWARPairsWithGlobals(fn, ft, nil, nil, nil, globals, mode)
				} else {
					state := moduleState{production: true, importReferences: true, referenceCalls: true, localMutableReferences: true, globals: []globalSpec{{typ: wasm.FuncRef, wide: true}, {typ: wasm.FuncRef, wide: true}, {typ: wasm.FuncRef, wide: true, mutable: true}}}
					summary, e := walkInteger(fn, ft, &state, nil)
					if e != nil {
						t.Fatal(e)
					}
					code, info, _, err = compileIntegerMode(fn, ft, &state, summary, mode)
				}
				if err != nil || info.FrameBytes <= 2048 {
					t.Fatal("select large frame", info, err)
				}
				entry := thunkImageRV64(t, code)
				e, job, args, results, trap, _ := thunkFixtureRV64(t, 4)
				arena, err := coreruntime.NewArena(96)
				if err != nil {
					t.Fatal(err)
				}
				dir, cells := arena.Alloc(24), arena.Alloc(48)
				for i := range 3 {
					binary.LittleEndian.PutUint64(dir[8*i:], uint64(thunkPointerRV64(cells[16*i:])))
				}
				job.SetGlobalsPtr(thunkPointerRV64(dir))
				t.Cleanup(func() {
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
				if vector {
					binary.LittleEndian.PutUint64(args[8:], 0x123456789abcdef)
				}
				pairs := [][2]uint64{{1 << 32, 1 << 63}, {0xcafebabe80000001, ^uint64(0)}}
				if form != 0x64 {
					pairs = append(pairs, [2]uint64{0, 1 << 32}, [2]uint64{1 << 63, 0})
				}
				for _, pair := range pairs {
					for _, condition := range []uint64{0xdeadbeef00000000, 0xdeadbeef00000001, 0x80000000, 0xffffffff00000000} {
						for i := range cells {
							cells[i] = 0xa5
						}
						binary.LittleEndian.PutUint64(cells, pair[0])
						binary.LittleEndian.PutUint64(cells[16:], pair[1])
						before := bytes.Clone(cells)
						beforeDir := bytes.Clone(dir)
						binary.LittleEndian.PutUint64(args[8*slots:], condition)
						beforeArgs := bytes.Clone(args)
						for i := range results {
							results[i] = 0xa5
						}
						clear(trap)
						want := pair[1]
						if uint32(condition) != 0 {
							want = pair[0]
						}
						binary.LittleEndian.PutUint64(before[32:], want)
						if err := e.CallPrepared(entry, args, job.LinMemBase(), trap, results); err != nil || !bytes.Equal(cells, before) || !bytes.Equal(dir, beforeDir) || !bytes.Equal(args, beforeArgs) || !bytes.Equal(results[:8*slots], args[:8*slots]) || !bytes.Equal(results[8*slots:], bytes.Repeat([]byte{0xa5}, len(results)-8*slots)) || !bytes.Equal(trap, make([]byte, len(trap))) {
							t.Fatal("selected reference/condition/sentinels", pair, condition, err)
						}
					}
				}
				if a := testing.AllocsPerRun(20, func() {
					if err := e.CallPrepared(entry, args, job.LinMemBase(), trap, results); err != nil {
						panic(err)
					}
				}); a != 0 {
					t.Fatal("warm select allocation", a)
				}
			})
		}
	}
}
