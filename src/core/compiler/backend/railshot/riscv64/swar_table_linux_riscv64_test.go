//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func swarOwnedTableFixtureRV64(t *testing.T, j *coreruntime.JobMemory, entry uintptr, fixture fixtureFunc) ([]byte, []byte) {
	t.Helper()
	m, err := wasm.DecodeModule(swarTableSource(fixture))
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 128}); err != nil {
		t.Fatal(err)
	}
	return swarOwnedTableModuleRV64(t, j, entry, m)
}

func swarOwnedTableModuleRV64(t *testing.T, j *coreruntime.JobMemory, entry uintptr, m *wasm.Module) ([]byte, []byte) {
	t.Helper()
	functions := make([]*wasm.CompType, len(m.Code))
	for i := range functions {
		functions[i], _ = m.LocalFuncType(i)
	}
	leafCode, _, _, err := compileSWARPairsWithCalls(&m.Code[1], functions[1], nil, nil, functions, emissionMode{production: true, functionIndex: 1})
	if err != nil {
		t.Fatal(err)
	}
	entries := [2]uintptr{entry, thunkImageRV64(t, leafCode)}
	ar, err := coreruntime.NewArena(8 + 1024*32 + 3*40 + 8)
	if err != nil {
		t.Fatal(err)
	}
	table, directory, context := ar.Alloc(8+1024*32), ar.Alloc(3*40), ar.Alloc(8)
	contextBits := uint64(uintptr(unsafe.Pointer(&context[0])))
	binary.LittleEndian.PutUint32(table, 1024)
	binary.LittleEndian.PutUint32(table[4:], 0x5aa55aa5)
	binary.LittleEndian.PutUint64(directory[32:], contextBits)
	for index, entry := range entries {
		desc := directory[(index+1)*40:]
		binary.LittleEndian.PutUint64(desc, uint64(entry))
		key, ok := m.StructuralTypeKeyChecked(uint32(index))
		if !ok {
			t.Fatal("canonical function type key", index)
		}
		binary.LittleEndian.PutUint64(desc[8:], key)
		binary.LittleEndian.PutUint64(desc[16:], uint64(j.LinMemBase())|uint64(abi.FuncRefLocalWrapperHomeTag))
		binary.LittleEndian.PutUint64(desc[24:], uint64(uintptr(unsafe.Pointer(&desc[0]))))
		binary.LittleEndian.PutUint64(desc[32:], contextBits)
	}
	j.SetTablePtr(thunkPointerRV64(table))
	j.SetFuncRefDesc(thunkPointerRV64(directory))
	t.Cleanup(func() {
		j.SetTablePtr(0)
		j.SetFuncRefDesc(0)
		if err := ar.Close(); err != nil {
			t.Error(err)
		}
	})
	return table, directory
}

func TestSWARRV64OwnedReferencesTypedSelectLocalsAndTableCells(t *testing.T) {
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	f := swarTableFixture()
	fn, ft, functions, tableType := swarValidatedTableFunction(t, f)
	code, info, _, err := compileSWARPairsWithTable(fn, ft, nil, nil, functions, nil, 0, tableType, emissionMode{production: true})
	if err != nil || info.FrameBytes <= 2048 {
		t.Fatal("mixed reference frame", err)
	}
	entry := thunkImageRV64(t, code)
	table, directory := swarOwnedTableFixtureRV64(t, j, entry, f)
	for _, oldNull := range []bool{false, true} {
		for _, index := range []uint32{0, 1, 1023, 1024, 0xffffffff} {
			for _, condition := range []uint32{0, 1, 0x80000000, 0xffffffff} {
				clear(table[8:])
				if !oldNull {
					row := index
					if row >= 1024 {
						row = 0
					}
					copy(table[8+int(row)*32:], directory[40:72])
				}
				before := bytes.Clone(table)
				input := [4]uint64{0x7ff0000000000001, 0x80000000deadbeef, dirty(index), dirty(condition)}
				for i, v := range input {
					binary.LittleEndian.PutUint64(args[i*8:], v)
				}
				for i := range results {
					results[i] = 0xa5
				}
				err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
				if index >= 1024 {
					var tr *coreruntime.TrapError
					if !errors.As(err, &tr) || tr.Code != coreruntime.TrapTableOutOfBounds || binary.LittleEndian.Uint64(trap[16:]) != uint64(fn.LocalDeclBytes+4)<<32|1 || !bytes.Equal(table, before) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
						t.Fatal("table bounds/source before writes", index, condition, err)
					}
					clear(trap)
					continue
				}
				oldFlag, newFlag := uint64(0), uint64(0)
				if oldNull {
					oldFlag = 1
				}
				if condition == 0 {
					newFlag = 1
				}
				want := []uint64{input[0], input[1], oldFlag, newFlag, newFlag}
				for i, v := range want {
					if binary.LittleEndian.Uint64(results[i*8:]) != v {
						t.Fatal("live V/oldreference/typedselect/localref/tableget", oldNull, index, condition, i, err)
					}
				}
				if err != nil || !bytes.Equal(results[len(want)*8:], bytes.Repeat([]byte{0xa5}, len(results)-len(want)*8)) {
					t.Fatal("reference result tail", err)
				}
				row := before[8+int(index)*32 : 8+(int(index)+1)*32]
				if condition == 0 {
					clear(row)
				} else {
					copy(row, directory[80:112])
				}
				if !bytes.Equal(table, before) {
					t.Fatal("canonical full32B table cell or neighboring/header writes", oldNull, index, condition)
				}
				for i, v := range input {
					if binary.LittleEndian.Uint64(args[i*8:]) != v {
						t.Fatal("reference function changed arguments", i)
					}
				}
			}
		}
	}
	binary.LittleEndian.PutUint64(args[16:], dirty(0))
	binary.LittleEndian.PutUint64(args[24:], dirty(1))
	if allocs := testing.AllocsPerRun(50, func() {
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
			panic(err)
		}
	}); allocs != 0 {
		t.Fatal("mixed reference warm allocations", allocs)
	}
	before := bytes.Clone(table)
	originalTag, originalCtx := binary.LittleEndian.Uint64(directory[96:]), binary.LittleEndian.Uint64(directory[112:])
	for _, corruption := range []string{"tag", "context", "missing-directory", "missing-table"} {
		binary.LittleEndian.PutUint64(directory[96:], originalTag)
		binary.LittleEndian.PutUint64(directory[112:], originalCtx)
		j.SetFuncRefDesc(thunkPointerRV64(directory))
		j.SetTablePtr(thunkPointerRV64(table))
		wantPC, wantTrap := fn.LocalDeclBytes+uint32(bytes.IndexByte(f.body, 0x26)), coreruntime.TrapIndirectOutOfBounds
		switch corruption {
		case "tag":
			binary.LittleEndian.PutUint64(directory[96:], originalTag^0x1000)
		case "context":
			binary.LittleEndian.PutUint64(directory[112:], originalCtx+8)
		case "missing-directory":
			j.SetFuncRefDesc(0)
			wantPC = fn.LocalDeclBytes + uint32(bytes.IndexByte(f.body, 0xd2))
		case "missing-table":
			j.SetTablePtr(0)
			wantPC = fn.LocalDeclBytes + 4
			wantTrap = coreruntime.TrapTableOutOfBounds
		}
		for i := range results {
			results[i] = 0xa5
		}
		err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
		var tr *coreruntime.TrapError
		if !errors.As(err, &tr) || tr.Code != wantTrap || binary.LittleEndian.Uint64(trap[16:]) != uint64(wantPC)<<32|1 || !bytes.Equal(table, before) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
			t.Fatal("owned reference guard/source before row writes", corruption, err, wantTrap, trap[16:24])
		}
		clear(trap)
	}
	binary.LittleEndian.PutUint64(directory[96:], originalTag)
	binary.LittleEndian.PutUint64(directory[112:], originalCtx)
	j.SetFuncRefDesc(thunkPointerRV64(directory))
	j.SetTablePtr(thunkPointerRV64(table))
	if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
		t.Fatal("reference guard reuse", err)
	}
}

func TestSWARRV64OwnedReferenceInlineIfJoinPreservesVector(t *testing.T) {
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	f := fixtureFunc{params: []byte{0x7b, 0x7f}, results: []byte{0x7b, 0x7f}, locals: []byte{1, 126, 0x7b}, body: []byte{0x20, 0, 0x20, 1, 4, 0x70, 0xd2, 1, 5, 0xd0, 0x70, 0x0b, 0xd1, 0x0b}}
	fn, ft, functions, tableType := swarValidatedTableFunction(t, f)
	code, info, _, err := compileSWARPairsWithTable(fn, ft, nil, nil, functions, nil, 0, tableType, emissionMode{production: true})
	if err != nil || info.FrameBytes <= 2048 {
		t.Fatal(err)
	}
	entry := thunkImageRV64(t, code)
	swarOwnedTableFixtureRV64(t, j, entry, f)
	for _, condition := range []uint32{0, 1, 0x80000000, 0xffffffff} {
		input := [3]uint64{0x7ff0000000000001, 0x80000000deadbeef, dirty(condition)}
		for i, v := range input {
			binary.LittleEndian.PutUint64(args[i*8:], v)
		}
		for i := range results {
			results[i] = 0xa5
		}
		want := uint64(0)
		if condition == 0 {
			want = 1
		}
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || binary.LittleEndian.Uint64(results) != input[0] || binary.LittleEndian.Uint64(results[8:]) != input[1] || binary.LittleEndian.Uint64(results[16:]) != want || !bytes.Equal(results[24:], bytes.Repeat([]byte{0xa5}, len(results)-24)) {
			t.Fatal("reference if join/live V/narrow condition", condition, err)
		}
	}
}

func TestSWARRV64OwnedReferenceSurvivesRowClearBranchAndDirectCall(t *testing.T) {
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	f := fixtureFunc{params: []byte{0x7b, 0x7f}, results: []byte{0x7b, 0x7f}, locals: []byte{2, 3, 0x70, 123, 0x7b}, body: []byte{
		0x20, 0, 0x20, 1, 0x25, 0, 0x21, 3,
		0x20, 1, 0xd0, 0x70, 0x26, 0,
		0x20, 1, 2, 0x70, 0x42, 0x7f, 0x20, 3, 0x0c, 0, 0x0b, 0x41,
	}}
	// The branch moves an actual reference down one canonical home, discarding
	// an extra I64. Both it and the destination index then stay live below a
	// genuine scalar callee call before restoring the cleared source row.
	f.body = sleb(f.body, 77)
	f.body = append(f.body, 0x10, 1, 0x1a, 0x26, 0, 0x20, 4, 0xd1, 0x0b)
	fn, ft, functions, tableType := swarValidatedTableFunction(t, f)
	code, info, calls, err := compileSWARPairsWithTable(fn, ft, nil, nil, functions, nil, 0, tableType, emissionMode{production: true})
	if err != nil || len(calls) != 1 || info.FrameBytes <= 2048 {
		t.Fatal("saved reference mixed call", err)
	}
	// Link the framed caller and leaf before executable publication, using
	// the same relative relocation envelope as the private module assembler.
	m, err := wasm.DecodeModule(swarTableSource(f))
	if err != nil {
		t.Fatal(err)
	}
	leafType, _ := m.LocalFuncType(1)
	leafCode, _, _, err := compileSWARPairsWithCalls(&m.Code[1], leafType, nil, nil, functions, emissionMode{production: true, functionIndex: 1})
	if err != nil {
		t.Fatal(err)
	}
	for len(code)&15 != 0 {
		code = append(code, 0x13, 0, 0, 0)
	}
	calleeOffset := len(code)
	code = append(code, leafCode...)
	a := enc.Asm{B: code}
	if err := a.PatchLong(calls[0].site, int64(calleeOffset)); err != nil {
		t.Fatal(err)
	}
	entry := thunkImageRV64(t, code)
	table, directory := swarOwnedTableFixtureRV64(t, j, entry, f)
	binary.LittleEndian.PutUint64(directory[40:], uint64(entry))
	binary.LittleEndian.PutUint64(directory[80:], uint64(entry)+uint64(calleeOffset))
	for _, index := range []uint32{0, 1, 1023} {
		for _, oldNull := range []bool{false, true} {
			clear(table[8:])
			if !oldNull {
				copy(table[8+int(index)*32:], directory[40:72])
			}
			before := bytes.Clone(table)
			input := [3]uint64{0x7ff0000000000001, 0x80000000deadbeef, dirty(index)}
			for i, v := range input {
				binary.LittleEndian.PutUint64(args[i*8:], v)
			}
			for i := range results {
				results[i] = 0xa5
			}
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(table, before) || binary.LittleEndian.Uint64(results) != input[0] || binary.LittleEndian.Uint64(results[8:]) != input[1] || binary.LittleEndian.Uint64(results[16:]) != 1 || !bytes.Equal(results[24:], bytes.Repeat([]byte{0xa5}, len(results)-24)) {
				t.Fatal("old handle lost across clear/branch/call or default ref local not null", index, oldNull, err)
			}
		}
	}
}
