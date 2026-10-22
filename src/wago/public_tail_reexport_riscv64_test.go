//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64PublicTailNativeReexportEntry(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, shared := range []bool{false, true} {
			for _, shape := range []string{"zero", "mixed", "pair"} {
				kinds := []string{"native"}
				if shape != "pair" {
					kinds = append(kinds, "go", "owned")
				}
				for _, kind := range kinds {
					t.Run(fmt.Sprintf("artifact%t-shared%t-%s-%s", artifact, shared, shape, kind), func(t *testing.T) {
						types := []wasm.ValType{}
						if shape == "mixed" {
							types = []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}
						}
						if shape == "pair" {
							types = []wasm.ValType{wasm.I32, wasm.V128, wasm.F32}
						}
						input, want := rv64NativeVectorInputs(types)
						rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig().WithNativeStackBytes(MinNativeStackBytes)))
						defer rt.Close()
						var memory *Memory
						if shared {
							var err error
							memory, err = NewSharedMemory(1, 4)
							if err != nil {
								t.Fatal(err)
							}
							defer memory.Close()
						}
						var binding any
						var leaf, relay *Instance
						depth, calls := 0, 0
						fail := false
						cause := errors.New("proper tail from native reexport")
						host := CallerHostCallFunc(func(caller Caller, call HostCall) {
							calls++
							if caller.in != leaf || !slices.Equal(call.ParamSlots(), want) {
								panic("reexport tail host namespace/transport")
							}
							if depth == 0 {
								view := rv64InvocationViewForTest(leaf)
								before := bytes.Clone(view)
								depth++
								got, err := relay.InvokeFromHost(context.Background(), caller, "alias", input...)
								depth--
								if err != nil || !slices.Equal(got, want) || !bytes.Equal(view, before) {
									panic(fmt.Sprintf("code-free proper-tail relay reentry: %v %v", got, err))
								}
								runtime.GC()
								if fail {
									panic(HostTrap{Err: cause})
								}
							}
							copy(call.ResultSlots(), call.ParamSlots())
						})
						key := fmt.Sprintf("tail-reexport-%s-shared%t", shape, shared)
						var identity *Instance
						if kind == "native" {
							pc := rv64NativeTailCompile(t, key+"-identity", rv64NativeVectorProducer(types, shared), artifact)
							failCell := NewGlobalI32(0, true)
							defer failCell.Close()
							identityImports := NewImports().Global("env", "fail", failCell)
							if shared {
								identityImports.Memory("env", "memory", memory)
							}
							identity = rv64HostTailInstantiate(t, rt, pc, identityImports)
							ex, err := identity.ExportedFunc("identity")
							if err != nil {
								t.Fatal(err)
							}
							binding = ex
						} else if kind == "owned" {
							values := valTypesFromWasm(types)
							owner, err := rt.NewHostFuncRef(host, FuncSig{Params: values, Results: values})
							if err != nil {
								t.Fatal(err)
							}
							defer owner.Close()
							binding = owner
						} else {
							binding = host
						}
						// The producer's framed direct tail carries a real source plan. Relay
						// entry itself is an owned import bridge, without a defined guest entry.
						imports := testImports("env.tail", binding)
						if shared {
							imports.Memory("env", "memory", memory)
						}
						leaf = rv64HostTailInstantiate(t, rt, rv64NativeTailCompile(t, key+"-leaf", rv64TailReexportProducer(types, shared), artifact), imports)
						ex, err := leaf.ExportedFunc("call")
						if err != nil {
							t.Fatal(err)
						}
						flag := NewGlobalI32(0, true)
						defer flag.Close()
						imports = NewImports().Global("env", "flag", flag).Function("env", "unused", func() { panic("unused relay host import") }).Function("env", "target", ex)
						if shared {
							imports.Memory("env", "memory", memory)
						}
						rc := rv64NativeTailCompile(t, key+"-relay", rv64PublicImportOnlyModule(types, shared, false, true, shape == "pair"), artifact)
						if len(rc.code) != 0 || len(rc.Entry) != 0 || len(rc.InternalEntry) != 0 || len(rc.Funcs) != 0 {
							t.Fatal("relay acquired a defined machine-code entry")
						}
						relay = rv64HostTailInstantiate(t, rt, rc, imports)
						if shared && (leaf.jm.LinMemBase() != relay.jm.LinMemBase() || identity != nil && identity.jm.LinMemBase() != relay.jm.LinMemBase()) {
							t.Fatal("reexport tail did not share native home")
						}
						if !shared && (leaf.jm.LinMemBase() == relay.jm.LinMemBase() || identity != nil && (identity.jm.LinMemBase() == leaf.jm.LinMemBase() || identity.jm.LinMemBase() == relay.jm.LinMemBase())) {
							t.Fatal("distinct reexport tail homes alias")
						}
						for i := 0; i < 3; i++ {
							got, err := relay.Invoke("relay", input...)
							if err != nil || !slices.Equal(got, want) {
								t.Fatal("native reexport to proper tail", got, err)
							}
							got, err = relay.Invoke("alias", input...)
							if err != nil || !slices.Equal(got, want) {
								t.Fatal("proper-tail alias entry", got, err)
							}
						}
						if kind != "native" {
							fail = true
							if _, err := relay.Invoke("relay", input...); !errors.Is(err, cause) {
								t.Fatal("proper-tail reexport cold trap", err)
							}
							fail = false
							if got, err := relay.Invoke("alias", input...); err != nil || !slices.Equal(got, want) {
								t.Fatal("proper-tail reexport cold reuse", got, err)
							}
							if calls != 16 {
								t.Fatal("proper-tail reexport nested host count", calls)
							}
						}
						if _, err := relay.Invoke("relay", append(slices.Clone(input), 99)...); err == nil {
							t.Fatal("reexport tail wrong root arity")
						}
						ctx, cancel := context.WithCancel(context.Background())
						cancel()
						if _, err := relay.InvokeContext(ctx, "relay", input...); !errors.Is(err, context.Canceled) {
							t.Fatal("reexport tail precancellation", err)
						}
						if err := leaf.c.Close(); err != nil {
							t.Fatal(err)
						}
						if err := rc.Close(); err != nil {
							t.Fatal(err)
						}
						if err := leaf.Close(); err != nil {
							t.Fatal(err)
						}
						if identity != nil {
							if err := identity.Close(); err != nil {
								t.Fatal(err)
							}
						}
						if leaf.jm == nil || leaf.pluginState.Load().railshotInvocation.tailDispatcher == nil {
							t.Fatal("retained relay released tail producer")
						}
						if got, err := relay.Invoke("alias", input...); err != nil || !slices.Equal(got, want) {
							t.Fatal("logically closed attached tail producer", got, err)
						}
						if err := relay.Close(); err != nil {
							t.Fatal(err)
						}
						if leaf.jm != nil || leaf.pluginState.Load().railshotInvocation.tailDispatcher != nil || identity != nil && identity.jm != nil {
							t.Fatal("final relay retained tail graph")
						}
					})
				}
			}
		}
	}
}

func rv64TailReexportProducer(types []wasm.ValType, shared bool) []byte {
	imports := [][]byte{append(append(wasmtest.Name("env"), wasmtest.Name("tail")...), 0, 0)}
	if shared {
		imports = append(imports, rv64VectorMemoryImport())
	}
	body := []byte{}
	for i := range types {
		body = append(body, 0x20, byte(i))
	}
	body = append(body, 0x12, 0, 0x0b)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types))), wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
}
