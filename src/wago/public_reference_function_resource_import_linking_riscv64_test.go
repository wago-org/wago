//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64PublicReferenceFunctionResourceImportsLinkingRollback(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, kind := range []string{"wrong-width", "wrong-mutability", "missing-dotted-function", "initializer-high-u64"} {
				t.Run(fmt.Sprintf("artifact%t-vector%t-%s", artifact, vector, kind), func(t *testing.T) {
					rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
					defer rv64HostCertificateCloseRuntime(t, rt)
					offset := uint64(32)
					if kind == "initializer-high-u64" {
						offset = 1<<32 | 32
					}
					rc := rv64NativeTailCompile(t, fmt.Sprintf("reference-resource-rollback-provider-v%t-high%t", vector, kind == "initializer-high-u64"), wasmtest.ClosedNumericImportResourcesOffset(vector, true, offset), artifact)
					resource := rv64NativeTokenInstantiate(t, rt, rc, NewImports())
					imports, memories := rv64ReferenceResourceImportBindings(t, resource)
					pc := rv64NativeTailCompile(t, fmt.Sprintf("reference-resource-imports-v%t-ttrue-wtrue", vector), wasmtest.ClosedReferenceResourceImports(vector, true, true), artifact)
					calls := 0
					host := func(v int32) int32 { calls++; return v }
					// Real native/owned function bindings must also be released on failure.
					nc := rv64NativeTailCompile(t, "reference-imports-native", rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), artifact)
					native := rv64NativeTokenInstantiate(t, rt, nc, NewImports())
					nf, err := native.ExportedFunc("identity")
					if err != nil {
						t.Fatal(err)
					}
					owned := rv64DynamicHostBinding(t, rt, true, host, pc.importFuncSigs[2])
					imports.Function("env.calls", "native.fn", nf).Function("env.calls", "shared.fn", host).Function("env.calls", "owned.fn", owned)
					switch kind {
					case "wrong-width":
						imports.Memory("env.resources", "alias.1", memories[0])
					case "wrong-mutability":
						g, err := resource.ExportedGlobalObject("counter")
						if err != nil {
							t.Fatal(err)
						}
						imports.Global("env.resources", "seed", g)
					case "missing-dotted-function":
						imports = NewImports()
						bindings, _ := rv64ReferenceResourceImportBindings(t, resource)
						imports = bindings.Function("env", "calls.native.fn", nf).Function("env.calls", "shared.fn", host).Function("env.calls", "owned.fn", owned)
					}
					beforeResource, beforeNative := resource.resourceRefs, native.resourceRefs
					mod, err := rt.Module(pc)
					if err != nil {
						t.Fatal(err)
					}
					in, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
					mod.Close()
					if in != nil {
						in.Close()
					}
					if in != nil || err == nil || calls != 0 {
						t.Fatal("bad resource import instantiated", kind, err, calls)
					}
					if kind == "initializer-high-u64" && !strings.Contains(err.Error(), "out of bounds") {
						t.Fatal("full-u64 initializer rejection", err)
					}
					if resource.resourceRefs != beforeResource || native.resourceRefs != beforeNative {
						t.Fatal("failed resource link retained physical roots", resource.resourceRefs, beforeResource, native.resourceRefs, beforeNative)
					}
					if cc := pc.loadCodeCache(); cc != nil && cc.refs != 0 {
						t.Fatal("failed resource link retained code reference")
					}
					if err := owned.(*HostFuncRef).Close(); err != nil {
						t.Fatal("failed resource link retained owned host", err)
					}
					if err := resource.Close(); err != nil {
						t.Fatal(err)
					}
					if err := native.Close(); err != nil {
						t.Fatal(err)
					}
					if !resource.resourcesClosed || !native.resourcesClosed {
						t.Fatal("failed resource link retained provider")
					}
				})
			}
		}
	}
}

func rv64ReferenceGlobalAliasImports() []byte {
	types := [][]byte{wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.FuncRef, wasm.I32}, []wasm.ValType{wasm.FuncRef})}
	body := []byte{0x20, 1, 0x24, 0, 0x23, 1, 0x20, 1, 0x47, 4, 0x40, 0, 0x0b, 0x20, 0, 0x0b}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env.resources", "counter.a", wasm.I32, true), wasmtest.GlobalImportEntry("env.resources", "counter.b", wasm.I32, true))), wasmtest.Section(3, []byte{2, 0, 1}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("a", 3, 0), wasmtest.ExportEntry("b", 3, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd0, 0x70, 0x20, 0, 0x10, 1, 0xd1, 0x0b}), wasmtest.Code(body))))
}

func TestRV64PublicReferenceFunctionResourceImportsGlobalAliases(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			rc := rv64NativeTailCompile(t, "reference-resource-provider-vfalse-wtrue", wasmtest.ClosedNumericImportResources(false, true), artifact)
			resource := rv64NativeTokenInstantiate(t, rt, rc, NewImports())
			global, err := resource.ExportedGlobalObject("counter")
			if err != nil {
				t.Fatal(err)
			}
			pc := rv64NativeTailCompile(t, "reference-resource-global-alias", rv64ReferenceGlobalAliasImports(), artifact)
			producer := rv64NativeTokenInstantiate(t, rt, pc, NewImports().Global("env.resources", "counter.a", global).Global("env.resources", "counter.b", global))
			if producer.globalCells[0] != producer.globalCells[1] {
				t.Fatal("global alias cells differ")
			}
			if err := resource.Close(); err != nil {
				t.Fatal(err)
			}
			if err := rc.Close(); err != nil {
				t.Fatal(err)
			}
			if resource.resourcesClosed {
				t.Fatal("numeric global-only importer lost resource provider")
			}
			if got, err := producer.Invoke("run", 0xdeadbeef12345678); err != nil || !slices.Equal(got, []uint64{1}) || readGlobalObject(producer.globalCells[1], ValI32) != 0x12345678 {
				t.Fatal("aliased global reference call", got, err)
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			if !resource.resourcesClosed {
				t.Fatal("global-only final root retained")
			}
		})
	}
}
