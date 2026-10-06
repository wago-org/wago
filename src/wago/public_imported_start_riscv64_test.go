//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64PublicImportedStartModule(vector bool) []byte {
	imports := [][]byte{}
	if vector {
		imports = append(imports, wasmtest.GlobalImportEntry("env", "v", wasm.V128, true))
	}
	imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("unused")...), 0, 2), append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, 1, 1, 4), append(append(wasmtest.Name("env"), wasmtest.Name("start")...), 0, 0), wasmtest.GlobalImportEntry("env", "flag", wasm.I32, true))
	results := []wasm.ValType{wasm.I32}
	flag, counter := byte(0), byte(1)
	globals := [][]byte{wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 5, 0x0b})}
	if vector {
		flag, counter = 1, 2
		results = append(results, wasm.V128)
		globals = append(globals, wasmtest.GlobalEntry(wasm.V128, true, append(append([]byte{0xfd, 12}, make([]byte, 16)...), 0x0b)))
	}
	initialize := []byte{0x23, counter, 0x41, 0, 0x2d, 0, 0, 0x6a, 0x23, flag, 0x6a, 0x24, counter, 0x23, flag, 0x41, 1, 0x6a, 0x24, flag, 0x41, 1, 0x23, counter, 0x3a, 0, 0}
	get := []byte{0x23, counter}
	if vector {
		initialize = append(initialize, 0x23, 0, 0x24, 3)
		get = append(get, 0x23, 3)
	}
	initialize, get = append(initialize, 0x0b), append(get, 0x0b)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, results), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{2, 0, 1}), wasmtest.Section(6, wasmtest.Vec(globals...)), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("initialize", 0, 2), wasmtest.ExportEntry("get", 0, 3))), wasmtest.Section(8, []byte{1}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(initialize), wasmtest.Code(get))), wasmtest.Section(11, []byte{1, 0, 0x41, 0, 0x0b, 3, 7, 0, 0}))
}

func TestRV64PublicImportedHostStartStateAndLifecycle(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	for _, vectorProfile := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			for _, ownedRuntime := range []bool{false, true} {
				for _, behavior := range []string{"success", "pre-cancel", "cancel", "instance-close", "instance-self", "runtime-close", "runtime-self", "panic", "exit", "owned-handle", "owned-cancel", "owned-instance-close", "owned-runtime-close"} {
					if !ownedRuntime && (behavior == "runtime-close" || behavior == "runtime-self" || strings.HasPrefix(behavior, "owned-")) {
						continue
					}
					t.Run(fmt.Sprintf("vector%t-artifact%t-runtime%t-%s", vectorProfile, artifact, ownedRuntime, behavior), func(t *testing.T) {
						ownedHandle := strings.HasPrefix(behavior, "owned-")
						behavior := behavior
						if ownedHandle && behavior != "owned-handle" {
							behavior = strings.TrimPrefix(behavior, "owned-")
						}

						producer, memory, vector, flag, imports := rv64PublicStartImports(t)
						c := rv64PublicStartCompile(t, rv64PublicImportedStartModule(vectorProfile), artifact)
						if !c.HasStart || !c.StartIsImport || c.StartImportIdx != 1 || c.NumImports != 2 {
							t.Fatal("interleaved imported start namespace")
						}
						imports.Function("env", "unused", HostCallFunc(func(call HostCall) { panic("unused host called") }))
						var rt *Runtime
						var mod *Module
						resolver := new(CallerResolver)
						if ownedRuntime {
							rt = NewRuntime()
							defer rt.Close()
							resolver.rt.Store(rt)
							var err error
							mod, err = rt.Module(c)
							if err != nil {
								t.Fatal(err)
							}
							defer mod.Close()
						}
						type contextKey struct{}
						ctx, cancel := context.WithCancel(context.WithValue(context.Background(), contextKey{}, "private-value"))
						defer cancel()
						if behavior == "pre-cancel" {
							cancel()
						}
						cause := errors.New("imported start host trap")
						entered, release := make(chan *Instance, 1), make(chan struct{})
						var once sync.Once
						unblock := func() { once.Do(func() { close(release) }) }
						var callbackContext context.Context
						calls := 0
						host := CallerHostCallFunc(func(caller Caller, call HostCall) {
							calls++
							if calls != 1 || call.ParamCount() != 0 || call.ResultCount() != 0 || caller.Memory()[0] != 7 || caller.Memory()[1] != 0 || flag.Get() != 9 {
								panic("imported start initialization order")
							}
							if ownedRuntime {
								var err error
								if mayCancel, err := resolver.InvocationMayCancel(caller); err != nil || !mayCancel {
									panic("imported start lost cancellation context generation")
								}
								callbackContext, err = resolver.InvocationContext(caller)
								if err != nil || callbackContext.Value(contextKey{}) != nil {
									panic("imported start context authority")
								}
								again, err := resolver.InvocationContext(caller)
								if err != nil || callbackContext != again {
									panic("imported start unstable callback context")
								}
							}
							if _, err := caller.in.Invoke("initialize"); err != nil {
								panic(HostTrap{Err: err})
							}
							if _, err := producer.Invoke("bump"); err != nil {
								panic(HostTrap{Err: err})
							}
							runtime.GC()
							switch behavior {
							case "panic":
								panic(HostTrap{Err: cause})
							case "exit":
								panic(HostExit{Code: 17})
							case "instance-self":
								if err := caller.in.Close(); err != nil {
									panic(HostTrap{Err: err})
								}
							case "runtime-self":
								if err := rt.Close(); err != nil {
									panic(HostTrap{Err: err})
								}
							}
							if behavior != "success" && behavior != "owned-handle" {
								entered <- caller.in
								<-release
							}
						})
						var handle *HostFuncRef
						if ownedHandle {
							var err error
							handle, err = rt.NewHostFuncRef(host, FuncSig{})
							if err != nil {
								t.Fatal(err)
							}
							defer handle.Close()
							imports.Function("env", "start", handle)
						} else {
							imports.Function("env", "start", host)
						}
						type outcome struct {
							in  *Instance
							err error
						}
						done := make(chan outcome, 1)
						joined := false
						defer func() {
							cancel()
							unblock()
							if !joined {
								result := <-done
								if result.in != nil {
									_ = result.in.Close()
								}
							}
						}()
						go func() {
							var in *Instance
							var err error
							if ownedRuntime {
								in, err = rt.Instantiate(ctx, mod, WithImports(imports))
							} else {
								in, err = Instantiate(c, InstantiateOptions{Imports: imports, startContext: ctx})
							}
							done <- outcome{in, err}
						}()
						parked := behavior == "cancel" || behavior == "instance-close" || behavior == "instance-self" || behavior == "runtime-close" || behavior == "runtime-self"
						if parked {
							var active *Instance
							select {
							case active = <-entered:
							case <-time.After(5 * time.Second):
								t.Fatal("imported start did not park")
							}
							switch behavior {
							case "cancel":
								cancel()
								if ownedRuntime {
									select {
									case <-callbackContext.Done():
									case <-time.After(5 * time.Second):
										t.Fatal("start callback missed parent cancellation")
									}
								}
							case "instance-close":
								if err := active.Close(); err != nil {
									t.Fatal(err)
								}
							case "runtime-close":
								if err := rt.Close(); err != nil {
									t.Fatal(err)
								}
							}
							active.lifeMu.Lock()
							released := active.resourcesClosed || !active.constructionActive || active.eng == nil
							active.lifeMu.Unlock()
							if released || active.invocationState.Load()&instanceInvocationCount == 0 {
								t.Fatal("parked imported start lost resource lease")
							}
							if handle != nil {
								handle.mu.Lock()
								live := handle.importers == 1 && handle.fn != nil
								handle.mu.Unlock()
								if !live || handle.Close() == nil {
									t.Fatal("parked imported start lost owned callback root")
								}
							}
							unblock()
						}
						var result outcome
						select {
						case result = <-done:
							joined = true
						case <-time.After(5 * time.Second):
							t.Fatal("imported start failed to unwind")
						}
						success := behavior == "success" || behavior == "owned-handle"
						if success {
							if result.err != nil || result.in == nil {
								t.Fatal("imported start", result.err)
							}
							defer result.in.Close()
							expected := []uint64{21}
							if vectorProfile {
								expected = append(expected, 0x80000000deadbeef, 0x7ff0000000000001)
							}
							for range 3 {
								importedGlobalInvoke(t, result.in, "get", expected)
							}
							if err := result.in.Close(); err != nil {
								t.Fatal(err)
							}
						} else {
							if result.in != nil {
								defer result.in.Close()
								t.Fatal("failed imported start returned instance")
							}
							switch behavior {
							case "pre-cancel", "cancel":
								if !errors.Is(result.err, context.Canceled) {
									t.Fatal("imported start cancellation cause", result.err)
								}
							case "panic":
								if !errors.Is(result.err, cause) {
									t.Fatal("imported start panic cause", result.err)
								}
							case "exit":
								var exit *ExitError
								if !errors.As(result.err, &exit) || exit.Code != 17 {
									t.Fatal("imported start exit", result.err)
								}
							default:
								if result.err == nil || !strings.Contains(result.err.Error(), "closed") {
									t.Fatal("imported start closure", result.err)
								}
							}
						}
						if ownedRuntime && behavior != "pre-cancel" {
							select {
							case <-callbackContext.Done():
							default:
								t.Fatal("completed start callback context remained valid")
							}
						}
						if behavior == "runtime-close" || behavior == "runtime-self" {
							wait, stop := context.WithTimeout(context.Background(), 5*time.Second)
							defer stop()
							if err := rt.WaitClosed(wait); err != nil {
								t.Fatal(err)
							}
						}
						if behavior == "pre-cancel" {
							if calls != 0 || flag.Get() != 9 || memory.UnsafeBytes()[0] != 0 {
								t.Fatal("pre-cancelled imported start side effects")
							}
						} else if calls != 1 || flag.Get() != 10 || memory.UnsafeBytes()[1] != 21 || memory.UnsafeBytes()[2] != 0 {
							t.Fatal("imported start effect/order")
						}
						if handle != nil {
							if err := handle.Close(); err != nil {
								t.Fatal("joined imported start callback roots", err)
							}
							handle.mu.Lock()
							retained := handle.importers != 0 || handle.fn != nil
							handle.mu.Unlock()
							if retained {
								t.Fatal("joined imported start retained callback")
							}
						}
						if err := vector.Close(); err != nil {
							t.Fatal("imported start vector roots", err)
						}
						if err := flag.Close(); err != nil {
							t.Fatal("imported start scalar roots", err)
						}
						if err := producer.Close(); err != nil || memory.jobMemory() != nil {
							t.Fatal("imported start memory roots", err)
						}
					})
				}
			}
		}
	}
}

func TestRV64PublicImportedStartClosedForeignTargetRejectedBeforePublication(t *testing.T) {
	producerCode := rv64PublicStartCompile(t, wasmtest.Module(wasmtest.Section(1, []byte{1, 0x60, 0, 0}), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("noop", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b})))), false)
	target, err := Instantiate(producerCode)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	export, err := target.ExportedFunc("noop")
	if err != nil {
		t.Fatal(err)
	}
	if err := target.Close(); err != nil {
		t.Fatal(err)
	}
	for _, artifact := range []bool{false, true} {
		producer, _, _, _, imports := rv64PublicStartImports(t)
		defer producer.Close()
		imports.Function("env", "unused", HostCallFunc(func(HostCall) {})).Function("env", "start", export)
		c := rv64PublicStartCompile(t, rv64PublicImportedStartModule(true), artifact)
		before := target.referenceLifetime().snapshot().ResourceRoots
		beforeBase, beforeSealed := c.codeCache.base, c.codeCache.sealed
		in, err := Instantiate(c, imports)
		if in != nil {
			defer in.Close()
			t.Fatal("foreign start returned instance")
		}
		if err == nil || !strings.Contains(err.Error(), "closed") || c.codeCache.base != beforeBase || c.codeCache.sealed != beforeSealed || target.referenceLifetime().snapshot().ResourceRoots != before {
			t.Fatal("foreign start published/retained target", err)
		}
	}
}
