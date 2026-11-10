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
)

func TestTailReferenceSourceRV64DecodedImportedCycles(t *testing.T) {
	for _, force := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, shared := range []bool{false, true} {
				t.Run(fmt.Sprintf("force%t-vector%t-shared%t", force, vector, shared), func(t *testing.T) {
					x := dynamicTailSourceFixtureWithKind(t, force, vector, shared, 2, false)
					f := x.f
					x.prepare(t, 2049)
					if err := f.e.CallPrepared(x.root.Base(), f.args, f.a.LinMemBase(), f.trap, f.results); err != nil {
						t.Fatal("decoded imported reference cycle", err)
					}
					cell := 0
					for _, typ := range x.i.types[2].Results {
						v := binary.LittleEndian.Uint64(x.input[(cell+1)*8:])
						if swarClassOf(typ).narrow() {
							v = uint64(uint32(v))
						}
						if binary.LittleEndian.Uint64(f.results[cell*8:]) != v {
							t.Fatal("raw typed reference result", cell)
						}
						cell++
						if wasm.EqualValType(typ, wasm.V128) {
							if binary.LittleEndian.Uint64(f.results[cell*8:]) != binary.LittleEndian.Uint64(x.input[(cell+1)*8:]) {
								t.Fatal("vector high reference result", cell)
							}
							cell++
						}
					}
					for n, table := range x.tables {
						if binary.LittleEndian.Uint64(table[64:]) != uint64(tailRAPointer(x.owners[n].descriptors)+80) {
							t.Fatal("actual guest reference producer not reached", n)
						}
					}
					f.assertReturned(t)
					if f.a.RV64InvocationView() != x.view {
						t.Fatal("original reference caller view lost")
					}
					if alloc := testing.AllocsPerRun(4, func() {
						x.prepare(t, 2049)
						if err := f.e.CallPrepared(x.root.Base(), f.args, f.a.LinMemBase(), f.trap, f.results); err != nil {
							panic(err)
						}
					}); alloc != 0 {
						t.Fatal("warm reference tail allocation", alloc)
					}
				})
			}
		}
	}
}

func TestTailReferenceSourceRV64NullAndForgedMetadataBeforePublication(t *testing.T) {
	for _, force := range []bool{false, true} {
		for _, null := range []bool{false, true} {
			t.Run(fmt.Sprintf("force%t-null%t", force, null), func(t *testing.T) {
				x := dynamicTailSourceFixtureWithKind(t, force, force, false, 2, null)
				f := x.f
				x.prepare(t, 1)
				want := coreruntime.TrapIndirectOutOfBounds
				if !null {
					// Candidate descriptor membership is genuine, but the live
					// signature key cannot relabel an authenticated frozen owner.
					binary.LittleEndian.PutUint64(x.owners[0].descriptors[48:], wasm.StructuralFuncTypeKey(x.i.types[1]))
					want = coreruntime.TrapCalledFnNotLinked
				}
				before := bytes.Clone(f.args)
				for i := range f.results {
					f.results[i] = 0xa5
				}
				err := f.e.CallPrepared(x.root.Base(), f.args, f.a.LinMemBase(), f.trap, f.results)
				var trap *coreruntime.TrapError
				if !errors.As(err, &trap) || trap.Code != want || len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != 1 || trap.Frames[0].ProgramCounter != x.i.plans[0].sourcePC {
					t.Fatal("reference source guard attribution", err)
				}
				if !bytes.Equal(before, f.args) || !bytes.Equal(f.results, bytes.Repeat([]byte{0xa5}, len(f.results))) {
					t.Fatal("reference source guard published transport")
				}
				f.assertCanaries(t)
			})
		}
	}
}

func TestTailReferenceSourceRV64ActiveCycleInterrupt(t *testing.T) {
	tailSourceActiveCycleInterrupt(t, 2)
}
