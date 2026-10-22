//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_precompiled

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"slices"
	"testing"
)

//go:noinline
func rv64PublicPureIncrement(v int32) int32 { return v + 1 }

//go:noinline
func rv64PublicPurePair(a, b int32) int32 { return a*b + a - b }

func TestRV64PublicNativeLeafPolicyValuesAndArtifacts(t *testing.T) {
	source := rv64PublicHostModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	for _, enabled := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			c, err := Compile(NewRuntimeConfig().WithOptimization("native-leaf-host", enabled), source)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if artifact {
				blob, err := c.MarshalBinary()
				if err != nil {
					t.Fatal(err)
				}
				loaded, err := LoadTrustedArtifact(blob)
				if err != nil {
					t.Fatal(err)
				}
				c = loaded
				defer c.Close()
			}
			in, err := Instantiate(c, testImports("env.host", rv64PublicPureIncrement))
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			wantNative := enabled && !artifact && !codeProfileEnabled
			if got := in.executionFlags.Load()&executionFlagNativeScalarLeaf != 0; got != wantNative {
				t.Fatal("leaf policy", enabled, artifact, got, wantNative)
			}
			prepared, err := in.WasmFunc("call")
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range []int32{0, 1, -1, -2147483648, 2147483647} {
				for _, call := range []func(...uint64) ([]uint64, error){func(args ...uint64) ([]uint64, error) { return in.Invoke("call", args...) }, prepared.Invoke} {
					got, err := call(I32(v))
					if err != nil || !slices.Equal(got, []uint64{I32(v + 1)}) {
						t.Fatal("signed native value", v, got, err)
					}
				}
			}
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			got, err := in.Invoke("call", 41)
			if err != nil || !slices.Equal(got, []uint64{42}) {
				t.Fatal("retained native owner", got, err)
			}
		}
	}
}
func TestRV64PublicNativeLeafSideEffectsFallback(t *testing.T) {
	source := rv64PublicHostModule([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32})
	c, err := Compile(NewRuntimeConfig().WithOptimization("native-leaf-host", true), source)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, testImports("env.host", rv64PublicPurePair))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got := in.executionFlags.Load()&executionFlagNativeScalarLeaf != 0; got != !codeProfileEnabled {
		t.Fatal("pair native policy", got)
	}
	state := uint32(0x9e3779b9)
	for range 512 {
		state = state*1664525 + 1013904223
		a := int32(state)
		state = state*1664525 + 1013904223
		b := int32(state)
		got, err := in.Invoke("call", I32(a), I32(b))
		if err != nil || !slices.Equal(got, []uint64{I32(rv64PublicPurePair(a, b))}) {
			t.Fatal(a, b, got, err)
		}
	}
	calls := 0
	fallback, err := Instantiate(c, testImports("env.host", func(a, b int32) int32 { calls++; return a*b + a - b }))
	if err != nil {
		t.Fatal(err)
	}
	defer fallback.Close()
	if fallback.executionFlags.Load()&executionFlagNativeScalarLeaf != 0 {
		t.Fatal("side-effect callback admitted")
	}
	got, err := fallback.Invoke("call", 7, 9)
	if err != nil || !slices.Equal(got, []uint64{61}) || calls != 1 {
		t.Fatal("side effects", got, err, calls)
	}
}

func BenchmarkRV64PublicHostWarmAllocation(b *testing.B) {
	types := []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}
	c, err := Compile(rv64PublicHostModule(types, types))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, testImports("env.host", HostCallFunc(func(call HostCall) { copy(call.ResultSlots(), call.ParamSlots()) })))
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	args := []uint64{1, 2, 3, 4}
	if _, err := in.Invoke("call", args...); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := in.Invoke("call", args...); err != nil {
			b.Fatal(err)
		}
	}
}
