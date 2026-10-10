//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64PublicNativeStartProducerModule(shared bool, tail []byte) []byte {
	imports := [][]byte{wasmtest.GlobalImportEntry("env", "flag", wasm.I32, true)}
	if shared {
		imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, 1, 1, 4))
	}
	imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("host")...), 0, 0))
	body := append([]byte{0x10, 0}, tail...)
	body = append(body, 0x23, 0, 0x41, 1, 0x6a, 0x24, 0)
	if shared {
		body = append(body, 0x41, 2, 0x41, 0xe3, 0, 0x3a, 0, 0)
	}
	body = append(body, 0x0b)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{2, 0, 1}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("producer_boot", 0, 1), wasmtest.ExportEntry("safe", 0, 2))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code([]byte{0x41, 23, 0x0b}))))
}

func rv64PublicNativeStartHostPanicProducerModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "f", 0, 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("boot", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x0b}))),
	)
}

func TestRV64PublicNativeImportedStartHostPanicCompletes(t *testing.T) {
	cases := []struct {
		name  string
		value any
	}{{name: "value", value: "native imported start panic"}}
	if os.Getenv("WAGO_NATIVE_IMPORTED_START_NIL_PANIC") == "1" {
		cases = append(cases, struct {
			name  string
			value any
		}{name: "nil", value: nil})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fail := true
			calls := 0
			var callerToken Caller
			producerCode := rv64PublicStartCompile(t, rv64PublicNativeStartHostPanicProducerModule(), false)
			producer, err := Instantiate(producerCode, NewImports().Function("env", "f", CallerHostCallFunc(func(caller Caller, _ HostCall) {
				calls++
				callerToken = caller
				if fail {
					panic(tc.value)
				}
			})))
			if err != nil {
				t.Fatal(err)
			}
			defer producer.Close()
			export, err := producer.ExportedFunc("boot")
			if err != nil {
				t.Fatal(err)
			}
			consumerCode := rv64PublicStartCompile(t, invocationContextImportedStartModule(), false)
			beforeRoots := producer.referenceLifetime().snapshot().ResourceRoots
			var failed *Instance
			in, err := Instantiate(consumerCode, InstantiateOptions{
				Imports:     testImports("env.outer", export),
				afterCreate: func(in *Instance) error { failed = in; return nil },
			})
			if in != nil {
				defer in.Close()
			}
			if in != nil || err == nil || calls != 1 || !strings.Contains(err.Error(), "wago: instantiation panicked after instance creation") || !strings.Contains(err.Error(), fmt.Sprint(tc.value)) {
				t.Fatalf("native imported start panic = (%p, %v, calls=%d)", in, err, calls)
			}
			if failed == nil || callerToken.valid() || !failed.resourcesClosed || failed.constructionIsActive() || failed.invocationState.Load()&instanceInvocationCount != 0 || failed.jm != nil || failed.thunkMem != nil || producer.referenceLifetime().snapshot().ResourceRoots != beforeRoots || producer.invocationState.Load()&instanceInvocationCount != 0 {
				t.Fatal("native imported start panic retained caller, constructor, native resources, or producer attachment")
			}
			if state := failed.pluginState.Load(); state != nil && (state.invokeMu.state.Load()&invocationGateHeld != 0 || state.invocationID != 0) {
				t.Fatal("native imported start panic retained consumer invocation gate")
			}
			if state := producer.pluginState.Load(); state != nil && (state.invokeMu.state.Load()&invocationGateHeld != 0 || state.invocationID != 0) {
				t.Fatal("native imported start panic retained producer invocation gate")
			}

			fail = false
			if got, err := producer.Invoke("boot"); err != nil || len(got) != 0 {
				t.Fatal("producer not reusable after failed native imported start", got, err)
			}
			retry, err := Instantiate(consumerCode, testImports("env.outer", export))
			if retry != nil {
				defer func() {
					if retry != nil {
						_ = retry.Close()
					}
				}()
			}
			if err != nil || retry == nil {
				t.Fatal("consumer not reusable after failed native imported start", retry, err)
			}
			if err := retry.Close(); err != nil {
				t.Fatal(err)
			}
			retry = nil
			if calls != 3 || producer.referenceLifetime().snapshot().ResourceRoots != beforeRoots {
				t.Fatal("native imported start retry did not balance calls or attachment", calls, producer.referenceLifetime().snapshot().ResourceRoots)
			}
		})
	}
}

func TestRV64PublicNativeImportedStartStateReentryAndLifecycle(t *testing.T) {
	for _, shared := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			for _, managed := range []bool{false, true} {
				for _, behavior := range []string{"success", "pre-cancel", "cancel", "importer-close", "runtime-close", "producer-self-close", "host-trap", "host-exit", "producer-trap"} {
					if behavior == "runtime-close" && !managed {
						continue
					}
					t.Run(fmt.Sprintf("shared%t-artifact%t-runtime%t-%s", shared, artifact, managed, behavior), func(t *testing.T) {
						_, memory, _, flag, imports := rv64PublicStartImports(t)
						consumerCode := rv64PublicStartCompile(t, rv64PublicImportedStartModule(true), artifact)
						if consumerCode.StartImportIdx != 1 || !consumerCode.StartIsImport || !consumerCode.HasStart {
							t.Fatal("interleaved native start namespace")
						}
						var active *Instance
						created, terminal, failed := 0, 0, 0
						capture := func(in *Instance) error { active = in; created++; return nil }
						var rt *Runtime
						var mod *Module
						if managed {
							rt = NewRuntime()
							defer rt.Close()
							var err error
							mod, err = rt.Module(consumerCode)
							if err != nil {
								t.Fatal(err)
							}
							defer mod.Close()
						}
						type contextKey struct{}
						ctx, cancel := context.WithCancel(context.WithValue(context.Background(), contextKey{}, "private"))
						defer cancel()
						if behavior == "pre-cancel" {
							cancel()
						}
						entered, release := make(chan struct{}, 1), make(chan struct{})
						var once sync.Once
						unblock := func() { once.Do(func() { close(release) }) }
						cause := errors.New("native imported start host trap")
						var producer *Instance
						calls := 0
						var callbackContext context.Context
						resolver := new(CallerResolver)
						if managed {
							resolver.rt.Store(rt)
						}
						host := CallerHostCallFunc(func(caller Caller, call HostCall) {
							calls++
							if active == nil || calls != 1 || call.ParamCount() != 0 || call.ResultCount() != 0 || active.Memory().UnsafeBytes()[0] != 7 || flag.Get() != 9 {
								panic("native start initializer ordering")
							}
							if shared && caller.Memory()[0] != 7 {
								panic("native start shared-home selection")
							}
							if got, err := active.InvokeFromHost(context.Background(), caller, "initialize"); err != nil || len(got) != 0 {
								panic(HostTrap{Err: err})
							}
							if got, err := producer.InvokeFromHost(context.Background(), caller, "safe"); err != nil || !slices.Equal(got, []uint64{23}) {
								panic(HostTrap{Err: err})
							}
							if managed {
								var err error
								callbackContext, err = resolver.InvocationContext(caller)
								if err != nil || callbackContext.Value(contextKey{}) != nil {
									panic("host context leaked private parent values")
								}
							}
							runtime.GC()
							switch behavior {
							case "host-trap":
								panic(HostTrap{Err: cause})
							case "host-exit":
								panic(HostExit{Code: 17})
							case "producer-self-close":
								if err := producer.Close(); err != nil {
									panic(HostTrap{Err: err})
								}
							}
							if behavior == "cancel" || behavior == "importer-close" || behavior == "runtime-close" {
								entered <- struct{}{}
								<-release
							}
						})
						tail := []byte(nil)
						if behavior == "producer-trap" {
							tail = []byte{0}
						}
						producerCode := rv64PublicStartCompile(t, rv64PublicNativeStartProducerModule(shared, tail), artifact)
						producerImports := testImports("env.host", host).Global("env", "flag", flag)
						if shared {
							producerImports.Memory("env", "memory", memory)
						}
						var err error
						if managed {
							producerModule, moduleErr := rt.Module(producerCode)
							if moduleErr != nil {
								t.Fatal(moduleErr)
							}
							defer producerModule.Close()
							producer, err = rt.Instantiate(context.Background(), producerModule, WithImports(producerImports))
							hooks := rt.loadHooks().clone()
							hooks.afterCreate = append(hooks.afterCreate, func(event InstantiationEvent) error { return capture(event.Instance.value) })
							hooks.afterInstantiate = append(hooks.afterInstantiate, func(InstantiationEvent) { terminal++ })
							hooks.onInstantiateError = append(hooks.onInstantiateError, func(InstantiationErrorEvent) { failed++ })
							rt.storeHooks(hooks)
						} else {
							producer, err = Instantiate(producerCode, producerImports)
						}
						if err != nil {
							t.Fatal(err)
						}
						defer producer.Close()
						export, err := producer.ExportedFunc("producer_boot")
						if err != nil {
							t.Fatal(err)
						}
						imports.Function("env", "unused", HostCallFunc(func(HostCall) { panic("unused imported start called") })).Function("env", "start", export)
						beforeRoots := producer.referenceLifetime().snapshot().ResourceRoots
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
									result.in.Close()
								}
							}
						}()
						go func() {
							var in *Instance
							var err error
							if managed {
								in, err = rt.Instantiate(ctx, mod, WithImports(imports))
							} else {
								in, err = Instantiate(consumerCode, InstantiateOptions{Imports: imports, startContext: ctx, afterCreate: capture})
							}
							done <- outcome{in, err}
						}()
						if behavior == "cancel" || behavior == "importer-close" || behavior == "runtime-close" {
							select {
							case <-entered:
							case <-time.After(5 * time.Second):
								t.Fatal("native start did not park")
							}
							if active == nil || !active.constructionIsActive() || active.invocationState.Load()&instanceInvocationCount == 0 || len(active.thunkMem) == 0 || producer.referenceLifetime().snapshot().ResourceRoots != beforeRoots+1 {
								t.Fatal("native start lost construction/attachment lease")
							}
							switch behavior {
							case "cancel":
								cancel()
							case "importer-close":
								if err := active.Close(); err != nil {
									t.Fatal(err)
								}
							case "runtime-close":
								if err := rt.Close(); err != nil {
									t.Fatal(err)
								}
							}
							deadline := time.Now().Add(5 * time.Second)
							for atomic.LoadUint32((*uint32)(unsafe.Pointer(&active.trap[0]))) != uint32(coreruntime.TrapInterrupted) && time.Now().Before(deadline) {
								runtime.Gosched()
							}
							if atomic.LoadUint32((*uint32)(unsafe.Pointer(&active.trap[0]))) != uint32(coreruntime.TrapInterrupted) {
								t.Fatal("native start cancellation did not publish")
							}
							if active.jm == nil || producer.jm == nil {
								t.Fatal("native start disposed parked pointers")
							}
							unblock()
						}
						var result outcome
						select {
						case result = <-done:
							joined = true
						case <-time.After(5 * time.Second):
							t.Fatal("native start did not finish")
						}
						success := behavior == "success" || behavior == "producer-self-close"
						if success {
							if result.err != nil || result.in == nil {
								t.Fatal("native start success", result.err)
							}
							defer result.in.Close()
							if shared && producer.jm != result.in.jm {
								t.Fatal("native start shared memory fixture")
							}
							for range 3 {
								importedGlobalInvoke(t, result.in, "get", []uint64{21, 0x80000000deadbeef, 0x7ff0000000000001})
							}
							if flag.Get() != 11 || shared && memory.UnsafeBytes()[2] != 99 || calls != 1 {
								t.Fatal("native start not exactly once", flag.Get(), calls)
							}
							if behavior == "producer-self-close" && producer.jm == nil {
								t.Fatal("self-retired native producer lost attached root")
							}
							if err := result.in.Close(); err != nil {
								t.Fatal(err)
							}
							if behavior == "producer-self-close" && producer.jm != nil {
								t.Fatal("last constructor edge did not release retired producer")
							}
						} else {
							if result.in != nil {
								result.in.Close()
								t.Fatal("failed native start returned instance")
							}
							switch behavior {
							case "pre-cancel", "cancel":
								if !errors.Is(result.err, context.Canceled) {
									t.Fatal("native start cancellation cause", result.err)
								}
							case "host-trap":
								if !errors.Is(result.err, cause) {
									t.Fatal("native start host cause", result.err)
								}
							case "host-exit":
								var exit *ExitError
								if !errors.As(result.err, &exit) || exit.Code != 17 {
									t.Fatal("native start host exit", result.err)
								}
							case "producer-trap":
								var trap *coreruntime.TrapError
								if !errors.As(result.err, &trap) || trap.Code != coreruntime.TrapUnreachable || len(trap.Frames) != 1 || trap.Frames[0].FunctionName != "producer_boot" || trap.Frames[0].FunctionIndex != 1 || trap.Frames[0].ProgramCounter != 3 || !trap.Frames[0].HasProgramCounter {
									t.Fatal("native start producer source", result.err)
								}
							default:
								if result.err == nil {
									t.Fatal("native start closed constructor succeeded")
								}
							}
							if producer.referenceLifetime().snapshot().ResourceRoots != beforeRoots || active != nil && (active.jm != nil || active.thunkMem != nil) {
								t.Fatal("failed native start leaked attachment/publication")
							}
							want := uint64(10)
							if behavior == "pre-cancel" {
								want = 9
							}
							if flag.Get() != want || memory.UnsafeBytes()[2] != 0 {
								t.Fatal("failed native start resumed producer writes", flag.Get(), memory.UnsafeBytes()[2])
							}
							if behavior != "runtime-close" {
								if got, err := producer.Invoke("safe"); err != nil || !slices.Equal(got, []uint64{23}) {
									t.Fatal("producer reuse after native constructor unwind", got, err)
								}
							}
						}
						if managed {
							wantTerminal, wantFailed := 0, 1
							if success {
								wantTerminal, wantFailed = 1, 0
							}
							if behavior == "pre-cancel" {
								wantFailed = 0
							} // Runtime rejects before entering the construction hooks.
							if terminal != wantTerminal || failed != wantFailed {
								t.Fatal("native start terminal lifecycle", terminal, failed)
							}
							if behavior != "pre-cancel" {
								if callbackContext == nil {
									t.Fatal("native start callback context missing")
								}
								select {
								case <-callbackContext.Done():
								default:
									t.Fatal("native start callback scope outlived native completion")
								}
							}
						}
						if behavior == "pre-cancel" {
							if calls != 0 {
								t.Fatal("pre-cancel native start called producer")
							}
						} else if created != 1 || calls != 1 {
							t.Fatal("native start creation count", created, calls)
						}
					})
				}
			}
		}
	}
}

func TestRV64PublicNativeImportedStartRequiresOwnedBridge(t *testing.T) {
	in, err := Instantiate(rv64PublicStartCompile(t, rv64PublicNativeIdentityModule(nil), false))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for _, owned := range []map[uint32]uint64{nil, {0: 0}, {0: uint64(in.base)}, {0: 1}} {
		if handled, err := railshotInvokeNativeImportedStart(in, 0, owned, context.Background()); !handled || err == nil || !strings.Contains(err.Error(), "owned framed bridge") {
			t.Fatal("native start bypassed owned thunk", err)
		}
	}
}

func TestRV64PublicNativeImportedStartActiveLoopCancellation(t *testing.T) {
	flag := NewGlobalI32(0, true)
	defer flag.Close()
	source := wasmtest.Module(wasmtest.Section(1, []byte{1, 0x60, 0, 0}), wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "flag", wasm.I32, true))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("loop", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 1, 0x24, 0, 0x03, 0x40, 0x0c, 0, 0x0b, 0x0b}))))
	producer, err := Instantiate(rv64PublicStartCompile(t, source, true), NewImports().Global("env", "flag", flag))
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	export, err := producer.ExportedFunc("loop")
	if err != nil {
		t.Fatal(err)
	}
	consumerSource := wasmtest.Module(wasmtest.Section(1, []byte{1, 0x60, 0, 0}), wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("start")...), 0, 0))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(8, []byte{0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))))
	consumer := rv64PublicStartCompile(t, consumerSource, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	joined := false
	defer func() {
		cancel()
		if !joined {
			<-done
		}
	}()
	go func() {
		in, err := Instantiate(consumer, InstantiateOptions{Imports: testImports("env.start", export), startContext: ctx})
		if in != nil {
			in.Close()
		}
		done <- err
	}()
	// Observe the aligned off-heap one-write native sentinel, rather than assume
	// elapsed time proves that the foreign loop actually entered.
	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadUint64((*uint64)(unsafe.Pointer(&flag.cell[0]))) != 1 && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if atomic.LoadUint64((*uint64)(unsafe.Pointer(&flag.cell[0]))) != 1 {
		t.Fatal("native start loop did not enter")
	}
	cancel()
	select {
	case err := <-done:
		joined = true
		if !errors.Is(err, context.Canceled) {
			t.Fatal("native imported loop cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("native imported loop did not unwind")
	}
	if producer.referenceLifetime().snapshot().ResourceRoots != 0 {
		t.Fatal("native looping start retained producer")
	}
}

func TestRV64PublicNativeImportedStartWideInternalCallsOncePerInstance(t *testing.T) {
	wide := make([]wasm.ValType, 128)
	identity, boot := []byte{}, []byte{}
	want := rv64PublicVector(0x8899aabbccddeeff, 0x7ff0000000000001)
	for index := range wide {
		wide[index] = wasm.V128
		identity = append(identity, 0x20)
		identity = append(identity, wasmtest.ULEB(uint32(index))...)
		value := make([]byte, 16)
		if index == 127 {
			binary.LittleEndian.PutUint64(value, 0x8899aabbccddeeff)
			binary.LittleEndian.PutUint64(value[8:], 0x7ff0000000000001)
		}
		boot = append(boot, 0xfd, 12)
		boot = append(boot, value...)
	}
	identity = append(identity, 0x0b)
	boot = append(boot, 0x10, 0, 0x24, 0)
	for range 127 {
		boot = append(boot, 0x1a)
	}
	boot = append(boot, 0x23, 1, 0x41, 1, 0x6a, 0x24, 1, 0x0b)
	source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(wide, wide), wasmtest.FuncType(nil, nil))), wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "witness", wasm.V128, true), wasmtest.GlobalImportEntry("env", "counter", wasm.I32, true))), wasmtest.Section(3, []byte{2, 0, 1}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("boot", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(identity), wasmtest.Code(boot))))
	consumerSource := wasmtest.Module(wasmtest.Section(1, []byte{1, 0x60, 0, 0}), wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("start")...), 0, 0))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("noop", 0, 1))), wasmtest.Section(8, []byte{0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))))
	for _, artifact := range []bool{false, true} {
		witness, counter := NewGlobalV128(V128{}, true), NewGlobalI32(0, true)
		defer witness.Close()
		defer counter.Close()
		producer, err := Instantiate(rv64PublicStartCompile(t, source, artifact), NewImports().Global("env", "witness", witness).Global("env", "counter", counter))
		if err != nil {
			t.Fatal(err)
		}
		defer producer.Close()
		export, err := producer.ExportedFunc("boot")
		if err != nil {
			t.Fatal(err)
		}
		consumer := rv64PublicStartCompile(t, consumerSource, artifact)
		for attempt := range 2 {
			in, err := Instantiate(consumer, testImports("env.start", export))
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if witness.GetV128() != want || counter.Get() != uint64(attempt+1) {
				t.Fatal("wide native start result/once count", witness.GetV128(), counter.Get())
			}
			for range 3 {
				if got, err := in.Invoke("noop"); err != nil || len(got) != 0 {
					t.Fatal("native start subsequent invocation", got, err)
				}
			}
			if counter.Get() != uint64(attempt+1) {
				t.Fatal("native imported start reran on Invoke")
			}
			if err := in.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestRV64PublicNativeImportedStartActiveLoopClose(t *testing.T) {
	for _, managed := range []bool{false, true} {
		t.Run(fmt.Sprintf("runtime%t", managed), func(t *testing.T) {
			flag := NewGlobalI32(0, true)
			defer flag.Close()
			source := wasmtest.Module(wasmtest.Section(1, []byte{1, 0x60, 0, 0}), wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "flag", wasm.I32, true))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("loop", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 1, 0x24, 0, 0x03, 0x40, 0x0c, 0, 0x0b, 0x0b}))))
			producer, err := Instantiate(rv64PublicStartCompile(t, source, true), NewImports().Global("env", "flag", flag))
			if err != nil {
				t.Fatal(err)
			}
			defer producer.Close()
			export, err := producer.ExportedFunc("loop")
			if err != nil {
				t.Fatal(err)
			}
			consumerSource := wasmtest.Module(wasmtest.Section(1, []byte{1, 0x60, 0, 0}), wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("start")...), 0, 0))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(8, []byte{0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))))
			consumer := rv64PublicStartCompile(t, consumerSource, true)
			var active *Instance
			created := make(chan *Instance, 1)
			capture := func(in *Instance) error { created <- in; return nil }
			var rt *Runtime
			var mod *Module
			if managed {
				rt = NewRuntime()
				defer rt.Close()
				hooks := rt.loadHooks().clone()
				hooks.afterCreate = append(hooks.afterCreate, func(event InstantiationEvent) error { return capture(event.Instance.value) })
				rt.storeHooks(hooks)
				mod, err = rt.Module(consumer)
				if err != nil {
					t.Fatal(err)
				}
				defer mod.Close()
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			joined := false
			defer func() {
				cancel()
				if !joined {
					<-done
				}
			}()
			go func() {
				var in *Instance
				var err error
				if managed {
					in, err = rt.Instantiate(ctx, mod, WithImports(testImports("env.start", export)))
				} else {
					in, err = Instantiate(consumer, InstantiateOptions{Imports: testImports("env.start", export), startContext: ctx, afterCreate: capture})
				}
				if in != nil {
					in.Close()
				}
				done <- err
			}()
			select {
			case active = <-created:
			case <-time.After(5 * time.Second):
				t.Fatal("native looping constructor was not created")
			}
			deadline := time.Now().Add(5 * time.Second)
			for atomic.LoadUint64((*uint64)(unsafe.Pointer(&flag.cell[0]))) != 1 && time.Now().Before(deadline) {
				runtime.Gosched()
			}
			if atomic.LoadUint64((*uint64)(unsafe.Pointer(&flag.cell[0]))) != 1 {
				t.Fatal("native close loop did not enter")
			}
			if active == nil || !active.constructionIsActive() {
				t.Fatal("native looping constructor lost its construction lease")
			}
			if managed {
				err = rt.Close()
			} else {
				err = active.Close()
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				joined = true
				if err == nil {
					t.Fatal("closed native looping start succeeded")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("closed native looping start did not unwind")
			}
			if producer.referenceLifetime().snapshot().ResourceRoots != 0 || active.jm != nil || active.thunkMem != nil {
				t.Fatal("closed looping constructor leaked owned native resources")
			}
		})
	}
}
