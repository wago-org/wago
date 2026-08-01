package wago

import (
	"context"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

func gcHostBridgeModule(t testing.TB) []byte {
	t.Helper()
	const moduleHex = "0061736d010000000114045f017f0060016e016e600001640060016e017f020d0104686f7374046563686f00010307060203010101010406016301010101073406036e65770001047265616400020664697265637400030863616c6c5f7265660004047461696c000508696e6469726563740006090c01060041000b630101d2000b0a38060700412afb00000b0b002000fb1600fb0200000b0600200010000b08002000d20014010b08002000d20015010b0900200041001101000b001a046e616d6501070100046563686f040a020001730104686f7374"
	module, err := hex.DecodeString(moduleHex)
	if err != nil {
		t.Fatal(err)
	}
	return module
}

func TestRuntimeGCHostFuncRefCallAndTailOwnership(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	rt := NewRuntime(WithRuntimeConfig(cfg))
	defer rt.Close()
	var calls int
	owner, err := rt.NewGCHostFuncRef(HostFunc(func(_ HostModule, args, results []uint64) {
		calls++
		if len(args) != 1 || len(results) == 0 || args[0] == 0 || args[0]>>32 == 0 {
			panic("GC host argument is not an opaque non-null token")
		}
		results[0] = args[0]
	}), FuncSig{Params: []ValType{ValAnyRef}, Results: []ValType{ValAnyRef}})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	mod, err := rt.Compile(gcHostBridgeModule(t))
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	in, err := rt.Instantiate(context.Background(), mod, WithImports(Imports{"host.echo": owner}))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if owner.gc.collector == nil || owner.gc.collector != in.gc || owner.gc.domainID == 0 || owner.gc.domainID != instanceNativeGCDomainID(in) {
		t.Fatalf("GC host owner domain = %p/%d, instance = %p/%d", owner.gc.collector, owner.gc.domainID, in.gc, instanceNativeGCDomainID(in))
	}
	created, err := in.Call(context.Background(), "new")
	if err != nil || len(created) != 1 || created[0].GCRef().IsNull() {
		t.Fatalf("new = %v, %v", created, err)
	}
	input := created[0].GCRef()
	for _, name := range []string{"direct", "call_ref", "tail", "indirect"} {
		got, callErr := in.Call(context.Background(), name, ValueGCRef(input))
		if callErr != nil || len(got) != 1 || got[0].Type() != ValAnyRef || got[0].GCRef().IsNull() {
			t.Fatalf("%s = %v, %v", name, got, callErr)
		}
		if read, readErr := in.Call(context.Background(), "read", ValueGCRef(got[0].GCRef())); readErr != nil || !reflect.DeepEqual(read, []Value{ValueI32(42)}) {
			t.Fatalf("read(%s) = %v, %v", name, read, readErr)
		}
		if err := in.ReleaseGCRef(got[0].GCRef()); err != nil {
			t.Fatalf("release %s result: %v", name, err)
		}
	}
	if calls != 4 {
		t.Fatalf("GC host calls = %d, want 4", calls)
	}
	if err := in.ReleaseGCRef(input); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeGCHostFuncRefRejectsUnownedAndForeignDomains(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	compiled, err := Compile(cfg, gcHostBridgeModule(t))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	plainRT := NewRuntime(WithRuntimeConfig(cfg))
	plain, err := plainRT.NewHostFuncRef(HostFunc(func(_ HostModule, args, results []uint64) { results[0] = args[0] }), FuncSig{Params: []ValType{ValAnyRef}, Results: []ValType{ValAnyRef}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plainRT.Instantiate(context.Background(), &Module{c: compiled}, WithImports(Imports{"host.echo": plain})); err == nil || !strings.Contains(err.Error(), "NewGCHostFuncRef") {
		t.Fatalf("plain GC host import error = %v", err)
	}
	_ = plain.Close()
	_ = plainRT.Close()

	firstRT := NewRuntime(WithRuntimeConfig(cfg))
	defer firstRT.Close()
	owner, err := firstRT.NewGCHostFuncRef(HostFunc(func(_ HostModule, args, results []uint64) { results[0] = args[0] }), FuncSig{Params: []ValType{ValAnyRef}, Results: []ValType{ValAnyRef}})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	firstMod, err := firstRT.Compile(gcHostBridgeModule(t))
	if err != nil {
		t.Fatal(err)
	}
	defer firstMod.Close()
	first, err := firstRT.Instantiate(context.Background(), firstMod, WithImports(Imports{"host.echo": owner}))
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	secondRT := NewRuntime(WithRuntimeConfig(cfg))
	defer secondRT.Close()
	secondMod, err := secondRT.Compile(gcHostBridgeModule(t))
	if err != nil {
		t.Fatal(err)
	}
	defer secondMod.Close()
	if _, err := secondRT.Instantiate(context.Background(), secondMod, WithImports(Imports{"host.echo": owner})); err == nil || !strings.Contains(err.Error(), "cannot transfer collector references") {
		t.Fatalf("foreign Runtime GC host import error = %v", err)
	}
}
