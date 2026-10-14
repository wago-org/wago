//go:build (linux || darwin || windows) && arm64 && !tinygo

package wago

import (
	"context"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"runtime"
	"strings"
	"testing"
)

//go:noinline
//go:norace
func nativeSingleCallLeaf(v int32) int32 { return v + 1 }

//go:noinline
//go:norace
func nativeSingleCallForward(v int32) int32 { return nativeSingleCallLeaf(v) }
func TestNativeSingleCallSourceArtifactAndPolicy(t *testing.T) {
	requireNativeSingleCallABI(t)
	for _, mode := range []string{"0", "1"} {
		t.Setenv("WAGO_ARM64_NATIVE_SCALAR_CALL", mode)
		for _, enabled := range []bool{false, true} {
			cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithOptimization("native-leaf-host", enabled)
			c, err := cfg.Compile(nativeLeafAliasModule())
			if err != nil {
				t.Fatal(err)
			}
			for artifact, code := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
				rt := NewRuntime(WithRuntimeConfig(cfg))
				ref, err := rt.NewHostFuncRef(nativeSingleCallForward, FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}})
				if err != nil {
					t.Fatal(err)
				}
				module, err := rt.Module(code)
				if err != nil {
					t.Fatal(err)
				}
				in, err := rt.Instantiate(context.Background(), module, WithImports(NewImports().Function("env", "a", ref).Function("env", "b", ref)))
				if err != nil {
					t.Fatal(err)
				}
				ref.Close()
				wantNative := mode == "1" && enabled && artifact == 0 && !codeProfileEnabled
				if actual := in.executionFlags.Load()&executionFlagNativeScalarLeaf != 0; actual != wantNative {
					t.Fatalf("mode=%s enabled=%t artifact=%d admission=%t want=%t", mode, enabled, artifact, actual, wantNative)
				}
				for _, v := range []int32{0, -1, 2147483647, -2147483648} {
					got, err := in.Invoke("run", I32(v))
					if err != nil || len(got) != 1 || got[0] != I32(v+2) {
						t.Fatalf("v=%d got=%v err=%v", v, got, err)
					}
				}
				in.Close()
				module.Close()
				rt.Close()
				if code != c {
					code.Close()
				}
			}
			c.Close()
		}
	}
}
func TestNativeSingleCallSharedSourceAndGoEffects(t *testing.T) {
	requireNativeSingleCallABI(t)
	t.Setenv("WAGO_ARM64_NATIVE_SCALAR_CALL", "1")
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit)
	c, err := cfg.Compile(fixedSharedHostLoopModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for artifact, code := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
		for _, effectful := range []bool{false, true} {
			calls := 0
			var fn any = nativeSingleCallForward
			if effectful {
				fn = func(v int32) int32 { calls++; return nativeSingleCallLeaf(v) }
			}
			in, err := Instantiate(code, InstantiateOptions{Imports: testImports("env.step", fn)})
			if err != nil {
				t.Fatal(err)
			}
			if actual := in.executionFlags.Load()&executionFlagNativeScalarLeaf != 0; actual != (artifact == 0 && !effectful && !codeProfileEnabled) {
				t.Fatalf("artifact=%d effectful=%t native=%t", artifact, effectful, actual)
			}
			for _, n := range []uint64{1, 17, 65536} {
				calls = 0
				got, err := in.Invoke("run", n)
				want := uint64(uint32(n * (n + 1) / 2))
				if err != nil || len(got) != 1 || got[0] != want || effectful && calls != int(n) {
					t.Fatalf("n=%d got=%v err=%v calls=%d", n, got, err, calls)
				}
			}
			in.Close()
		}
		if code != c {
			code.Close()
		}
	}
}
func TestNativeSingleCallCancellationAndGC(t *testing.T) {
	requireNativeSingleCallABI(t)
	t.Setenv("WAGO_ARM64_NATIVE_SCALAR_CALL", "1")
	module := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(2, []byte{2, 3, 'e', 'n', 'v', 1, 'a', 0, 0, 3, 'e', 'n', 'v', 1, 'b', 0, 0}), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{3, 0x40, 0x20, 1, 0x10, 0, 0x10, 1, 0x21, 1, 0x20, 0, 0x41, 1, 0x6b, 0x22, 0, 0x0d, 0, 0x0b, 0x20, 1, 0x0b}))))
	testNativeLeafLoopScheduling(t, module, testImports("env.a", nativeSingleCallForward, "env.b", nativeSingleCallForward))
}

func requireNativeSingleCallABI(t *testing.T) {
	t.Helper()
	v := runtime.Version()
	if v != "go1.27" && !strings.HasPrefix(v, "go1.27.") {
		t.Skip("single-call proof is restricted to Go1.27")
	}
}
