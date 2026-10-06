//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64PublicImportOnlyModule(types []wasm.ValType, shared, start, exported bool, vectorMarker ...bool) []byte {
	imports := [][]byte{wasmtest.GlobalImportEntry("env", "flag", wasm.I32, true), rv64VectorFunctionImport("unused", 1)}
	if shared {
		imports = append(imports, rv64VectorMemoryImport())
	}
	imports = append(imports, rv64VectorFunctionImport("target", 0))
	typeEntries := [][]byte{wasmtest.FuncType(types, types), wasmtest.FuncType(nil, nil)}
	if len(vectorMarker) != 0 && vectorMarker[0] {
		typeEntries = append(typeEntries, wasmtest.FuncType([]wasm.ValType{wasm.V128}, nil))
	}
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(typeEntries...)),
		wasmtest.Section(2, wasmtest.Vec(imports...)),
	}
	if exported {
		sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("relay", 0, 1), wasmtest.ExportEntry("alias", 0, 1))))
	}
	if start {
		sections = append(sections, wasmtest.Section(8, []byte{1}))
	}
	return wasmtest.Module(sections...)
}

func TestRV64PublicImportOnlyNativeTransportAndLifetime(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, shared := range []bool{false, true} {
			for _, shape := range []string{"void", "mixed4", "scalar64", "vector128"} {
				t.Run(fmt.Sprintf("artifact%t-shared%t-%s", artifact, shared, shape), func(t *testing.T) {
					count := map[string]int{"void": 0, "mixed4": 4, "scalar64": 64, "vector128": 128}[shape]
					types := make([]wasm.ValType, count)
					for i := range types {
						types[i] = []wasm.ValType{wasm.I32, wasm.F32, wasm.I64, wasm.F64}[i%4]
						if shape == "vector128" {
							types[i] = wasm.V128
						}
					}
					input, want := rv64NativeVectorInputs(types)
					flag, fail := NewGlobalI32(0, true), NewGlobalI32(0, true)
					defer flag.Close()
					defer fail.Close()
					var memory *Memory
					if shared {
						_, memory = importedMemoryProvider(t)
						defer memory.Close()
					}
					producerImports := NewImports().Global("env", "fail", fail)
					if shared {
						producerImports.Memory("env", "memory", memory)
					}
					producer, err := Instantiate(rv64PublicStartCompile(t, rv64NativeVectorProducer(types, shared), artifact), producerImports)
					if err != nil {
						t.Fatal(err)
					}
					defer producer.Close()
					fn, err := producer.ExportedFunc("identity")
					if err != nil {
						t.Fatal(err)
					}
					c := rv64PublicStartCompile(t, rv64PublicImportOnlyModule(types, shared, false, true), artifact)
					if len(c.Funcs) != 0 || len(c.Entry) != 0 || len(c.InternalEntry) != 0 || len(c.code) != 0 || c.NumImports != 2 {
						t.Fatal("import-only artifact acquired a defined code entry")
					}
					imports := NewImports().Global("env", "flag", flag).Function("env", "unused", func() { panic("unused import executed") }).Function("env", "target", fn)
					if shared {
						imports.Memory("env", "memory", memory)
					}
					relay, err := Instantiate(c, imports)
					if err != nil {
						t.Fatal(err)
					}
					defer relay.Close()
					if err := c.Close(); err != nil {
						t.Fatal(err)
					}
					runtime.GC()
					if c.codeCache.refs != 1 || c.codeCache.mem == nil {
						t.Fatal("live import-only relay lost its closed code owner")
					}
					if err := producer.Close(); err != nil {
						t.Fatal(err)
					}
					for _, export := range []string{"relay", "alias"} {
						for range 3 {
							got, err := relay.Invoke(export, input...)
							if err != nil || !slices.Equal(got, want) {
								t.Fatal(export, got, err)
							}
						}
					}
					fail.Set(1)
					if _, err := relay.Invoke("relay", input...); err == nil {
						t.Fatal("producer trap was lost")
					}
					fail.Set(0)
					if got, err := relay.Invoke("relay", input...); err != nil || !slices.Equal(got, want) {
						t.Fatal("cold reuse", got, err)
					}
					if _, err := relay.WasmFunc("relay"); err == nil {
						t.Fatal("unqualified prepared reexport was admitted")
					}
					if err := relay.Close(); err != nil || producer.jm != nil {
						t.Fatal("last attachment did not release producer", err)
					}
					if c.codeCache.refs != 0 || c.codeCache.mem != nil || c.codeCache.base != 0 {
						t.Fatal("import-only mapping survived final instance release")
					}
				})
			}
		}
	}
}

func TestRV64PublicImportOnlyStartAndBindingGates(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			for _, kind := range []string{"go", "handle", "native"} {
				for _, trap := range []bool{false, true} {
					t.Run(fmt.Sprintf("vector%t-artifact%t-%s-trap%t", vector, artifact, kind, trap), func(t *testing.T) {
						flag := NewGlobalI32(0, true)
						defer flag.Close()
						cause := errors.New("import-only start host trap")
						calls := 0
						var expired Caller
						host := CallerHostCallFunc(func(caller Caller, call HostCall) {
							active, valid := resolveHostCaller(caller)
							if caller.in == nil || call.ParamCount() != 0 || call.ResultCount() != 0 || !valid || !active.valid() {
								panic("import-only start callback state")
							}
							calls++
							expired = caller
							runtime.GC()
							if trap {
								panic(HostTrap{Err: cause})
							}
						})
						var binding any = host
						var rt *Runtime
						if kind == "handle" {
							rt = NewRuntime()
							defer rt.Close()
							handle, err := rt.NewHostFuncRef(host, FuncSig{})
							if err != nil {
								t.Fatal(err)
							}
							defer handle.Close()
							binding = handle
						}
						if kind == "native" {
							producerSource := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))), wasmtest.Section(2, wasmtest.Vec(rv64VectorFunctionImport("host", 0))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("boot", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x0b}))))
							producer, err := Instantiate(rv64PublicStartCompile(t, producerSource, artifact), NewImports().Function("env", "host", host))
							if err != nil {
								t.Fatal(err)
							}
							defer producer.Close()
							binding, err = producer.ExportedFunc("boot")
							if err != nil {
								t.Fatal(err)
							}
						}
						c := rv64PublicStartCompile(t, rv64PublicImportOnlyModule(nil, false, true, false, vector), artifact)
						if c.requiredFeatures.IsEnabled(CoreFeatureSIMD) != vector {
							t.Fatal("unused SIMD type did not select the exact public profile")
						}
						imports := NewImports().Global("env", "flag", flag).Function("env", "unused", func() { panic("unused start") }).Function("env", "target", binding)
						instantiate := func(ctx context.Context, compiled *Compiled) (*Instance, error) {
							if rt == nil {
								return Instantiate(compiled, InstantiateOptions{Imports: imports, startContext: ctx})
							}
							mod, err := rt.Module(compiled)
							if err != nil {
								return nil, err
							}
							defer mod.Close()
							return rt.Instantiate(ctx, mod, WithImports(imports))
						}
						ctx, cancel := context.WithCancel(context.Background())
						cancel()
						if in, err := instantiate(ctx, c); in != nil || !errors.Is(err, context.Canceled) || calls != 0 {
							t.Fatal("pre-cancel", in, err, calls)
						}
						for attempt := 1; attempt <= 2; attempt++ {
							in, err := instantiate(context.Background(), c)
							if trap {
								if in != nil || !errors.Is(err, cause) {
									t.Fatal("start trap", in, err)
								}
							} else {
								if err != nil || in == nil {
									t.Fatal("start", err)
								}
								if err := in.Close(); err != nil {
									t.Fatal(err)
								}
							}
							active, valid := resolveHostCaller(expired)
							if calls != attempt || valid && active.valid() {
								t.Fatal("start once/expiry", calls)
							}
						}
						if kind != "native" {
							exported := rv64PublicStartCompile(t, rv64PublicImportOnlyModule(nil, false, false, true, vector), artifact)
							before := exported.codeCache.base
							if in, err := instantiate(context.Background(), exported); in != nil || err == nil || exported.codeCache.base != before {
								t.Fatal("host reexport escaped pre-mapping gate", in, err)
							}
						}
					})
				}
			}
		}
	}
}
