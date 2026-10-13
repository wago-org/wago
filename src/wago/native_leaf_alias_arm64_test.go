//go:build (linux || darwin || windows) && arm64 && !tinygo

package wago

import (
	"context"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func nativeLeafAliasModule() []byte {
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, []byte{2, 3, 'e', 'n', 'v', 1, 'a', 0, 0, 3, 'e', 'n', 'v', 1, 'b', 0, 0}),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x10, 0, 0x10, 1, 0x0b}))))
}

func TestNativeLeafAliasesAdmissionAndValues(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains Go callbacks")
	}
	for _, enabled := range []bool{false, true} {
		c, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithOptimization("native-leaf-host", enabled).Compile(nativeLeafAliasModule())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		for index, product := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
			in, err := Instantiate(product, InstantiateOptions{Imports: testImports("env.a", nativeLeafIncrement, "env.b", nativeLeafIncrement)})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if admitted := in.executionFlags.Load()&executionFlagNativeScalarLeaf != 0; admitted != (enabled && index == 0) {
				t.Fatalf("enabled=%t codec=%d native=%t", enabled, index, admitted)
			}
			if in.boundedHostSegments() && in.executionFlags.Load()&executionFlagNativeScalarLeaf != 0 {
				t.Fatal("native alias treated as Go safe point")
			}
			for _, x := range []int32{0, -1, 2147483647, -2147483648} {
				got, err := in.Invoke("run", I32(x))
				if err != nil || len(got) != 1 || got[0] != I32(x+2) {
					t.Fatalf("x=%d got=%v err=%v", x, got, err)
				}
			}
		}
		calls := 0
		in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.a", nativeLeafIncrement, "env.b", func(x int32) int32 { calls++; return x + 5 })})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		if in.executionFlags.Load()&executionFlagNativeScalarLeaf != 0 {
			t.Fatal("mixed effectful callback admitted")
		}
		got, err := in.Invoke("run", 7)
		if err != nil || got[0] != 13 || calls != 1 {
			t.Fatalf("mixed=%v err=%v calls=%d", got, err, calls)
		}
	}
}

//go:noinline
func capturedNativeLeafAlias(delta int32) func(int32) int32 {
	return func(x int32) int32 { return x + delta }
}

func TestNativeLeafAliasesRejectDifferentBodiesAndCapturedState(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains Go callbacks")
	}
	c, err := NewRuntimeConfig().Compile(nativeLeafAliasModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for name, callbacks := range map[string][2]func(int32) int32{
		"different-pure-bodies":       {nativeLeafIncrement, nativeLeafIncrementFive},
		"same-code-distinct-captures": {capturedNativeLeafAlias(1), capturedNativeLeafAlias(5)},
	} {
		t.Run(name, func(t *testing.T) {
			in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.a", callbacks[0], "env.b", callbacks[1])})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if in.executionFlags.Load()&executionFlagNativeScalarLeaf != 0 {
				t.Fatal("nonalias or captured body admitted")
			}
			got, err := in.Invoke("run", 7)
			if err != nil || len(got) != 1 || got[0] != 13 {
				t.Fatalf("got=%v err=%v", got, err)
			}
		})
	}
}

func TestNativeLeafAliasesLoopScheduling(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, []byte{2, 3, 'e', 'n', 'v', 1, 'a', 0, 0, 3, 'e', 'n', 'v', 1, 'b', 0, 0}),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x03, 0x40, 0x20, 1, 0x10, 0, 0x10, 1, 0x21, 1, 0x20, 0, 0x41, 1, 0x6b, 0x22, 0, 0x0d, 0, 0x0b, 0x20, 1, 0x0b}))))
	testNativeLeafLoopScheduling(t, module, testImports("env.a", nativeLeafIncrement, "env.b", nativeLeafIncrement))
}

func TestNativeLeafOwnedAliasesLifetime(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains Go callbacks")
	}
	owner := NewRuntime()
	defer owner.Close()
	ref, err := owner.NewHostFuncRef(nativeLeafIncrement, FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}})
	if err != nil {
		t.Fatal(err)
	}
	defer ref.Close()
	c, err := owner.Compile(nativeLeafAliasModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := owner.Instantiate(context.Background(), c, WithImports(NewImports().Function("env", "a", ref).Function("env", "b", ref)))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if in.executionFlags.Load()&executionFlagNativeScalarLeaf == 0 {
		t.Fatal("owned aliases did not lower")
	}
	got, err := in.Invoke("run", 7)
	if err != nil || got[0] != 9 {
		t.Fatalf("got=%v err=%v", got, err)
	}
	// Logical close preserves the references already retained by this instance.
	ref.Close()
	got, err = in.Invoke("run", 7)
	if err != nil || got[0] != 9 {
		t.Fatalf("retained aliases got=%v err=%v", got, err)
	}
}
