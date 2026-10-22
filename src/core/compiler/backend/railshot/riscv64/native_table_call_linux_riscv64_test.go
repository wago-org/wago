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

// An owned-table declaration establishes the validated private compiler shape;
// the test owner supplies a retained foreign canonical descriptor at runtime.
// The module/public profiles themselves never enable foreignTableCalls.
func nativeTableCallModuleRV64(t *testing.T, types []wasm.ValType) *wasm.Module {
	t.Helper()
	params := append([]wasm.ValType(nil), types...)
	computed := len(types) == 128 && wasm.EqualValType(types[0], wasm.V128)
	if computed {
		params = params[:127] // leave room for the i32 index in128 logical locals
	}
	params = append(params, wasm.I32)
	identity, caller := []byte{}, []byte{}
	for index := range types {
		identity = append(identity, 0x20)
		identity = append(identity, wasmtest.ULEB(uint32(index))...)
		if computed && index == 127 {
			caller = binary.LittleEndian.AppendUint64(append(caller, 0xfd, 12), 0x7ff000000000007f)
			caller = binary.LittleEndian.AppendUint64(caller, 0x80000000ff80017f)
		} else {
			caller = append(caller, 0x20)
			caller = append(caller, wasmtest.ULEB(uint32(index))...)
		}
	}
	identity = append(identity, 0x0b)
	caller = append(caller, 0x20)
	caller = append(caller, wasmtest.ULEB(uint32(len(params)-1))...)
	caller = append(caller, 0x11, 0, 0, 0x0b)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types), wasmtest.FuncType(params, types))),
		wasmtest.Section(3, []byte{2, 0, 1}),
		wasmtest.Section(4, []byte{1, 0x70, 1, 2, 4}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("identity", 0, 0), wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(identity), wasmtest.Code(caller))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
		t.Fatal(err)
	}
	return m
}

func nativeTableCallCodeRV64(t *testing.T, m *wasm.Module, vector bool) uintptr {
	t.Helper()
	ft, _ := m.LocalFuncType(1)
	mode := emissionMode{production: true, interruptible: true, functionIndex: 1, entryPC: uint32(m.Code[1].LocalDeclBytes), foreignTableCalls: true}
	var code []byte
	var err error
	if vector {
		types, functions := make([]*wasm.CompType, len(m.Types)), make([]*wasm.CompType, len(m.Code))
		keys := make([]uint64, len(types))
		for index := range types {
			types[index] = &m.Types[index].SubTypes[0].Comp
			var ok bool
			keys[index], ok = m.StructuralTypeKeyChecked(uint32(index))
			if !ok {
				t.Fatal("private immutable type key")
			}
		}
		for index := range functions {
			functions[index], _ = m.LocalFuncType(index)
		}
		code, _, _, err = compileSWARPairsWithIndirectKeys(&m.Code[1], ft, nil, types, functions, nil, 0, &m.Tables[0].Type, keys, mode)
	} else {
		state, stateErr := admitStateMode(m, true)
		if stateErr != nil {
			t.Fatal(stateErr)
		}
		summary, summaryErr := walkInteger(&m.Code[1], ft, &state, nil)
		if summaryErr != nil {
			t.Fatal(summaryErr)
		}
		code, _, _, err = compileIntegerMode(&m.Code[1], ft, &state, summary, mode)
	}
	if err != nil {
		t.Fatal(err)
	}
	return thunkImageRV64(t, code)
}

// nativeTable64IndirectCodeRV64 deliberately omits the source-profile
// table.set prefix. It is a trusted emitter probe for call_indirect's own i64
// spill load and unsigned bounds guard; public admission remains exact.
func nativeTable64IndirectCodeRV64(t *testing.T, m *wasm.Module) uintptr {
	t.Helper()
	specs := make([]functionSpec, len(m.Types))
	for i := range specs {
		ft := &m.Types[i].SubTypes[0].Comp
		specs[i] = functionSpec{signature: ft, params: make([]bool, len(ft.Params)), results: make([]bool, len(ft.Results))}
		var ok bool
		specs[i].key, ok = m.StructuralTypeKeyChecked(uint32(i))
		if !ok {
			t.Fatal("table64 indirect immutable type key")
		}
		for j, typ := range ft.Params {
			specs[i].params[j] = scalarWide(typ)
		}
		for j, typ := range ft.Results {
			specs[i].results[j] = scalarWide(typ)
		}
	}
	state := moduleState{production: true, foreignTableCalls: true, ownedTable: true, table64: true, functions: specs, types: specs}
	fn := wasm.Func{BodyBytes: []byte{0x20, 0, 0x11, 0, 0, 0x0b}}
	ft := specs[1].signature
	summary, err := walkInteger(&fn, ft, &state, nil)
	if err != nil {
		t.Fatal(err)
	}
	mode := emissionMode{production: true, interruptible: true, foreignTableCalls: true, functionIndex: 1}
	code, _, _, err := compileIntegerMode(&fn, ft, &state, summary, mode)
	if err != nil {
		t.Fatal(err)
	}
	return thunkImageRV64(t, code)
}

func nativeTable64TargetCodeRV64(t *testing.T, m *wasm.Module) uintptr {
	t.Helper()
	state := moduleState{production: true}
	ft := &m.Types[0].SubTypes[0].Comp
	summary, err := walkInteger(&m.Code[0], ft, &state, nil)
	if err != nil {
		t.Fatal(err)
	}
	code, _, _, err := compileIntegerMode(&m.Code[0], ft, &state, summary, emissionMode{production: true, interruptible: true, functionIndex: 0})
	if err != nil {
		t.Fatal(err)
	}
	return thunkImageRV64(t, code)
}

func nativeTableCallCellsRV64(types []wasm.ValType) (input, want []uint64) {
	for index, typ := range types {
		switch typ {
		case wasm.I32:
			input = append(input, 0xfeedface80000001+uint64(index))
			want = append(want, uint64(uint32(input[len(input)-1])))
		case wasm.I64:
			input = append(input, 0xffedcba987654321+uint64(index))
			want = append(want, input[len(input)-1])
		case wasm.F32:
			input = append(input, 0xdeadbeef7f800123+uint64(index))
			want = append(want, uint64(uint32(input[len(input)-1])))
		case wasm.F64:
			input = append(input, 0x7ff0000000000001+uint64(index))
			want = append(want, input[len(input)-1])
		case wasm.V128:
			input = append(input, 0x7ff0000000000000+uint64(index), 0x80000000ff800100+uint64(index))
			want = append(want, input[len(input)-2:]...)
		}
	}
	return input, want
}

func TestPrivateNativeTableRV64ActualScalarAndSWARIndirectCalls(t *testing.T) {
	for _, shape := range []string{"scalar4", "scalar64", "vector2", "mixed128", "vector128"} {
		for _, shared := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-shared%t", shape, shared), func(t *testing.T) {
				vector := shape != "scalar4" && shape != "scalar64"
				count := 128
				if shape == "scalar4" {
					count = 4
				} else if shape == "scalar64" {
					count = 64
				} else if shape == "vector2" {
					count = 2
				} else if shape == "mixed128" {
					count = 127 // plus the index fits128 logical locals
				}
				types := make([]wasm.ValType, count)
				for index := range types {
					types[index] = []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}[index%4]
					if shape == "mixed128" {
						types[index] = []wasm.ValType{wasm.V128, wasm.I32, wasm.F32, wasm.I64, wasm.F64}[index%5]
					} else if vector {
						types[index] = wasm.V128
					}
				}
				m := nativeTableCallModuleRV64(t, types)
				f := nativeTableFixtureRV64(t, shared)
				producer, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true})
				if err != nil {
					t.Fatal(err)
				}
				producerBase := thunkImageRV64(t, producer.Code)
				d := nativeTableDescriptorRV64(f, producerBase+uintptr(producer.Entry[0]))
				key, ok := m.StructuralTypeKeyChecked(0)
				if !ok {
					t.Fatal("native indirect immutable key")
				}
				binary.LittleEndian.PutUint64(d[8:], key)
				ar, err := coreruntime.NewArena(8 + 4*coreruntime.TableEntryBytes)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = ar.Close() })
				table := ar.Alloc(8 + 4*coreruntime.TableEntryBytes)
				binary.LittleEndian.PutUint32(table, 2)
				binary.LittleEndian.PutUint32(table[4:], 4)
				copy(table[8:], d[:coreruntime.TableEntryBytes])
				binary.LittleEndian.PutUint64(f.callerContext[8:], uint64(thunkPointerRV64(table)))
				f.bindCaller(t)
				f.caller.SetRV64NativeContext(thunkPointerRV64(f.callerContext))
				entry := nativeTableCallCodeRV64(t, m, vector)
				input, want := nativeTableCallCellsRV64(types)
				if shape == "vector128" {
					input = input[:254]
					want[254], want[255] = 0x7ff000000000007f, 0x80000000ff80017f
				}
				input = append(input, 0xfeedface00000000) // raw dirty i32 table index
				for attempt := 0; attempt < 3; attempt++ {
					for index, cell := range input {
						binary.LittleEndian.PutUint64(f.args[index*8:], cell)
					}
					for index := range f.results {
						f.results[index] = 0xa5
					}
					if err := f.e.CallPrepared(entry, f.args, f.caller.LinMemBase(), f.trap, f.results); err != nil {
						t.Fatal("genuine foreign call_indirect", err)
					}
					for index, cell := range want {
						if got := binary.LittleEndian.Uint64(f.results[index*8:]); got != cell {
							t.Fatal("genuine foreign indirect raw cell", index, got, cell)
						}
					}
					if !bytes.Equal(f.results[len(want)*8:], bytes.Repeat([]byte{0xa5}, len(f.results)-len(want)*8)) {
						t.Fatal("genuine indirect result tail")
					}
					f.assertCaller(t)
				}
				for _, fault := range []string{"null", "key", "canonical-entry", "canonical-home", "index"} {
					beforeTable := bytes.Clone(table)
					switch fault {
					case "null":
						binary.LittleEndian.PutUint64(table[8:], 0)
					case "key":
						binary.LittleEndian.PutUint64(table[16:], key^1)
					case "canonical-entry":
						binary.LittleEndian.PutUint64(table[8:], uint64(producerBase+uintptr(producer.Entry[1])))
					case "canonical-home":
						binary.LittleEndian.PutUint64(table[24:], binary.LittleEndian.Uint64(d[16:])^8)
					}
					for index, cell := range input {
						binary.LittleEndian.PutUint64(f.args[index*8:], cell)
					}
					if fault == "index" {
						binary.LittleEndian.PutUint64(f.args[(len(input)-1)*8:], 0xfeedface00000002)
					}
					beforeArgs := bytes.Clone(f.args)
					for index := range f.results {
						f.results[index] = 0xa5
					}
					err := f.e.CallPrepared(entry, f.args, f.caller.LinMemBase(), f.trap, f.results)
					wantCode := coreruntime.TrapIndirectOutOfBounds
					if fault == "key" {
						wantCode = coreruntime.TrapIndirectWrongSig
					}
					wantPC := m.Code[1].LocalDeclBytes + uint32(len(m.Code[1].BodyBytes)-4)
					var trap *coreruntime.TrapError
					if !errors.As(err, &trap) || trap.Code != wantCode || len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != 1 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != wantPC || !bytes.Equal(beforeArgs, f.args) || !bytes.Equal(f.results, bytes.Repeat([]byte{0xa5}, len(f.results))) {
						t.Fatal("genuine indirect guard before root writes", fault, err)
					}
					copy(table, beforeTable)
					f.assertCaller(t)
				}
			})
		}
	}
}

func TestPrivateNativeTable64RV64IndirectWideBoundsPrewriteAndReuse(t *testing.T) {
	m := ownedTable64IndirectProfile()
	f := nativeTableFixtureRV64(t, false)
	d := nativeTableDescriptorRV64(f, nativeTable64TargetCodeRV64(t, m))
	key, ok := m.StructuralTypeKeyChecked(0)
	if !ok {
		t.Fatal("table64 indirect target key")
	}
	binary.LittleEndian.PutUint64(d[8:], key)
	ar, err := coreruntime.NewArena(8 + 2*coreruntime.TableEntryBytes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ar.Close() })
	table := ar.Alloc(8 + 2*coreruntime.TableEntryBytes)
	binary.LittleEndian.PutUint32(table, 2)
	binary.LittleEndian.PutUint32(table[4:], 2)
	copy(table[8:], d[:coreruntime.TableEntryBytes])
	beforeTable := bytes.Clone(table)
	binary.LittleEndian.PutUint64(f.callerContext[8:], uint64(thunkPointerRV64(table)))
	f.bindCaller(t)
	f.caller.SetRV64NativeContext(thunkPointerRV64(f.callerContext))
	entry := nativeTable64IndirectCodeRV64(t, m)

	runLow := func() {
		binary.LittleEndian.PutUint64(f.args, 0)
		for i := range f.results {
			f.results[i] = 0xa5
		}
		if err := f.e.CallPrepared(entry, f.args, f.caller.LinMemBase(), f.trap, f.results); err != nil || binary.LittleEndian.Uint64(f.results) != 7 || !bytes.Equal(f.results[8:], bytes.Repeat([]byte{0xa5}, len(f.results)-8)) || binary.LittleEndian.Uint64(f.args) != 0 || !bytes.Equal(table, beforeTable) {
			t.Fatal("table64 indirect low-index execution/reuse", err)
		}
		f.assertCaller(t)
	}
	runLow()
	for _, index := range []uint64{1 << 32, ^uint64(0)} {
		binary.LittleEndian.PutUint64(f.args, index)
		beforeArgs := bytes.Clone(f.args)
		for i := range f.results {
			f.results[i] = 0xa5
		}
		err := f.e.CallPrepared(entry, f.args, f.caller.LinMemBase(), f.trap, f.results)
		var trap *coreruntime.TrapError
		if !errors.As(err, &trap) || trap.Code != coreruntime.TrapIndirectOutOfBounds || len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != 1 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 2 || !bytes.Equal(f.args, beforeArgs) || !bytes.Equal(f.results, bytes.Repeat([]byte{0xa5}, len(f.results))) || !bytes.Equal(table, beforeTable) {
			t.Fatal("table64 indirect wide guard before transport writes", index, err)
		}
		clear(f.trap)
		f.assertCaller(t)
		runLow()
	}
}
