//go:build linux && amd64 && !tinygo

package wago

import (
	"context"
	"strings"
	"testing"

	"github.com/wago-org/wago/testutil/wasmtest"
)

func typedMutableGlobalModule(typeDefs [][]byte, typeIndex uint32) []byte {
	refType := encodedNullableIndexedRef(typeIndex)
	global := append([]byte(nil), refType...)
	global = append(global, 0x01, 0xd0) // mutable, ref.null
	global = append(global, wasmtest.ULEB(typeIndex)...)
	global = append(global, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(typeDefs...)),
		wasmtest.Section(6, wasmtest.Vec(global)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 3, 0))),
	)
}

func TestTypedFunctionReferenceMutableGlobalBoundaries(t *testing.T) {
	store := newReferenceStore(false)
	producer, err := instantiateCore(stagedTypedStorageCompile(t, typedCallBoundaryModule()), InstantiateOptions{store: store})
	if err != nil {
		t.Fatalf("instantiate token producer: %v", err)
	}
	defer producer.Close()

	matching, err := producer.Call(context.Background(), "getF")
	if err != nil || len(matching) != 1 {
		t.Fatalf("getF = %v, %v", matching, err)
	}
	mismatch, err := producer.Call(context.Background(), "getG")
	if err != nil || len(mismatch) != 1 {
		t.Fatalf("getG = %v, %v", mismatch, err)
	}

	dummy := encodedFuncType(nil, nil)
	equivalent := encodedFuncType(nil, nil)
	compiled := stagedTypedStorageCompile(t, typedMutableGlobalModule([][]byte{dummy, equivalent}, 1))
	in, err := instantiateCore(compiled, InstantiateOptions{store: store})
	if err != nil {
		t.Fatalf("instantiate typed mutable global: %v", err)
	}
	defer in.Close()

	if err := in.SetGlobalValue("g", matching[0]); err != nil {
		t.Fatalf("SetGlobalValue(matching): %v", err)
	}
	got, err := in.GlobalValue("g")
	if err != nil || got.Bits() != matching[0].Bits() {
		t.Fatalf("GlobalValue after matching set = %#x, %v; want %#x", got.Bits(), err, matching[0].Bits())
	}
	if err := in.SetGlobalValue("g", mismatch[0]); err == nil || !strings.Contains(err.Error(), "exact structural type") {
		t.Fatalf("SetGlobalValue(mismatch) error = %v", err)
	}
	got, err = in.GlobalValue("g")
	if err != nil || got.Bits() != matching[0].Bits() {
		t.Fatalf("GlobalValue after rejected set = %#x, %v; want unchanged %#x", got.Bits(), err, matching[0].Bits())
	}

	global, err := in.ExportedGlobalObject("g")
	if err != nil {
		t.Fatalf("ExportedGlobalObject: %v", err)
	}
	if err := global.SetValue(mismatch[0]); err == nil || !strings.Contains(err.Error(), "exact structural type") {
		t.Fatalf("Global.SetValue(mismatch) error = %v", err)
	}
	if err := global.SetValue(ValueFuncRef(NullFuncRef())); err != nil {
		t.Fatalf("Global.SetValue(null): %v", err)
	}
	got, err = global.GetValue()
	if err != nil || !got.FuncRef().IsNull() {
		t.Fatalf("Global.GetValue after null = %#x, %v", got.Bits(), err)
	}
	if err := global.SetValue(matching[0]); err != nil {
		t.Fatalf("Global.SetValue(matching): %v", err)
	}
	got, err = global.GetValue()
	if err != nil || got.Bits() != matching[0].Bits() {
		t.Fatalf("Global.GetValue after matching set = %#x, %v; want %#x", got.Bits(), err, matching[0].Bits())
	}
}
