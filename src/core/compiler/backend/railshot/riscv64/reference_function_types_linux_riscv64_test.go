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

func TestReferenceFunctionRV64MixedDirectCalls(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, indexed := range []bool{false, true} {
			for _, nullable := range []bool{false, true} {
				t.Run(fmt.Sprintf("vector%t-indexed%t-nullable%t", vector, indexed, nullable), func(t *testing.T) {
					typ, pad := wasm.F64, uint32(252)
					if vector {
						typ, pad = wasm.V128, 124
					}
					heap := wasm.AbsHeap(wasm.HeapFunc)
					if indexed {
						heap = wasm.IndexedHeap(wasm.TypeIdx{})
					}
					ref := wasm.RefVal(wasm.Ref(nullable, heap, false))
					params := []wasm.ValType{ref, wasm.I32, ref, typ}
					leaf := &wasm.CompType{Kind: wasm.CompFunc, Params: params, Results: []wasm.ValType{typ, ref, wasm.I32, ref}}
					caller := &wasm.CompType{Kind: wasm.CompFunc, Params: params, Results: []wasm.ValType{ref, typ, ref, wasm.I32, ref}}
					numeric := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}
					// Caller keeps a reference below the call operands, preserves two distinct
					// refs through a numeric call, then returns the actual arriving leaf tuple.
					m := referenceFunctionModule([]*wasm.CompType{{Kind: wasm.CompFunc}, leaf, caller, numeric}, []uint32{0, 1, 2, 2, 3}, [][]byte{{0x0b}, {0x20, 3, 0x20, 0, 0x20, 1, 0x20, 2, 0x0b}, {0x20, 0, 0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x10, 1, 0x21, 2, 0x10, 4, 0x20, 2, 0x0b}, {0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x10, 2, 0x0b}, {0x20, 0, 0x41, 1, 0x6a, 0x0b}})
					for _, i := range []int{1, 2, 3} {
						m.Code[i].Locals.Runs = []wasm.LocalRun{{Count: pad, Type: typ}}
					}
					code, infos := referenceFunctionImage(t, m, vector)
					if infos[2].FrameBytes <= 2048 || infos[2].DirectCalls != 2 || infos[3].DirectCalls != 1 {
						t.Fatal("actual large nested call frames", infos[2], infos[3])
					}
					base := thunkImageRV64(t, code)
					e, job, args, results, trap, _ := thunkFixtureRV64(t, 256)
					for _, first := range []uint64{0x8000000000000040, 0xcafebabe000000a0} {
						for _, count := range []uint64{0xdeadbeef00000007, 0xffffffff00000000, 0x12345678ffffffff, 0x80000000} {
							// These high-bit opaque references exercise only transport; neither fake
							// descriptor is dereferenced or published by this private fixture.
							input := []uint64{first, count, 0xfedcba9876543210, 0x7ff0000000000001}
							if vector {
								input = append(input, 0x0123456789abcdef)
							}
							want := []uint64{first, input[3]}
							if vector {
								want = append(want, input[4])
							}
							want = append(want, first, uint64(uint32(count)+1), input[2])
							write := func() {
								for i := range args {
									args[i] = 0x5a
								}
								for i, v := range input {
									binary.LittleEndian.PutUint64(args[8*i:], v)
								}
								for i := range results {
									results[i] = 0xa5
								}
								clear(trap)
							}
							for _, entry := range []int{2, 3} {
								write()
								err := e.CallPrepared(base+uintptr(infos[entry].Entry), args, job.LinMemBase(), trap, results)
								if err != nil {
									t.Fatal("mixed reference call", err)
								}
								for i, v := range want {
									if got := binary.LittleEndian.Uint64(results[8*i:]); got != v {
										t.Fatal("arriving reference/numeric/vector result", entry, i, got, v)
									}
								}
								if !bytes.Equal(results[8*len(want):], bytes.Repeat([]byte{0xa5}, len(results)-8*len(want))) || !bytes.Equal(args[8*len(input):], bytes.Repeat([]byte{0x5a}, len(args)-8*len(input))) || !bytes.Equal(trap, make([]byte, len(trap))) {
									t.Fatal("compact call sentinels")
								}
							}
							if a := testing.AllocsPerRun(10, func() {
								write()
								if err := e.CallPrepared(base+uintptr(infos[3].Entry), args, job.LinMemBase(), trap, results); err != nil {
									panic(err)
								}
							}); a != 0 {
								t.Fatal("warm private reference calls", a)
							}
						}
					}
				})
			}
		}
	}
}

func TestReferenceFunctionRV64RecursiveStackTrapAndReuse(t *testing.T) {
	for _, vector := range []bool{false, true} {
		t.Run(fmt.Sprintf("vector%t", vector), func(t *testing.T) {
			ft := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.FuncRef, wasm.I32}, Results: []wasm.ValType{wasm.FuncRef}}
			body := []byte{0x20, 1, 0x45, 4, 0x70, 0x20, 0, 5, 0x20, 0, 0x20, 1, 0x41, 1, 0x6b, 0x10, 0, 0x0b, 0x0b}
			m := referenceFunctionModule([]*wasm.CompType{ft}, []uint32{0}, [][]byte{body})
			code, infos := referenceFunctionImage(t, m, vector)
			if !vector && !infos[0].Result64 {
				t.Fatal("single reference result metadata")
			}
			entry := thunkImageRV64(t, code)
			e, job, args, results, trap, _ := thunkFixtureRV64(t, 256)
			for _, depth := range []uint32{0, 1, 2, 17, 31, 0xffffffff, 1} {
				binary.LittleEndian.PutUint64(args, 0xfedcba9800000040)
				binary.LittleEndian.PutUint64(args[8:], 0xdeadbeef00000000|uint64(depth))
				clear(trap)
				for i := range results {
					results[i] = 0xa5
				}
				err := e.CallPrepared(entry, args, job.LinMemBase(), trap, results)
				if depth == 0xffffffff {
					var tr *coreruntime.TrapError
					if !errors.As(err, &tr) || tr.Code != coreruntime.TrapStackFenceBreached || binary.LittleEndian.Uint64(trap[16:]) != uint64(1)<<32|1 || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
						t.Fatal("recursive reference stack bound/source/prewrite", err, trap[16:24])
					}
				} else if err != nil || binary.LittleEndian.Uint64(results) != 0xfedcba9800000040 || !bytes.Equal(results[8:], bytes.Repeat([]byte{0xa5}, len(results)-8)) {
					t.Fatal("recursive reference result/reuse", depth, err, results[:8])
				}
			}
		})
	}
}

func TestReferenceFunctionRV64MaximumCompactCells(t *testing.T) {
	testReferenceFunctionMaximumCompactCells(t, false)
}

func TestReferenceTailRV64MaximumCompactCells(t *testing.T) {
	testReferenceFunctionMaximumCompactCells(t, true)
}

func testReferenceFunctionMaximumCompactCells(t *testing.T, tails bool) {
	t.Helper()
	for _, vector := range []bool{false, true} {
		for _, dense := range []bool{false, true} {
			if dense && !vector {
				continue
			}
			t.Run(fmt.Sprintf("vector%t-dense%t", vector, dense), func(t *testing.T) {
				n := 256
				if vector {
					n = 128
				}
				params := make([]wasm.ValType, n)
				resultsTypes := make([]wasm.ValType, n)
				for i := range params {
					params[i] = wasm.FuncRef
					if vector && (dense && i > 0 || !dense && i%2 != 0) {
						params[i] = wasm.V128
					}
					resultsTypes[n-i-1] = params[i]
				}
				sig := &wasm.CompType{Kind: wasm.CompFunc, Params: params, Results: resultsTypes}
				var caller, leaf []byte
				var input, want []uint64
				groups := make([][]uint64, n)
				for i := range params {
					caller = append(caller, 0x20)
					caller = append(caller, wasmtest.ULEB(uint32(i))...)
					leaf = append(leaf, 0x20)
					leaf = append(leaf, wasmtest.ULEB(uint32(n-i-1))...)
					groups[i] = []uint64{0xfedcba9800000040 ^ uint64(i)*0x10101}
					if vector && (dense && i > 0 || !dense && i%2 != 0) {
						groups[i] = append(groups[i], 0x8123456700000080^uint64(i)*0x1010101)
					}
					input = append(input, groups[i]...)
				}
				for i := n - 1; i >= 0; i-- {
					want = append(want, groups[i]...)
				}
				op := byte(0x10)
				if tails {
					op = 0x12
				}
				caller = append(caller, op, 1, 0x0b)
				leaf = append(leaf, 0x0b)
				m := referenceFunctionModule([]*wasm.CompType{sig}, []uint32{0, 0}, [][]byte{caller, leaf})
				code, infos := referenceFunctionImageWithTails(t, m, vector, tails)
				wantSlots := 256
				if vector {
					wantSlots = 192
					if dense {
						wantSlots = 255
					}
				}
				if len(input) != wantSlots || infos[0].Params != wantSlots || infos[0].Results != wantSlots {
					t.Fatal("compact transfer extent", len(input), infos[0])
				}
				entry := thunkImageRV64(t, code)
				e, job, args, results, trap, _ := thunkFixtureRV64(t, 256)
				write := func() {
					for i := range args {
						args[i] = 0x5a
					}
					for i, v := range input {
						binary.LittleEndian.PutUint64(args[i*8:], v)
					}
					for i := range results {
						results[i] = 0xa5
					}
					clear(trap)
				}
				write()
				if err := e.CallPrepared(entry, args, job.LinMemBase(), trap, results); err != nil {
					t.Fatal(err)
				}
				for i, v := range want {
					if got := binary.LittleEndian.Uint64(results[i*8:]); got != v {
						t.Fatal("maximum reference/vector compact cell", i, got, v)
					}
				}
				if !bytes.Equal(results[8*len(want):], bytes.Repeat([]byte{0xa5}, len(results)-8*len(want))) || !bytes.Equal(args[8*len(input):], bytes.Repeat([]byte{0x5a}, len(args)-8*len(input))) {
					t.Fatal("maximum compact boundary")
				}
				if a := testing.AllocsPerRun(10, func() {
					write()
					if err := e.CallPrepared(entry, args, job.LinMemBase(), trap, results); err != nil {
						panic(err)
					}
				}); a != 0 {
					t.Fatal("maximum reference warm allocation", a)
				}
			})
		}
	}
}

func TestReferenceFunctionRV64RootReferenceJoins(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, exit := range []string{"end", "return", "branch"} {
			t.Run(fmt.Sprintf("vector%t-%s", vector, exit), func(t *testing.T) {
				ft := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.FuncRef}, Results: []wasm.ValType{wasm.FuncRef}}
				body := []byte{0x20, 0, 0x10, 1}
				if exit == "return" {
					body = append(body, 0x0f)
				} else if exit == "branch" {
					body = append(body, 0x0c, 0)
				}
				body = append(body, 0x0b)
				m := referenceFunctionModule([]*wasm.CompType{ft}, []uint32{0, 0}, [][]byte{body, {0x20, 0, 0x0b}})
				code, infos := referenceFunctionImage(t, m, vector)
				if !vector && !infos[0].Result64 {
					t.Fatal("root reference result width")
				}
				entry := thunkImageRV64(t, code)
				e, job, args, results, trap, _ := thunkFixtureRV64(t, 8)
				for _, ref := range []uint64{0, 1 << 63, 0x8000000080000000, 0x80000000, 0xffffffffffffffff} {
					for i := range args {
						args[i] = 0x5a
					}
					binary.LittleEndian.PutUint64(args, ref)
					before := bytes.Clone(args)
					for i := range results {
						results[i] = 0xa5
					}
					clear(trap)
					if err := e.CallPrepared(entry, args, job.LinMemBase(), trap, results); err != nil || binary.LittleEndian.Uint64(results) != ref || !bytes.Equal(results[8:], bytes.Repeat([]byte{0xa5}, len(results)-8)) || !bytes.Equal(args, before) || !bytes.Equal(trap, make([]byte, len(trap))) {
						t.Fatal("root reference join/sentinels", ref, err)
					}
				}
			})
		}
	}
}
