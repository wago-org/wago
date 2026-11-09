//go:build arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"math"
	"testing"
)

//go:noinline
func packetOracleFP(a, b float64, op byte) float64 {
	switch op {
	case 0xa0:
		return a + b
	case 0xa1:
		return a - b
	case 0xa2:
		return a * b
	case 0xa3:
		return a / b
	}
	panic("bad oracle")
}

func floatStorePacketFixture(t testing.TB, offset uint32, inner, outer, final byte) *wasm.Module {
	body := []byte{0}
	c := func(x uint32) { body = append(body, 0x41); body = append(body, wasmtest.SLEB32(int32(x))...) }
	get := func(x byte) { body = append(body, 0x20, x) }
	c(48)
	c(42)
	body = append(body, 0x3a, 0, 0)
	for lane := byte(0); lane < 2; lane++ {
		get(0)
		get(0)
		body = append(body, 0x2b, 0)
		body = append(body, wasmtest.ULEB(offset+uint32(lane)*8)...)
		get(1)
		get(3 + lane)
		get(2)
		body = append(body, inner, outer, final, 0x39, 0)
		body = append(body, wasmtest.ULEB(offset+uint32(lane)*8)...)
	}
	for i := byte(1); i <= 4; i++ {
		c(56 + uint32(i)*8)
		get(i)
		body = append(body, 0x39, 0, 0)
	}
	c(0)
	body = append(body, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.F64, wasm.F64, wasm.F64, wasm.F64}, []wasm.ValType{wasm.I32}, body)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	if e := wasm.ValidateModule(m); e != nil {
		t.Fatal(e)
	}
	return m
}

func TestFloatStorePacketsMemoryTrapsOrderAndBorrowedLocals(t *testing.T) {
	saved := floatStorePacketsEnabled
	defer func() { floatStorePacketsEnabled = saved }()
	values := [][4]float64{{.125, .25, -4, 7}, {math.Copysign(0, -1), -1, 0, math.Copysign(0, -1)}, {math.Inf(1), .25, 2, -3}, {1, 0, 1, -1}, {math.SmallestNonzeroFloat64, .5, math.MaxFloat64, -math.MaxFloat64}, {math.Float64frombits(0x7ff8000000001234), math.Float64frombits(0x7ff8000000005678), -2, math.Float64frombits(0x7ff0000000002222)}}
	for _, inner := range []byte{0xa0, 0xa1, 0xa2, 0xa3} {
		for _, outer := range []byte{0xa2, 0xa3} {
			for _, final := range []byte{0xa0, 0xa1} {
				for _, offset := range []uint32{0, 1, 7, 65520, 0xfffffff8} {
					m := floatStorePacketFixture(t, offset, inner, outer, final)
					for _, base := range []uint32{0, 17, 65520, 65521, 65528, 65535, 0xffffffff} {
						for _, v := range values {
							initial := bytes.Repeat([]byte{0x39}, 65536)
							for lane := 0; lane < 2; lane++ {
								ea := uint64(base) + uint64(offset+uint32(lane)*8)
								if ea+8 <= 65536 {
									binary.LittleEndian.PutUint64(initial[ea:], math.Float64bits([]float64{1.25, -2.5}[lane]))
								}
							}
							want := append([]byte(nil), initial...)
							want[48] = 42
							traps := false
							var written [2]bool
							for lane := 0; lane < 2; lane++ {
								ea := uint64(base) + uint64(offset+uint32(lane)*8)
								if ea+8 > 65536 {
									traps = true
									break
								}
								old := math.Float64frombits(binary.LittleEndian.Uint64(want[ea:]))
								a := packetOracleFP(v[2+lane], v[1], inner)
								b := packetOracleFP(v[0], a, outer)
								result := packetOracleFP(old, b, final)
								binary.LittleEndian.PutUint64(want[ea:], math.Float64bits(result))
								written[lane] = true
							}
							if !traps {
								for i, x := range v {
									binary.LittleEndian.PutUint64(want[64+i*8:], math.Float64bits(x))
								}
							}
							for _, on := range []bool{false, true} {
								floatStorePacketsEnabled = on
								for _, compact := range []bool{false, true} {
									var stats ModuleStats
									opts := CompileOptions{CompactNative: compact}
									if diagnosticsEnabled {
										opts.Stats = &stats
									}
									args := []uint64{uint64(base) | 0xfeed00000000}
									for _, x := range v {
										args = append(args, math.Float64bits(x))
									}
									got, e := runLinearSpanMemory(t, m, opts, initial, args...)
									if (e != nil) != traps {
										t.Fatalf("trap mismatch off%x base%x on%v err%v", offset, base, on, e)
									}
									if traps {
										var trap *rt.TrapError
										if !errors.As(e, &trap) || trap.Code != rt.TrapLinMemOutOfBounds {
											t.Fatalf("bad trap: %v", e)
										}
									}
									// Wasm permits arithmetic NaN payload variation only in computed results.
									// Every preserved source local and every other memory byte remains exact.
									for lane, yes := range written {
										if !yes {
											continue
										}
										ea := uint64(base) + uint64(offset+uint32(lane)*8)
										wb := binary.LittleEndian.Uint64(want[ea:])
										gb := binary.LittleEndian.Uint64(got[ea:])
										if math.IsNaN(math.Float64frombits(wb)) && math.IsNaN(math.Float64frombits(gb)) && gb&(1<<51) != 0 {
											copy(got[ea:ea+8], want[ea:ea+8])
										}
									}
									if !bytes.Equal(got, want) {
										t.Fatalf("memory mismatch ops%x/%x/%x off%x base%x on%v compact%v values%x", inner, outer, final, offset, base, on, compact, args)
									}
									if diagnosticsEnabled && offset < 65536 {
										hit := stats.Funcs[0].Peephole["float-store-packet"] != 0
										if hit != on {
											t.Fatalf("admission on%v stats%v", on, stats.Funcs[0].Peephole)
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
}

func TestFloatStorePacketProbeBudgetPressureAndOwnedRecords(t *testing.T) {
	m := floatStorePacketFixture(t, 32, 0xa2, 0xa2, 0xa1)
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	r := wasm.ReaderFrom(m.Code[0].BodyBytes)
	var imm wasm.InstructionImmediate
	for r.HasNext() {
		op, e := r.Byte()
		if e != nil {
			t.Fatal(e)
		}
		if op == 0x20 {
			break
		}
		if e = classifier.ClassifyInto(&r, op, &imm); e != nil {
			t.Fatal(e)
		}
	}
	f := fn{m: m, nLocals: 5, nParams: 5, localType: []machineType{mtI32, mtF64, mtF64, mtF64, mtF64}, locals: []localDef{{reg: X19}, {reg: 8, isFloat: true}, {reg: 9, isFloat: true}, {reg: 10, isFloat: true}, {reg: 11, isFloat: true}}}
	start := r.Offset()
	if allocs := testing.AllocsPerRun(50, func() {
		f.floatStoreWork = 0
		p, ok := f.inspectFloatStorePacket(&r)
		if !ok || p.arithmetic != 3 || p.peak == 0 || p.offset != 32 || r.Offset() != start {
			panic("private probe failed or changed source")
		}
	}); allocs != 0 {
		t.Fatalf("probe allocations %v", allocs)
	}
	f.floatStoreWork = floatStoreWorkLimit - 1
	if _, ok := f.inspectFloatStorePacket(&r); ok || f.floatStoreWork != floatStoreWorkLimit || r.Offset() != start {
		t.Fatal("work/read budget contract")
	}
	f.floatStoreWork = 0
	f.locals[2].reg = regNone
	if _, ok := f.inspectFloatStorePacket(&r); ok {
		t.Fatal("unpinned uniform input admitted")
	}
	f.locals[2].reg = 9
	f.floatStoreWork = 0
	for _, reg := range gpAlloc {
		f.regUser[reg] = &elem{}
	}
	if done, e := f.emitFloatStoreCandidate(&r); done || e != nil || r.Offset() != start || f.a != nil {
		t.Fatal("GP pressure miss changed native/source state")
	}
	for _, reg := range gpAlloc {
		f.regUser[reg] = nil
	}
	f.fpinned = ^regMask(0)
	f.floatStoreWork = 0
	if done, e := f.emitFloatStoreCandidate(&r); done || e != nil || r.Offset() != start || f.a != nil {
		t.Fatal("FP pressure miss changed native/source state")
	}
}
