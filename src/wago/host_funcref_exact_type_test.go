//go:build !tinygo

package wago

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func hostFuncRefTestFuncType(params, results []wasm.ValType) []byte {
	out := []byte{0x60}
	out = append(out, wasmtest.ULEB(uint32(len(params)))...)
	appendType := func(t wasm.ValType) {
		if b, ok := wasm.EncodeValType(t); ok {
			out = append(out, b)
			return
		}
		ref := t.Ref()
		if ref.Nullable() {
			out = append(out, 0x63)
		} else {
			out = append(out, 0x64)
		}
		out = append(out, wasmtest.SLEB32(int32(ref.Heap().Type().Index))...)
	}
	for _, param := range params {
		appendType(param)
	}
	out = append(out, wasmtest.ULEB(uint32(len(results)))...)
	for _, result := range results {
		appendType(result)
	}
	return out
}

func hostFuncRefExactTypeModule(paddingTypes int, nodeResult wasm.ValType) []byte {
	types := make([][]byte, 0, paddingTypes+3)
	for i := 0; i < paddingTypes; i++ {
		types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.I64}, nil))
	}
	nodeIndex := uint32(len(types))
	nullNodeRef := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: nodeIndex}), false))
	nodeRef := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: nodeIndex}), false))
	types = append(types, hostFuncRefTestFuncType([]wasm.ValType{nullNodeRef}, []wasm.ValType{nodeResult}))
	hostTypeIndex := uint32(len(types))
	types = append(types, hostFuncRefTestFuncType([]wasm.ValType{nodeRef}, []wasm.ValType{nodeRef}))
	runTypeIndex := uint32(len(types))
	types = append(types, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))

	importEntry := append(wasmtest.Name("host"), wasmtest.Name("echo")...)
	importEntry = append(importEntry, 0x00)
	importEntry = append(importEntry, wasmtest.ULEB(hostTypeIndex)...)

	targetBody := []byte{0x41, 0x07, 0x0b}
	if nodeResult == wasm.I64 {
		targetBody = []byte{0x42, 0x07, 0x0b}
	}
	runBody := []byte{0xd2, 0x01, 0x41, 0x00, 0x11}
	runBody = append(runBody, wasmtest.ULEB(hostTypeIndex)...)
	runBody = append(runBody, 0x00, 0xd1, 0x0b)

	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(nodeIndex), wasmtest.ULEB(runTypeIndex))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x01, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0), tableTestDeclarativeElem(1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(targetBody), wasmtest.Code(runBody))),
	)
}

func hostFuncRefDuplicateExactTypeModule() []byte {
	padding := wasmtest.FuncType([]wasm.ValType{wasm.I64}, nil)
	nodeAIndex, nodeBIndex := uint32(1), uint32(2)
	nullNodeA := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: nodeAIndex}), false))
	nodeA := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: nodeAIndex}), false))
	nullNodeB := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: nodeBIndex}), false))
	nodeB := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: nodeBIndex}), false))
	types := [][]byte{
		padding,
		hostFuncRefTestFuncType([]wasm.ValType{nullNodeA}, []wasm.ValType{wasm.I32}),
		hostFuncRefTestFuncType([]wasm.ValType{nullNodeB}, []wasm.ValType{wasm.I32}),
		hostFuncRefTestFuncType([]wasm.ValType{nodeA}, []wasm.ValType{nodeA}),
		hostFuncRefTestFuncType([]wasm.ValType{nodeB}, []wasm.ValType{nodeB}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	importA := append(wasmtest.Name("host"), wasmtest.Name("echo-a")...)
	importA = append(importA, 0x00)
	importA = append(importA, wasmtest.ULEB(3)...)
	importB := append(wasmtest.Name("host"), wasmtest.Name("echo-b")...)
	importB = append(importB, 0x00)
	importB = append(importB, wasmtest.ULEB(4)...)
	runA := []byte{0xd2, 0x02, 0x41, 0x00, 0x11, 0x03, 0x00, 0xd1, 0x0b}
	runB := []byte{0xd2, 0x03, 0x41, 0x01, 0x11, 0x04, 0x00, 0xd1, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(2, wasmtest.Vec(importA, importB)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(5), wasmtest.ULEB(5))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x02, 0x02})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run-a", 0, 4), wasmtest.ExportEntry("run-b", 0, 5))),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0, 1), tableTestDeclarativeElem(2, 3))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x07, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x08, 0x0b}),
			wasmtest.Code(runA),
			wasmtest.Code(runB),
		)),
	)
}

func hostFuncRefScalarExactTypeModule() []byte {
	ft := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64})
	importEntry := append(wasmtest.Name("host"), wasmtest.Name("scalar")...)
	importEntry = append(importEntry, 0x00, 0x00)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(ft)),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x00, 0x0b}))),
	)
}

func TestHostFuncRefPreservesIndexedScalarSignature(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	owner, err := rt.NewHostFuncRef(HostFunc(func(m HostModule, args, results []uint64) {
		storageModule, ok := m.(GuestStorageHostModule)
		if !ok {
			panic(HostTrap{Err: fmt.Errorf("host module does not expose guest storage")})
		}
		if err := storageModule.WithGuestStorage(func(storage GuestStorage) error {
			param, paramOK := storage.ImportParamType(0)
			result, resultOK := storage.ImportResultType(0)
			defined, definedOK := storage.DefinedType(0)
			if !paramOK || param.Kind != ValueTypeI32 || !resultOK || result.Kind != ValueTypeI64 || !definedOK || defined.Kind != CompositeTypeFunction || len(defined.Params) != 1 || len(defined.Results) != 1 {
				return fmt.Errorf("scalar exact signature = param %#v/%v result %#v/%v type %#v/%v", param, paramOK, result, resultOK, defined, definedOK)
			}
			return nil
		}); err != nil {
			panic(HostTrap{Err: err})
		}
		results[0] = args[0]
	}), FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI64}, TypeIndex: 0, HasTypeIndex: true})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	compiled, err := rt.Compile(hostFuncRefScalarExactTypeModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := rt.Instantiate(context.Background(), compiled, WithImports(Imports{"host.scalar": owner}))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := in.Call(context.Background(), "run", ValueI32(9)); err != nil || len(got) != 1 || got[0].I64() != 9 {
		t.Fatalf("scalar owned host call = %v, %v; want [9], nil", got, err)
	}
	owner.mu.Lock()
	bindings := len(owner.gc.dispatchBindings)
	if owner.gc.inlineDispatchBinding != nil {
		bindings++
	}
	owner.mu.Unlock()
	if bindings != 1 {
		t.Fatalf("live scalar dispatch bindings = %d, want 1", bindings)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	owner.mu.Lock()
	bindings = len(owner.gc.dispatchBindings)
	if owner.gc.inlineDispatchBinding != nil {
		bindings++
	}
	owner.mu.Unlock()
	if bindings != 0 {
		t.Fatalf("scalar dispatch bindings after close = %d, want 0", bindings)
	}
}

func TestHostFuncRefUsesImporterLocalExactSignature(t *testing.T) {
	requireCompleteCore3Backend(t)
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	rt := NewRuntime(WithRuntimeConfig(cfg))
	defer rt.Close()

	var seenTypeIndexes []uint32
	owner, err := rt.NewHostFuncRef(HostFunc(func(m HostModule, args, results []uint64) {
		storageModule, ok := m.(GuestStorageHostModule)
		if !ok {
			panic(HostTrap{Err: fmt.Errorf("host module does not expose guest storage")})
		}
		if err := storageModule.WithGuestStorage(func(storage GuestStorage) error {
			param, ok := storage.ImportParamType(0)
			if !ok || param.Kind != ValueTypeReference || param.Ref.Nullable || !param.Ref.Heap.Defined {
				return fmt.Errorf("exact host parameter = %#v, %v", param, ok)
			}
			result, ok := storage.ImportResultType(0)
			if !ok || result != param {
				return fmt.Errorf("exact host result = %#v, %v; want %#v", result, ok, param)
			}
			node, ok := storage.DefinedType(param.Ref.Heap.TypeIndex)
			if !ok || node.Kind != CompositeTypeFunction || len(node.Params) != 1 || len(node.Results) != 1 || node.Results[0].Kind != ValueTypeI32 {
				return fmt.Errorf("defined recursive function type = %#v, %v", node, ok)
			}
			self := node.Params[0]
			if self.Kind != ValueTypeReference || !self.Ref.Nullable || !self.Ref.Heap.Defined || self.Ref.Heap.TypeIndex != param.Ref.Heap.TypeIndex {
				return fmt.Errorf("recursive parameter = %#v, want nullable self reference to %d", self, param.Ref.Heap.TypeIndex)
			}
			seenTypeIndexes = append(seenTypeIndexes, param.Ref.Heap.TypeIndex)
			return nil
		}); err != nil {
			panic(HostTrap{Err: err})
		}
		results[0] = args[0]
	}), FuncSig{Params: []ValType{ValFuncRef}, Results: []ValType{ValFuncRef}})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()

	var instances []*Instance
	defer func() {
		for _, in := range instances {
			_ = in.Close()
		}
	}()
	for _, padding := range []int{1, 2} {
		compiled, err := rt.Compile(hostFuncRefExactTypeModule(padding, wasm.I32))
		if err != nil {
			t.Fatalf("compile importer with %d padding types: %v", padding, err)
		}
		defer compiled.Close()
		in, err := rt.Instantiate(context.Background(), compiled, WithImports(Imports{"host.echo": owner}))
		if err != nil {
			t.Fatalf("instantiate importer with %d padding types: %v", padding, err)
		}
		instances = append(instances, in)
		if got, err := in.Call(context.Background(), "run"); err != nil || len(got) != 1 || got[0].I32() != 0 {
			t.Fatalf("run importer with %d padding types = %v, %v; want [0], nil", padding, got, err)
		}
	}
	duplicate, err := rt.Compile(hostFuncRefDuplicateExactTypeModule())
	if err != nil {
		t.Fatalf("compile duplicate importer bindings: %v", err)
	}
	defer duplicate.Close()
	duplicateInstance, err := rt.Instantiate(context.Background(), duplicate, WithImports(Imports{"host.echo-a": owner, "host.echo-b": owner}))
	if err != nil {
		t.Fatalf("instantiate duplicate importer bindings: %v", err)
	}
	instances = append(instances, duplicateInstance)
	for _, export := range []string{"run-a", "run-b"} {
		if got, err := duplicateInstance.Call(context.Background(), export); err != nil || len(got) != 1 || got[0].I32() != 0 {
			t.Fatalf("%s duplicate importer binding = %v, %v; want [0], nil", export, got, err)
		}
	}
	if fmt.Sprint(seenTypeIndexes) != "[1 2 1 2]" {
		t.Fatalf("callback importer-local type indexes = %v, want [1 2 1 2]", seenTypeIndexes)
	}

	mismatch, err := rt.Compile(hostFuncRefExactTypeModule(2, wasm.I64))
	if err != nil {
		t.Fatalf("compile structural mismatch: %v", err)
	}
	defer mismatch.Close()
	if _, err := rt.Instantiate(context.Background(), mismatch, WithImports(Imports{"host.echo": owner})); err == nil || !strings.Contains(err.Error(), "structural signature mismatch") {
		t.Fatalf("non-equivalent recursive host signature error = %v", err)
	}
}
