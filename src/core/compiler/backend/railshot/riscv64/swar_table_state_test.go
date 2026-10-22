//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func swarStateElement(kind wasm.ElemModeKind, offset uint32, functions ...uint32) wasm.Elem {
	e := wasm.Elem{Mode: wasm.ElemMode{Kind: kind}, Kind: wasm.ElemKind{Kind: wasm.ElemFuncs}}
	for _, index := range functions {
		e.Kind.Funcs = append(e.Kind.Funcs, wasm.FuncIdx(index))
	}
	if kind == wasm.ElemActive {
		e.Mode.Offset.BodyBytes = append(sleb([]byte{0x41}, int64(int32(offset))), 0x0b)
	}
	return e
}

func swarTableStateSource(fixtures []fixtureFunc, minimum, maximum uint32, elements []wasm.Elem) []byte {
	source := modifySection(binaryModule(fixtures...), 7, 4, uleb(uleb([]byte{1, 0x70, 1}, minimum), maximum), false)
	payload := uleb(nil, uint32(len(elements)))
	for _, e := range elements {
		switch e.Mode.Kind {
		case wasm.ElemActive:
			payload = append(payload, 0)
			payload = append(payload, e.Mode.Offset.BodyBytes...)
		case wasm.ElemPassive:
			payload = append(payload, 1, 0)
		case wasm.ElemDeclarative:
			payload = append(payload, 3, 0)
		}
		payload = uleb(payload, uint32(len(e.Kind.Funcs)))
		for _, index := range e.Kind.Funcs {
			payload = uleb(payload, uint32(index))
		}
	}
	return modifySection(source, 10, 9, payload, false)
}

// Only owner-produced offheap addresses are converted, with bounded sizes.
func swarTableOwnedBytes(pointer uintptr, length int) []byte {
	return unsafe.Slice((*byte)(*(*unsafe.Pointer)(unsafe.Pointer(&pointer))), length)
}
func swarTableCodeBuffer(t *testing.T, image *swarModuleImage) *coreruntime.CodeBuffer {
	t.Helper()
	code, err := coreruntime.NewCodeBuffer(0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := code.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := code.Append(image.code); err != nil {
		t.Fatal(err)
	}
	return code
}

func TestSWARTableModuleOwnedSnapshotCanonicalKeysAndState(t *testing.T) {
	fixtures := []fixtureFunc{{params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x0b}}, {params: []byte{0x7f}, result: 0x7f, body: []byte{0x20, 0, 0x0b}, unexported: true}}
	elements := []wasm.Elem{swarStateElement(wasm.ElemActive, 1, 0, 1), swarStateElement(wasm.ElemPassive, 0, 1, 0), swarStateElement(wasm.ElemActive, 2, 0), swarStateElement(wasm.ElemDeclarative, 0, 1)}
	source := swarTableStateSource(fixtures, 4, 8, elements)
	// Reorder type records and function type indices to prove function ordinal
	// is not a type index. Bodies/signatures keep their original meaning.
	typePayload := []byte{2, 0x60, 1, 0x7f, 1, 0x7f, 0x60, 1, 0x7b, 1, 0x7b}
	source = modifySection(source, 1, 1, typePayload, true)
	source = modifySection(source, 3, 3, []byte{2, 1, 0}, true)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	image, err := compileSWARTableModule(source, emissionMode{production: true, interruptible: true})
	if err != nil || image.table == nil || len(image.elements) != 4 || len(image.functionKeys) != 2 {
		t.Fatal("owned table module", err)
	}
	for i, index := range []uint32{1, 0} {
		key, ok := m.StructuralTypeKeyChecked(index)
		if !ok || image.functionKeys[i] != key {
			t.Fatal("checked key/type-index mapping", i)
		}
	}
	if old, err := compileSWARModule(source, emissionMode{production: true, interruptible: true}); old != nil || err == nil {
		t.Fatal("default table gate widened", err)
	}
	clear(source)
	for i, e := range image.elements {
		for n, index := range elements[i].Kind.Funcs {
			if e.functions[n] != uint32(index) {
				t.Fatal("element snapshot alias")
			}
		}
	}
	code := swarTableCodeBuffer(t, image)
	for pass := 0; pass < 2; pass++ {
		job, err := coreruntime.NewJobMemory(0)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := job.Close(); err != nil {
				t.Error(err)
			}
		})
		owner, err := image.instantiateTables(job, code)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := owner.Close(); err != nil {
				t.Error(err)
			}
		})
		table, functions, descriptors := swarTableOwnedBytes(owner.table, 8+8*32), swarTableOwnedBytes(owner.functions, 3*40), swarTableOwnedBytes(owner.elements, 4*16)
		ctx := job.CaptureInstanceContext()
		if ctx.TablePtr != owner.table || ctx.FuncRefDescPtr != owner.functions || ctx.PassiveElemPtr != owner.elements || binary.LittleEndian.Uint32(table) != 4 || binary.LittleEndian.Uint32(table[4:]) != 8 {
			t.Fatal("owner binding/header", pass)
		}
		identity := binary.LittleEndian.Uint64(functions[32:])
		if identity == 0 {
			t.Fatal("zero context")
		}
		for i := 0; i < 2; i++ {
			desc := functions[(i+1)*40 : (i+2)*40]
			if binary.LittleEndian.Uint64(desc) != uint64(code.Base())+uint64(image.functions[i].Entry) || binary.LittleEndian.Uint64(desc[8:]) != image.functionKeys[i] || binary.LittleEndian.Uint64(desc[16:]) != uint64(job.LinMemBase())|uint64(abi.FuncRefLocalWrapperHomeTag) || binary.LittleEndian.Uint64(desc[24:]) != uint64(owner.functions)+uint64((i+1)*40) || binary.LittleEndian.Uint64(desc[32:]) != identity {
				t.Fatal("complete canonical entry", pass, i)
			}
		}
		want := make([]byte, len(table))
		binary.LittleEndian.PutUint32(want, 4)
		binary.LittleEndian.PutUint32(want[4:], 8)
		for _, e := range image.elements {
			if e.kind == wasm.ElemActive {
				for n, index := range e.functions {
					copy(want[8+(int(e.offset)+n)*32:], functions[(int(index)+1)*40:(int(index)+1)*40+32])
				}
			}
		}
		if !bytes.Equal(table, want) {
			t.Fatal("source-order overlap/default/spare null rows")
		}
		for _, i := range []int{0, 2, 3} {
			if !bytes.Equal(descriptors[i*16:(i+1)*16], make([]byte, 16)) {
				t.Fatal("active/declarative starts dropped", i)
			}
		}
		ptr := uintptr(binary.LittleEndian.Uint64(descriptors[16:]))
		payload := swarTableOwnedBytes(ptr, 64)
		if binary.LittleEndian.Uint32(descriptors[24:]) != 2 || !bytes.Equal(payload[:32], functions[80:112]) || !bytes.Equal(payload[32:], functions[40:72]) || binary.LittleEndian.Uint32(descriptors[28:]) != 0 {
			t.Fatal("original passive descriptor/canonical payload")
		}
		clear(table[8:])
		binary.LittleEndian.PutUint32(descriptors[24:], 0)
		if other, err := image.instantiateTables(job, code); other != nil || err == nil {
			t.Fatal("bound job replaced", err)
		}
		if err := owner.Close(); err != nil || owner.arena != nil || job.CaptureInstanceContext().TablePtr != 0 || job.CaptureInstanceContext().FuncRefDescPtr != 0 || job.PassiveElemPtr() != 0 {
			t.Fatal("detach/close", err)
		}
		if err := owner.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSWARTableOwnerPreflightAllocationSealAndRetry(t *testing.T) {
	fixtures := []fixtureFunc{{body: []byte{0x0b}}, {body: []byte{0x0b}, unexported: true}}
	source := swarTableStateSource(fixtures, 2, 4, []wasm.Elem{swarStateElement(wasm.ElemActive, 0, 1), swarStateElement(wasm.ElemPassive, 0, 0, 1)})
	image, err := compileSWARTableModule(source, emissionMode{production: true})
	if err != nil {
		t.Fatal(err)
	}
	code := swarTableCodeBuffer(t, image)
	job, err := coreruntime.NewJobMemory(0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := job.Close(); err != nil {
			t.Error(err)
		}
	})
	failure := errors.New("injected table allocation failure")
	calls := 0
	owner, err := image.instantiateTablesWithArena(job, code, func(int) (*coreruntime.Arena, error) { calls++; return nil, failure })
	if owner != nil || !errors.Is(err, failure) || calls != 1 || job.CaptureInstanceContext().TablePtr != 0 || job.CaptureInstanceContext().FuncRefDescPtr != 0 || job.PassiveElemPtr() != 0 {
		t.Fatal("arena failure published", err, calls)
	}
	owner, err = image.instantiateTables(job, code)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := owner.Close(); err != nil {
			t.Error(err)
		}
	})
	saved := owner.table
	for _, replace := range []func(uintptr){job.SetTablePtr, job.SetFuncRefDesc, job.SetPassiveElemPtr} {
		replace(1)
		before := job.CaptureInstanceContext()
		if err := owner.Close(); err == nil || owner.arena == nil || owner.code != code || job.CaptureInstanceContext() != before {
			t.Fatal("partially replaced binding cleared/released", err)
		}
		job.SetTablePtr(owner.table)
		job.SetFuncRefDesc(owner.functions)
		job.SetPassiveElemPtr(owner.elements)
	}
	if err := owner.closeWith(func(*coreruntime.Arena) error { return failure }); !errors.Is(err, failure) || owner.arena == nil || owner.job != job || owner.code != code || owner.table != saved || job.CaptureInstanceContext().TablePtr != 0 || job.CaptureInstanceContext().FuncRefDescPtr != 0 || job.PassiveElemPtr() != 0 {
		t.Fatal("failed release lost ownership or retained binding", err)
	}
	if err := owner.Close(); err != nil || owner.arena != nil || owner.code != nil || code.Base() == 0 {
		t.Fatal("retry release/code shared lifetime", err)
	}
	for _, bad := range []func(*swarModuleImage){
		func(i *swarModuleImage) {
			i.functions = append([]FunctionInfo(nil), i.functions...)
			i.functions[0].Entry = 4
		},
		func(i *swarModuleImage) { i.functionKeys = nil },
		func(i *swarModuleImage) { i.elements = []swarElement{{kind: wasm.ElemPassive, functions: []uint32{2}}} },
		func(i *swarModuleImage) {
			i.elements = []swarElement{{kind: wasm.ElemPassive, functions: make([]uint32, 1025)}}
		},
		func(i *swarModuleImage) { i.elements = []swarElement{{kind: wasm.ElemModeKind(255)}} },
		func(i *swarModuleImage) { i.elements = []swarElement{{kind: wasm.ElemDeclarative, offset: 1}} },
	} {
		broken := *image
		bad(&broken)
		calls = 0
		if got, err := broken.instantiateTablesWithArena(job, code, func(int) (*coreruntime.Arena, error) { calls++; return nil, failure }); got != nil || err == nil || calls != 0 || job.CaptureInstanceContext().TablePtr != 0 {
			t.Fatal("malformed owner reached allocation", err, calls)
		}
	}
	// A late invalid active segment prevents all sealing/allocation/publication.
	for _, offset := range []uint32{2, 3, 0xffffffff} {
		broken, err := compileSWARTableModule(swarTableStateSource(fixtures, 2, 4, []wasm.Elem{swarStateElement(wasm.ElemActive, 0, 0), swarStateElement(wasm.ElemActive, offset, 1)}), emissionMode{production: true})
		if err != nil {
			t.Fatal(err)
		}
		unsealed := swarTableCodeBuffer(t, broken)
		calls = 0
		if got, err := broken.instantiateTablesWithArena(job, unsealed, func(int) (*coreruntime.Arena, error) { calls++; return nil, failure }); got != nil || err == nil || calls != 0 || job.CaptureInstanceContext().TablePtr != 0 {
			t.Fatal("active all-range preflight", offset, err, calls)
		}
		if err := unsealed.Append(nil); err != nil {
			t.Fatal("preflight failure sealed code", err)
		}
	}
	// Exact bytes and real offheap sealing are prerequisites, not address guesses.
	heap, err := coreruntime.NewHeapCodeBuffer(len(image.code))
	if err != nil {
		t.Fatal(err)
	}
	defer heap.Close()
	if err := heap.Append(image.code); err != nil {
		t.Fatal(err)
	}
	calls = 0
	if got, err := image.instantiateTablesWithArena(job, heap, func(int) (*coreruntime.Arena, error) { calls++; return nil, failure }); got != nil || err == nil || calls != 0 {
		t.Fatal("heap sealing accepted", err, calls)
	}
	mismatch := swarTableCodeBuffer(t, image)
	mismatch.Bytes()[0] ^= 1
	if got, err := image.instantiateTables(job, mismatch); got != nil || err == nil {
		t.Fatal("mismatched code published", err)
	}
	closed := swarTableCodeBuffer(t, image)
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	if got, err := image.instantiateTables(job, closed); got != nil || err == nil {
		t.Fatal("closed code published", err)
	}
}

func TestSWARTableModuleLegacyElementsAndResourcePolicy(t *testing.T) {
	f := fixtureFunc{body: []byte{0x0b}}
	for _, source := range [][]byte{
		swarTableStateSource([]fixtureFunc{f}, 1025, 1025, nil),
		swarTableStateSource([]fixtureFunc{f}, 0, 1025, nil),
		swarTableStateSource([]fixtureFunc{f}, 2, 1, nil),
		swarTableStateSource([]fixtureFunc{f}, 1, 1, make([]wasm.Elem, 257)),
	} {
		if image, err := compileSWARTableModule(source, emissionMode{production: true}); image != nil || err == nil {
			t.Fatal("bounded table metadata escaped", err)
		}
	}
	// Even declaration-only payloads count towards the shared1024-entry quota.
	large := swarStateElement(wasm.ElemDeclarative, 0)
	large.Kind.Funcs = make([]wasm.FuncIdx, 1024)
	source := swarTableStateSource([]fixtureFunc{f}, 0, 0, []wasm.Elem{large})
	if image, err := compileSWARTableModule(source, emissionMode{production: true}); image == nil || err != nil {
		t.Fatal("exact declaration payload bound", err)
	}
	large.Kind.Funcs = append(large.Kind.Funcs, 0)
	if image, err := compileSWARTableModule(swarTableStateSource([]fixtureFunc{f}, 0, 0, []wasm.Elem{large}), emissionMode{production: true}); image != nil || err == nil {
		t.Fatal("declaration payload bound escaped", err)
	}
	for _, elements := range [][]wasm.Elem{
		{{Mode: wasm.ElemMode{Kind: wasm.ElemPassive, Table: 1}}},
		{{Mode: wasm.ElemMode{Kind: wasm.ElemPassive}, Kind: wasm.ElemKind{Kind: wasm.ElemFuncExprs}}},
		{{Mode: wasm.ElemMode{Kind: wasm.ElemActive, Offset: wasm.Expr{BodyBytes: []byte{0x41, 1, 0x41, 2, 0x6a, 0x0b}}}}},
	} {
		if copied, err := swarModuleElements(elements); copied != nil || err == nil {
			t.Fatal("element shape escaped", err)
		}
	}
}
