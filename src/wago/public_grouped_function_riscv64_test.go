//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64PrimitiveTailTypeKeysSnapshotQuota(t *testing.T) {
	c := rv64NativeTailCompile(t, "primitive-tail-type-quota", rv64PublicReferenceTailModule(nil, false), false)
	view := c.executionView()
	keys := view.loadCompileIndexes().railshotTail.primitiveTailTypeKeys
	if len(keys) != len(view.Types) || len(keys) == 0 {
		t.Fatal("missing retained declared keys")
	}
	want := unsafe.Sizeof(uint64(0)) + unsafe.Sizeof([]uint64(nil)) + uintptr(len(keys))*unsafe.Sizeof(uint64(0))
	if got := railshotExecutionSnapshotCacheBytes(view); got != want {
		t.Fatal("snapshot declared key charge", got, want)
	}
	bytes, err := snapshotMetadataBytes(view, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.freezeExecution(bytes - 1); err == nil {
		t.Fatal("snapshot accepted an insufficient metadata quota")
	}
	if got, err := c.freezeExecution(bytes); err != nil || got != view {
		t.Fatal("exact snapshot quota", err)
	}
}

func TestRV64PublicGroupedFunctionSourceAndArtifact(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false} {
			name, carrier, params, body := "scalar", byte(0x7f), byte(0), []byte{0x41, 42, 0x0b}
			var args []uint64
			want := []uint64{42}
			if vector {
				name, carrier, params, body = "vector", 0x7b, 1, []byte{0x20, 0, 0x0b}
				args = []uint64{0x1122334455667788, 0x9988776655443322}
				want = args
			}
			t.Run(name+map[bool]string{false: "/source", true: "/artifact"}[artifact], func(t *testing.T) {
				sig := []byte{0x60, params}
				if params != 0 {
					sig = append(sig, carrier)
				}
				sig = append(sig, 1, carrier)
				types := append([]byte{1, 0x4e, 2, 0x50, 0}, sig...)
				types = append(types, 0x4f, 1, 0)
				types = append(types, sig...)
				source := wasmtest.Module(wasmtest.Section(1, types), wasmtest.Section(3, []byte{1, 1}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
				c, err := rv64RootedScalarConfig().Compile(source)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				if c.codeCache.gcI31Product != 0 {
					t.Fatal("function-only graph gained i31 execution product")
				}

				rt := NewRuntime(WithRuntimeConfig(rv64RootedScalarConfig()))
				defer rt.Close()
				mod, err := rt.Module(c)
				if err != nil {
					t.Fatal(err)
				}
				defer mod.Close()
				in, err := rt.Instantiate(context.Background(), mod)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				got, err := in.Invoke("run", args...)
				if err != nil || !slices.Equal(got, want) {
					t.Fatal(got, err)
				}
			})
		}
	}
}

func TestRV64PublicAggregateTypeDeclarationsSourceAndArtifact(t *testing.T) {
	source := wasmtest.Module(wasmtest.Section(1, []byte{1, 0x4e, 2, 0x5e, 0x63, 1, 0, 0x5f, 1, 0x63, 0, 1}))
	c, err := rv64RootedScalarConfig().Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	for _, code := range []*Compiled{c} {
		in, err := Instantiate(code)
		if err != nil {
			t.Fatal(err)
		}
		in.Close()
		if len(code.code) != 0 || len(code.Entry) != 0 || len(code.Types) != 2 {
			t.Fatal("type-only graph gained code or lost types")
		}
	}
}

func TestRV64ExecutableFunctionsWithInertAggregateGraph(t *testing.T) {
	for _, vector := range []bool{false, true} {
		t.Run(fmt.Sprintf("vector%t", vector), func(t *testing.T) {
			signature, body := []byte{0x60, 0, 1, 0x7f}, []byte{0x41, 42, 0x0b}
			args, want := []uint64(nil), []uint64{42}
			if vector {
				signature, body = []byte{0x60, 1, 0x7b, 1, 0x7b}, []byte{0x20, 0, 0x0b}
				args = []uint64{0x1122334455667788, 0x9988776655443322}
				want = args
			}
			types := append([]byte{1, 0x4e, 2}, signature...)
			types = append(types, 0x5f, 1, 0x63, 0, 0)
			source := wasmtest.Module(
				wasmtest.Section(1, types),
				wasmtest.Section(3, []byte{1, 0}),
				wasmtest.Section(6, []byte{1, 0x64, 0, 0, 0xd2, 0, 0x0b}),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
			)
			c, err := rv64RootedScalarConfig().Compile(source)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()

			for _, code := range []*Compiled{c} {
				if code.needsNativeGCABI() {
					t.Fatal("inert graph gained a native GC ABI")
				}
				in, err := Instantiate(code)
				if err != nil {
					t.Fatal(err)
				}
				if in.gc != nil {
					t.Fatal("inert graph created a collector")
				}
				got, callErr := in.Invoke("run", args...)
				in.Close()
				if callErr != nil || !slices.Equal(got, want) {
					t.Fatal(got, callErr)
				}
			}
		})
	}
}

func TestRV64EmptyStructAndInternalCollectorFunctionSourceAndArtifact(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, []byte{3, 0x5f, 0, 0x60, 1, 0x64, 0, 0, 0x60, 0, 1, 0x7f}),
		wasmtest.Section(3, []byte{2, 1, 2}),
		wasmtest.Section(6, []byte{1, 0x64, 1, 0, 0xd2, 0, 0x0b}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}), wasmtest.Code([]byte{0xfb, 1, 0, 0x1a, 0x41, 42, 0x0b}))),
	)
	c, err := rv64RootedScalarConfig().Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	for _, code := range []*Compiled{c} {
		in, err := Instantiate(code)
		if err != nil {
			t.Fatal(err)
		}
		got, err := in.Invoke("run")
		in.Close()
		if err != nil || !slices.Equal(got, []uint64{42}) {
			t.Fatal(got, err)
		}
	}
}

func TestRV64OpaqueAggregateDeclarationsWithoutExecution(t *testing.T) {
	source := wasmtest.Module(wasmtest.Section(1, []byte{1, 0x5f, 2, 0x70, 0, 0x6f, 0}))
	c, err := rv64RootedScalarConfig().Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	for _, code := range []*Compiled{c} {
		if len(code.code) != 0 || code.needsNativeGCABI() || code.needsFuncRefContextHeader {
			t.Fatal("opaque declarations gained native execution state")
		}
		in, err := Instantiate(code)
		if err != nil {
			t.Fatal(err)
		}
		in.Close()
	}
}

func TestRV64ActualI31KeepsExecutionProduct(t *testing.T) {
	c, err := rv64I31Config().Compile([]byte(rv64I31CoreSource))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.codeCache.gcI31Product == 0 {
		t.Fatal("actual i31 execution lost its product")
	}
}

func TestRV64RecursiveReferenceIndirectCallSourceAndArtifact(t *testing.T) {
	// Type 0 takes its own nullable function type. Type 1 is the public wrapper.
	source := wasmtest.Module(
		wasmtest.Section(1, []byte{2, 0x4e, 1, 0x60, 1, 0x63, 0, 1, 0x7f, 0x60, 1, 0x7f, 1, 0x7f}),
		wasmtest.Section(3, []byte{2, 0, 1}),
		wasmtest.Section(4, []byte{1, 0x70, 1, 2, 2}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(9, []byte{1, 0, 0x41, 0, 0x0b, 1, 0}),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 42, 0x0b}), wasmtest.Code([]byte{0xd0, 0, 0x20, 0, 0x11, 0, 0, 0x0b}))),
	)
	c, err := rv64RootedScalarConfig().Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	for _, code := range []*Compiled{c} {
		in, err := Instantiate(code)
		if err != nil {
			t.Fatal(err)
		}
		got, callErr := in.Invoke("run", uint64(0))
		if callErr != nil || !slices.Equal(got, []uint64{42}) {
			in.Close()
			t.Fatal(got, callErr)
		}
		got, callErr = in.Invoke("run", uint64(1))
		in.Close()
		if callErr == nil || got != nil {
			t.Fatal("null indirect target did not trap", got, callErr)
		}
	}
}

func TestRV64CovariantReferenceTailResultSourceAndArtifact(t *testing.T) {
	for _, vector := range []bool{false, true} {
		t.Run(fmt.Sprintf("vector%t", vector), func(t *testing.T) {
			functions := []byte{4, 0, 1, 2, 0}
			bodies := [][]byte{
				wasmtest.Code([]byte{0x41, 42, 0x0b}),
				wasmtest.Code([]byte{0xd2, 0, 0x0b}),
				wasmtest.Code([]byte{0xd2, 1, 0x15, 1, 0x0b}),
				wasmtest.Code([]byte{0x10, 2, 0x14, 0, 0x0b}),
			}
			if vector {
				functions[0]++
				functions = append(functions, 0)
				body := append([]byte{0xfd, 0x0c}, make([]byte, 16)...)
				body = append(body, 0x1a, 0x41, 0, 0x0b)
				bodies = append(bodies, wasmtest.Code(body))
			}
			source := wasmtest.Module(
				wasmtest.Section(1, []byte{3, 0x60, 0, 1, 0x7f, 0x60, 0, 1, 0x64, 0, 0x60, 0, 1, 0x63, 0}),
				wasmtest.Section(3, functions),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 3))),
				wasmtest.Section(9, []byte{1, 3, 0, 2, 0, 1}),
				wasmtest.Section(10, wasmtest.Vec(bodies...)),
			)
			cfg := rv64NativeTailConfig().WithCoreFeatures(platformCoreFeatures() | CoreFeatureTypedFunctionReferences | CoreFeatureTailCall)
			c, err := cfg.Compile(source)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()

			for _, code := range []*Compiled{c} {
				in, err := Instantiate(code)
				if err != nil {
					t.Fatal(err)
				}
				if !in.retainResourceRoot() {
					t.Fatal("tail type owner root")
				}
				owner := rv64TailCodeOwner{in}
				carrier := []wasm.ValType{wasm.FuncRef}
				if key, err := owner.NativeTailTypeKey(1, nil, carrier); err != nil || key != in.c.funcTypeKey(1) {
					t.Fatal("exact tail declared type", key, err)
				}
				if n := testing.AllocsPerRun(20, func() {
					if _, err := owner.NativeTailTypeKey(1, nil, carrier); err != nil {
						panic(err)
					}
				}); n != 0 {
					t.Fatal("warm declared type lookup allocated", n)
				}
				got, callErr := in.Invoke("run")
				in.Close()
				in.releaseResourceRoot()
				if callErr != nil || !slices.Equal(got, []uint64{42}) {
					t.Fatal(got, callErr)
				}
			}
		})
	}
}

func TestRV64ImportOnlyTypedFunctionSourceAndArtifact(t *testing.T) {
	types := wasmtest.Section(1, []byte{2, 0x60, 1, 0x7f, 0, 0x60, 1, 0x63, 0, 0})
	providerSource := wasmtest.Module(types,
		wasmtest.Section(3, []byte{1, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	)
	consumerSource := wasmtest.Module(types,
		wasmtest.Section(2, []byte{1, 3, 'e', 'n', 'v', 1, 'f', 0, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
	)
	for _, artifact := range []bool{false} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			var codes [2]*Compiled
			for i, source := range [][]byte{providerSource, consumerSource} {
				c, err := rv64RootedScalarConfig().Compile(source)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()

				codes[i] = c
			}
			rt := NewRuntime(WithRuntimeConfig(rv64RootedScalarConfig()))
			defer rt.Close()
			providerModule, err := rt.Module(codes[0])
			if err != nil {
				t.Fatal(err)
			}
			provider, err := rt.Instantiate(context.Background(), providerModule)
			providerModule.Close()
			if err != nil {
				t.Fatal(err)
			}
			f, err := provider.ExportedFunc("f")
			if err != nil {
				t.Fatal(err)
			}
			consumerModule, err := rt.Module(codes[1])
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("env.f", f)))
			consumerModule.Close()
			if err != nil {
				t.Fatal(err)
			}
			if got, err := consumer.Invoke("run", uint64(0)); err != nil || len(got) != 0 {
				t.Fatal(got, err)
			}
		})
	}
}
