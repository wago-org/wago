//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func swarTableStateImageRV64(t *testing.T, source []byte) (*swarModuleImage, *coreruntime.CodeBuffer) {
	t.Helper()
	image, err := compileSWARTableModule(source, emissionMode{production: true, interruptible: true})
	if err != nil {
		t.Fatal(err)
	}
	return image, swarTableCodeBuffer(t, image)
}

func swarTableStateOwnerRV64(t *testing.T, image *swarModuleImage, code *coreruntime.CodeBuffer, job *coreruntime.JobMemory) *swarTableOwner {
	t.Helper()
	owner, err := image.instantiateTables(job, code)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := owner.Close(); err != nil {
			t.Error(err)
		}
	})
	return owner
}

// Genuine native execution through Seal's final executable mapping. Type0 is
// reused by functions1/2, type1 belongs to function0, and function2 calls1 via
// a relocated backward direct call. The default/public gates remain closed.
func TestSWARRV64TableStateSealedMixedIndirectTypeReuseAndSharedCode(t *testing.T) {
	caller, leaf := swarIndirectFixtures()
	caller.body = bytes.ReplaceAll(caller.body, []byte{0x11, 1, 0}, []byte{0x11, 0, 0})
	forward := leaf
	forward.body = nil
	for i := range leaf.params {
		forward.body = append(forward.body, 0x20, byte(i))
	}
	forward.body = append(forward.body, 0x10, 1, 0x0b)
	source := swarTableStateSource([]fixtureFunc{caller, leaf, forward}, 1024, 1024, []wasm.Elem{
		swarStateElement(wasm.ElemActive, 0, 1), swarStateElement(wasm.ElemActive, 1023, 2),
		swarStateElement(wasm.ElemPassive, 0, 2, 1), swarStateElement(wasm.ElemDeclarative, 0, 0, 1, 2),
	})
	types := []byte{2}
	for _, f := range []fixtureFunc{leaf, caller} {
		types = append(types, 0x60)
		types = append(uleb(types, uint32(len(f.params))), f.params...)
		types = append(uleb(types, uint32(len(f.results))), f.results...)
	}
	source = modifySection(source, 1, 1, types, true)
	source = modifySection(source, 3, 3, []byte{3, 1, 0, 0}, true)
	image, code := swarTableStateImageRV64(t, source)
	e1, j1, args1, results1, trap1, _ := thunkFixtureRV64(t, 256)
	e2, j2, args2, results2, trap2, _ := thunkFixtureRV64(t, 256)
	o1 := swarTableStateOwnerRV64(t, image, code, j1)
	o2 := swarTableStateOwnerRV64(t, image, code, j2)
	entry := code.Base() + uintptr(image.functions[0].Entry)
	table1, table2 := swarTableOwnedBytes(o1.table, 8+1024*32), swarTableOwnedBytes(o2.table, 8+1024*32)
	dir1, dir2 := swarTableOwnedBytes(o1.functions, 4*40), swarTableOwnedBytes(o2.functions, 4*40)
	if image.functionKeys[0] == image.functionKeys[1] || image.functionKeys[1] != image.functionKeys[2] || binary.LittleEndian.Uint64(dir1[32:]) == binary.LittleEndian.Uint64(dir2[32:]) || code.Base() == 0 || image.functions[2].DirectCalls != 1 {
		t.Fatal("canonical type reuse/private contexts/direct relocation")
	}
	run := func(e *coreruntime.Engine, j *coreruntime.JobMemory, args, results, trap []byte, index uint32, pair [2]uint64) {
		input := [9]uint64{pair[0], 0x7ff0000000000001, pair[1], pair[1], pair[0], ^pair[1], ^pair[0], 0x8000000000000000, dirty(index)}
		for i, v := range input {
			binary.LittleEndian.PutUint64(args[i*8:], v)
		}
		for i := range results {
			results[i] = 0xa5
		}
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
			panic(err)
		}
		want := [10]uint64{input[5], input[6], input[1] ^ input[5], input[2] ^ input[6], uint64(uint32(input[0])), input[7], input[5], input[6], uint64(uint32(input[3])), input[4]}
		for i, v := range want {
			if got := binary.LittleEndian.Uint64(results[i*8:]); got != v {
				t.Fatal("sealed mixed call raw/narrow/large-frame value", index, i, got, v)
			}
		}
		if !bytes.Equal(results[80:], bytes.Repeat([]byte{0xa5}, len(results)-80)) || binary.LittleEndian.Uint64(args[64:]) != input[8] {
			t.Fatal("sealed mixed call tail/dirty index")
		}
	}
	for _, pair := range dirtyInputs() {
		for _, index := range []uint32{0, 1023} {
			run(e1, j1, args1, results1, trap1, index, pair)
			run(e2, j2, args2, results2, trap2, index, pair)
		}
	}
	// A foreign complete row remains invalid even if its home field is forged to
	// the receiving job. Canonical context identity is checked before arg writes.
	old := bytes.Clone(table2[8:40])
	for _, bad := range []string{"foreign", "foreign-home", "wrong-signature", "null", "bounds"} {
		copy(table2[8:40], old)
		index, want := uint32(0), coreruntime.TrapIndirectOutOfBounds
		switch bad {
		case "foreign", "foreign-home":
			copy(table2[8:40], table1[8:40])
			if bad == "foreign-home" {
				copy(table2[24:32], old[16:24])
			}
		case "wrong-signature":
			copy(table2[8:40], dir2[40:72])
			want = coreruntime.TrapIndirectWrongSig
		case "null":
			clear(table2[8:40])
		case "bounds":
			index = 1024
		}
		for i := range args2 {
			args2[i] = byte(i*37) ^ 0x5a
		}
		binary.LittleEndian.PutUint64(args2[64:], dirty(index))
		before, rows := bytes.Clone(args2), bytes.Clone(table2)
		for i := range results2 {
			results2[i] = 0xa5
		}
		err := e2.CallPrepared(entry, args2, j2.LinMemBase(), trap2, results2)
		var tr *coreruntime.TrapError
		pc := uint32(3 + len(caller.body) - 4)
		if !errors.As(err, &tr) || tr.Code != want || binary.LittleEndian.Uint64(trap2[16:]) != uint64(pc)<<32|1 || !bytes.Equal(args2, before) || !bytes.Equal(table2, rows) || !bytes.Equal(results2, bytes.Repeat([]byte{0xa5}, len(results2))) {
			t.Fatal("sealed owner guard/prewrite/source", bad, err)
		}
		clear(trap2)
	}
	copy(table2[8:40], old)
	if err := o1.Close(); err != nil || code.Base() != entry-uintptr(image.functions[0].Entry) {
		t.Fatal("first owner closed shared code", err)
	}
	run(e2, j2, args2, results2, trap2, 1023, [2]uint64{0xffffffff80000000, 0xfff00000deadbeef})
	o1 = swarTableStateOwnerRV64(t, image, code, j1)
	run(e1, j1, args1, results1, trap1, 0, [2]uint64{0xffffffff80000000, 0xfff00000deadbeef})
	// Keep validation outside the allocation measurement; real calls use sealed
	// shared code and both owner-specific sets of descriptors.
	if allocs := testing.AllocsPerRun(50, func() {
		binary.LittleEndian.PutUint64(args1[64:], dirty(1023))
		if err := e1.CallPrepared(entry, args1, j1.LinMemBase(), trap1, results1); err != nil {
			panic(err)
		}
	}); allocs != 0 {
		t.Fatal("sealed table owner warm allocations", allocs)
	}
}

func TestSWARRV64TableStateElement255DropInitAndFreshOwner(t *testing.T) {
	f := fixtureFunc{params: []byte{0x7b, 0x7f, 0x7f, 0x7f, 0x7f}, results: []byte{0x7b, 0x7f}, locals: []byte{1, 123, 0x7b}, body: []byte{0x20, 0, 0x20, 4, 4, 0x40, 0xfc, 13, 0xff, 1, 0x0b, 0x20, 1, 0x20, 2, 0x20, 3, 0xfc, 12, 0xff, 1, 0, 0xfc, 16, 0, 0x0b}}
	leaf := fixtureFunc{params: []byte{0x7f}, result: 0x7f, body: []byte{0x20, 0, 0x0b}, unexported: true}
	elements := make([]wasm.Elem, 256)
	for i := range elements {
		elements[i] = swarStateElement(wasm.ElemPassive, 0, 1, 0, 1, 1)
	}
	elements[0] = swarStateElement(wasm.ElemActive, 1, 1)
	elements[17] = swarStateElement(wasm.ElemDeclarative, 0, 0, 1)
	elements[254] = swarStateElement(wasm.ElemPassive, 0)
	image, code := swarTableStateImageRV64(t, swarTableStateSource([]fixtureFunc{f, leaf}, 4, 8, elements))
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	owner := swarTableStateOwnerRV64(t, image, code, j)
	entry := code.Base() + uintptr(image.functions[0].Entry)
	verify := func(owner *swarTableOwner) {
		table, dir := swarTableOwnedBytes(owner.table, 8+8*32), swarTableOwnedBytes(owner.functions, 3*40)
		desc := swarTableOwnedBytes(owner.elements, 256*16)
		before := bytes.Clone(desc)
		input := swarTableInputsRV64(args, 0, 0, 4, 0)
		for i := range results {
			results[i] = 0xa5
		}
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(args, input) || !bytes.Equal(results[:16], input[:16]) || binary.LittleEndian.Uint64(results[16:]) != 4 || !bytes.Equal(results[24:], bytes.Repeat([]byte{0xa5}, len(results)-24)) || !bytes.Equal(desc, before) {
			t.Fatal("owned element255 init/raw/tails/descriptors", err)
		}
		for n, index := range []int{1, 0, 1, 1} {
			if !bytes.Equal(table[8+n*32:8+(n+1)*32], dir[(index+1)*40:(index+1)*40+32]) {
				t.Fatal("owned element255 canonical row", n)
			}
		}
		if !bytes.Equal(table[8+4*32:], make([]byte, 4*32)) {
			t.Fatal("element writes spare rows")
		}
		if n := testing.AllocsPerRun(50, func() {
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
				panic(err)
			}
		}); n != 0 {
			t.Fatal("owned positive element init warm allocations", n)
		}
		oldTable := bytes.Clone(table)
		swarTableInputsRV64(args, 4, 0, 0, 1)
		binary.LittleEndian.PutUint32(before[255*16+8:], 0)
		for round := 0; round < 2; round++ {
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(desc, before) || !bytes.Equal(table, oldTable) {
				t.Fatal("owned drop idempotence/pointer/neighbor state", err)
			}
		}
		swarTableInputsRV64(args, 0, 0, 1, 0)
		for i := range results {
			results[i] = 0xa5
		}
		err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
		var tr *coreruntime.TrapError
		pc := uint32(3 + bytes.Index(f.body, []byte{0xfc, 12}))
		if !errors.As(err, &tr) || tr.Code != coreruntime.TrapTableOutOfBounds || binary.LittleEndian.Uint64(trap[16:]) != uint64(pc)<<32|1 || !bytes.Equal(table, oldTable) || !bytes.Equal(desc, before) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
			t.Fatal("owned dropped init cold/source/prewrite", err)
		}
		clear(trap)
	}
	verify(owner)
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	owner = swarTableStateOwnerRV64(t, image, code, j)
	desc := swarTableOwnedBytes(owner.elements, 256*16)
	for _, index := range []int{0, 17, 254} {
		if !bytes.Equal(desc[index*16:(index+1)*16], make([]byte, 16)) {
			t.Fatal("active/declarative/empty initial state", index)
		}
	}
	if binary.LittleEndian.Uint32(desc[255*16+8:]) != 4 {
		t.Fatal("fresh owner inherited drop")
	}
	verify(owner)
}

func TestSWARRV64TableStateEmptyCapacityGrowAndIndirectTrap(t *testing.T) {
	grow := fixtureFunc{params: []byte{0x7b, 0x7f}, results: []byte{0x7b, 0x7f, 0x7f}, body: []byte{0x20, 0, 0xd0, 0x70, 0x20, 1, 0xfc, 15, 0, 0xfc, 16, 0, 0x0b}}
	indirect := fixtureFunc{params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x41, 0, 0x11, 1, 0, 0x0b}, unexported: true}
	image, code := swarTableStateImageRV64(t, swarTableStateSource([]fixtureFunc{grow, indirect}, 0, 0, nil))
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	owner := swarTableStateOwnerRV64(t, image, code, j)
	table := swarTableOwnedBytes(owner.table, 8)
	if !bytes.Equal(table, make([]byte, 8)) || owner.elements != 0 {
		t.Fatal("zero-capacity header/directory")
	}
	for _, delta := range []uint32{0, 1, 0xffffffff, 0} {
		binary.LittleEndian.PutUint64(args, 0x7ff0000000000001)
		binary.LittleEndian.PutUint64(args[8:], 0x80000000deadbeef)
		binary.LittleEndian.PutUint64(args[16:], dirty(delta))
		for i := range results {
			results[i] = 0xa5
		}
		want := uint64(0xffffffff)
		if delta == 0 {
			want = 0
		}
		if err := e.CallPrepared(code.Base()+uintptr(image.functions[0].Entry), args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:16], args[:16]) || binary.LittleEndian.Uint64(results[16:]) != want || binary.LittleEndian.Uint64(results[24:]) != 0 || !bytes.Equal(table, make([]byte, 8)) || !bytes.Equal(results[32:], bytes.Repeat([]byte{0xa5}, len(results)-32)) {
			t.Fatal("zero-capacity grow/full32delta/size/tail", delta, err)
		}
	}
	before := bytes.Clone(args)
	for i := range results {
		results[i] = 0xa5
	}
	err := e.CallPrepared(code.Base()+uintptr(image.functions[1].Entry), args, j.LinMemBase(), trap, results)
	var tr *coreruntime.TrapError
	pc := uint32(1 + bytes.Index(indirect.body, []byte{0x11, 1, 0}))
	if !errors.As(err, &tr) || tr.Code != coreruntime.TrapIndirectOutOfBounds || binary.LittleEndian.Uint64(trap[16:]) != uint64(pc)<<32|2 || !bytes.Equal(args, before) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
		t.Fatal("empty table indirect source/prewrite", err)
	}
}
