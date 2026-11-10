//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func swarGlobalCellsRV64(t *testing.T, j *coreruntime.JobMemory, class byte, count int) ([]byte, []byte) {
	t.Helper()
	width := 8
	if class == 0x7b {
		width = 16
	}
	ar, err := coreruntime.NewArena(count*8 + width + 8)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := ar.Close(); err != nil {
			t.Error(err)
		}
	})
	dir, cell := ar.Alloc(count*8), ar.Alloc(width+8)
	for i := 0; i < count; i++ {
		binary.LittleEndian.PutUint64(dir[i*8:], uint64(uintptr(unsafe.Pointer(&cell[0]))))
	}
	for i := width; i < len(cell); i++ {
		cell[i] = 0xa5
	}
	j.SetGlobalsPtr(uintptr(unsafe.Pointer(&dir[0])))
	return dir, cell
}

func TestSWARRV64TypedGlobalsAliasingRawBitsAndNarrowCells(t *testing.T) {
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	for _, class := range []byte{0x7f, 0x7e, 0x7d, 0x7c, 0x7b} {
		for _, index := range []uint32{1, 255} {
			fn, ft, globals := swarValidatedGlobalsFunction(t, swarGlobalFixture(class, index), class, int(index)+1)
			code, info, _, err := compileSWARPairsWithGlobals(fn, ft, nil, nil, nil, globals, emissionMode{production: true})
			if err != nil || info.FrameBytes <= 2048 {
				t.Fatal("large global frame", class, info, err)
			}
			entry := thunkImageRV64(t, code)
			dir, cell := swarGlobalCellsRV64(t, j, class, len(globals))
			originalDirectory := bytes.Clone(dir)
			for _, pair := range dirtyInputs() {
				binary.LittleEndian.PutUint64(cell, pair[0])
				if class == 0x7b {
					binary.LittleEndian.PutUint64(cell[8:], pair[1])
				}
				input := [4]uint64{0x7ff0000000000001, 0x80000000deadbeef, ^pair[0], ^pair[1]}
				for i, value := range input {
					binary.LittleEndian.PutUint64(args[i*8:], value)
				}
				for i := range results {
					results[i] = 0xa5
				}
				if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
					t.Fatal(err)
				}
				old, current := pair[0], input[2]
				if class == 0x7f || class == 0x7d {
					old, current = uint64(uint32(old)), uint64(uint32(current))
				}
				want := []uint64{input[0], input[1], old, current}
				width := 8
				if class == 0x7b {
					want = []uint64{input[0], input[1], old, pair[1], current, input[3]}
					width = 16
				}
				for i, value := range want {
					if binary.LittleEndian.Uint64(results[i*8:]) != value {
						t.Fatal("global mutation changed old live value or raw/narrow carrier", class, index, pair, i, results[:len(want)*8], want)
					}
				}
				if binary.LittleEndian.Uint64(cell) != current || class == 0x7b && binary.LittleEndian.Uint64(cell[8:]) != input[3] || !bytes.Equal(cell[width:], bytes.Repeat([]byte{0xa5}, 8)) || !bytes.Equal(dir, originalDirectory) || !bytes.Equal(results[len(want)*8:], bytes.Repeat([]byte{0xa5}, len(results)-len(want)*8)) {
					t.Fatal("global cell width/directory/result tail", class, index, pair)
				}
				for i, value := range input {
					if binary.LittleEndian.Uint64(args[i*8:]) != value {
						t.Fatal("global access mutated argument arena", class, i)
					}
				}
			}
			if allocs := testing.AllocsPerRun(50, func() {
				if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
					panic(err)
				}
			}); allocs != 0 {
				t.Fatal("typed global warm allocations", class, index, allocs)
			}
		}
	}
}

func TestSWARRV64TypedGlobalsReloadAcrossMixedDirectCall(t *testing.T) {
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	for _, class := range []byte{0x7f, 0x7e, 0x7d, 0x7c, 0x7b} {
		// Keep both a vector and an old global value live below a call which
		// mutates an aliased cell; the following get must see the fresh value.
		caller := fixtureFunc{params: []byte{0x7b, class}, results: []byte{0x7b, class, class}, body: []byte{0x20, 0, 0x23, 1, 0x20, 1, 0x10, 1, 0x23, 1, 0x0b}}
		leaf := fixtureFunc{params: []byte{class}, body: []byte{0x20, 0, 0x24, 0, 0x0b}}
		// Resolve the caller's forward target through a genuinely frontend-
		// validated two-function module, not the one-function fixture helper.
		data := swarGlobalSource(leaf, class, 2)
		data = modifySection(data, 1, 1, []byte{2, 0x60, 2, 0x7b, class, 3, 0x7b, class, class, 0x60, 1, class, 0}, true)
		data = modifySection(data, 3, 3, []byte{2, 0, 1}, true)
		bodies := []byte{2}
		for _, f := range []fixtureFunc{caller, leaf} {
			body := append([]byte{0}, f.body...)
			bodies = append(uleb(bodies, uint32(len(body))), body...)
		}
		data = modifySection(data, 10, 10, bodies, true)
		m, err := wasm.DecodeModule(data)
		if err != nil {
			t.Fatal(err)
		}
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		globals := make([]wasm.GlobalType, len(m.Globals))
		for i := range globals {
			globals[i] = m.Globals[i].Type
		}
		callerType, _ := m.LocalFuncType(0)
		leafType, _ := m.LocalFuncType(1)
		directory := []*wasm.CompType{callerType, leafType}
		var code []byte
		var infos [2]FunctionInfo
		var calls []directCall
		for i, ft := range directory {
			body, info, relocations, err := compileSWARPairsWithGlobals(&m.Code[i], ft, nil, directory, directory, globals, emissionMode{production: true, functionIndex: uint32(i)})
			if err != nil {
				t.Fatal(err)
			}
			for len(code)&15 != 0 {
				code = append(code, 0x13, 0, 0, 0)
			}
			info.Entry = len(code)
			infos[i] = info
			for j := range relocations {
				relocations[j].site += info.Entry
			}
			calls = append(calls, relocations...)
			code = append(code, body...)
		}
		a := enc.Asm{B: code}
		for _, call := range calls {
			if err := a.PatchLong(call.site, int64(infos[call.callee].Entry)); err != nil {
				t.Fatal(err)
			}
		}
		entry := thunkImageRV64(t, code)
		_, cell := swarGlobalCellsRV64(t, j, class, 2)
		binary.LittleEndian.PutUint64(cell, 0xdeadbeef80000001)
		if class == 0x7b {
			binary.LittleEndian.PutUint64(cell[8:], 0xfff0000000000001)
		}
		input := [4]uint64{0x7ff0000000000001, 0x80000000deadbeef, 0xabcdef12fedc80ff, 0x12345678ffffffff}
		for i, value := range input {
			binary.LittleEndian.PutUint64(args[i*8:], value)
		}
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
			t.Fatal(err)
		}
		old, current := uint64(0xdeadbeef80000001), input[2]
		if class == 0x7f || class == 0x7d {
			old, current = uint64(uint32(old)), uint64(uint32(current))
		}
		want := []uint64{input[0], input[1], old, current}
		if class == 0x7b {
			want = []uint64{input[0], input[1], old, 0xfff0000000000001, current, input[3]}
		}
		for i, value := range want {
			if binary.LittleEndian.Uint64(results[i*8:]) != value {
				t.Fatal("global reload/caller old home across call", class, i, results[:len(want)*8], want)
			}
		}
	}
}
