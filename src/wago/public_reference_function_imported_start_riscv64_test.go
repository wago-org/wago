//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

func rv64ReferenceImportedStartRows(in *Instance) bool {
	if len(in.funcRefDescs) != 9*coreruntime.FuncRefDescBytes {
		return false
	}
	for _, index := range []int{4, 7} {
		row := in.funcRefDescs[(index+1)*40 : (index+2)*40]
		if !railshotNonCallableFunction(in.c, index) || !bytes.Equal(row, make([]byte, 40)) {
			return false
		}
	}
	return true
}

func rv64ReferenceImportedStartCheck(t *testing.T, in *Instance, vector bool) {
	t.Helper()
	if !in.c.HasStart || !in.c.StartIsImport || in.c.StartImportIdx != 1 || in.c.StartLocalFunc != 0 || in.c.NumImports != 3 || readGlobalObject(in.globalCells[2], ValI32) != 1 {
		t.Fatal("imported selected start identity/count")
	}
	want := []uint64{0x7ff0000000000042}
	if vector {
		want = append(want, 0x80000000deadbeef)
	}
	want = append(want, 0xfffffffe, 0, 0)
	if got, err := in.Invoke("get"); err != nil || !slices.Equal(got, want) {
		t.Fatal("imported start private saved/lower reference result", got, err)
	}
}

func TestRV64PublicReferenceFunctionImportedStartGo(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, wide := range []bool{false, true} {
				for _, ownedStart := range []bool{false, true} {
					for _, behavior := range []string{"success", "trap", "panic", "exit", "pre-cancel", "cancel", "instance-close"} {
						t.Run(fmt.Sprintf("artifact%t-vector%t-wide%t-owned%t-%s", artifact, vector, wide, ownedStart, behavior), func(t *testing.T) {
							rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
							defer rv64HostCertificateCloseRuntime(t, rt)
							provider, _, memories, im, _ := rv64ReferenceStartProvider(t, rt, artifact, vector, wide, false)
							pc := rv64NativeTailCompile(t, fmt.Sprintf("reference-imported-start-v%t-w%t", vector, wide), wasmtest.ClosedReferenceImportedStart(vector, wide), artifact)
							gate := newPluginCallGate("rv64-imported-start")
							rt.storeHooks(&hookRegistry{operationGates: []*pluginCallGate{gate}})
							ctx, cancel := context.WithCancel(context.Background())
							defer cancel()
							if behavior == "pre-cancel" {
								cancel()
							}
							cause := errors.New("private imported start failure")
							calls, finishes := 0, 0
							var captured Caller
							host := CallerHostCallFunc(func(caller Caller, call HostCall) {
								calls++
								captured = caller
								in := caller.in
								capability := rv64ReferenceImportTailCapability(in)
								reservation := in.constructionReservationSnapshot()
								if capability == 0 || !rv64ReferenceImportedStartRows(in) || !in.constructionIsActive() || reservation == nil || caller.reservation != reservation || activeHostInvocationContext(in).reservation != reservation || !reservation.allows(gate) || currentInvocationReservation(in) != nil || call.ParamCount() != 0 || call.ResultCount() != 0 || caller.Memory()[48] != 7 || readGlobalObject(in.globalCells[2], ValI32) != 0 {
									panic("Go imported start context/init")
								}
								if got, err := in.InvokeFromHost(context.Background(), caller, "initialize"); err != nil || len(got) != 0 {
									panic(HostTrap{Err: err})
								}
								if rv64ReferenceImportTailCapability(in) != capability || !caller.valid() || caller.reservation != reservation || activeHostInvocationContext(in).reservation != reservation || currentInvocationReservation(in) != nil {
									panic("Go imported start callback restoration")
								}
								runtime.GC()
								switch behavior {
								case "trap":
									panic(HostTrap{Err: cause})
								case "panic":
									panic(cause)
								case "exit":
									panic(HostExit{Code: 17})
								case "cancel":
									cancel()
								case "instance-close":
									if err := in.Close(); err != nil {
										panic(err)
									}
								}
							})
							var start any = host
							if ownedStart {
								start = rv64DynamicHostBinding(t, rt, true, host, pc.importFuncSigs[1])
							}
							im.Function("env.calls", "unused.fn", func(int32) int32 { panic("unused import called") }).Function("env.calls", "start.fn", start).Function("env.calls", "finish.fn", HostCallFunc(func(call HostCall) {
								finishes++
								if call.ParamSlots()[0] != 0xfffffffd {
									panic("private imported start dirty i32")
								}
								call.ResultSlots()[0] = 0xcafebeeffffffffe
							}))
							mod, err := rt.Module(pc)
							if err != nil {
								t.Fatal(err)
							}
							defer mod.Close()
							roots := provider.referenceLifetime().snapshot().ResourceRoots
							refs := pc.loadCodeCache().refs
							in, err := rt.Instantiate(ctx, mod, WithImports(im))
							if behavior == "success" {
								if err != nil || in == nil {
									t.Fatal(err)
								}
								rv64ReferenceImportedStartCheck(t, in, vector)
								in.Close()
							} else {
								if in != nil {
									in.Close()
									t.Fatal("failed imported start returned instance")
								}
								if err == nil {
									t.Fatal("failed imported start succeeded")
								}
								switch behavior {
								case "trap", "panic":
									if !errors.Is(err, cause) {
										t.Fatal("lost imported start cause", err)
									}
								case "exit":
									var exit *ExitError
									if !errors.As(err, &exit) || exit.Code != 17 {
										t.Fatal(err)
									}
								case "pre-cancel", "cancel":
									if !errors.Is(err, context.Canceled) {
										t.Fatal(err)
									}
								}
							}
							expected := 1
							if behavior == "pre-cancel" {
								expected = 0
							}
							if calls != expected || finishes != 2*expected || readGlobalObject(provider.globalCells[1], ValI32) != uint64(11+expected) || int(memories[0].UnsafeBytes()[49]) != 42*expected || captured.valid() || gate.state.Load() != 0 || provider.referenceLifetime().snapshot().ResourceRoots != roots || pc.loadCodeCache().refs != refs {
								t.Fatal("imported start once/scope/root refund", calls, finishes)
							}
							if captured.in != nil && (!captured.in.resourcesClosed || captured.in.constructionIsActive()) {
								t.Fatal("imported constructor survived completion")
							}
							if ownedStart {
								if err := start.(*HostFuncRef).Close(); err != nil {
									t.Fatal(err)
								}
							}
						})
					}
				}
			}
		}
	}
}

func TestRV64PublicReferenceFunctionImportedStartGoParked(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, action := range []string{"cancel", "instance-close", "runtime-close"} {
				t.Run(fmt.Sprintf("artifact%t-vector%t-%s", artifact, vector, action), func(t *testing.T) {
					owners := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
					defer rv64HostCertificateCloseRuntime(t, owners)
					rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
					defer rv64HostCertificateCloseRuntime(t, rt)
					provider, grower, _, im, _ := rv64ReferenceStartProvider(t, owners, artifact, vector, true, false)
					pc := rv64NativeTailCompile(t, fmt.Sprintf("reference-imported-start-v%t-wtrue", vector), wasmtest.ClosedReferenceImportedStart(vector, true), artifact)
					entered, release := make(chan Caller, 1), make(chan struct{})
					released := false
					unblock := func() {
						if !released {
							close(release)
							released = true
						}
					}
					defer unblock()
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					host := CallerHostCallFunc(func(caller Caller, _ HostCall) {
						if _, err := caller.in.InvokeFromHost(context.Background(), caller, "initialize"); err != nil {
							panic(err)
						}
						entered <- caller
						<-release
					})
					owned := rv64DynamicHostBinding(t, rt, true, host, pc.importFuncSigs[1])
					im.Function("env.calls", "unused.fn", func(int32) int32 { return 0 }).Function("env.calls", "start.fn", owned).Function("env.calls", "finish.fn", func(v int32) int32 { return v + 1 })
					mod, err := rt.Module(pc)
					if err != nil {
						t.Fatal(err)
					}
					defer mod.Close()
					type result struct {
						in  *Instance
						err error
					}
					done := make(chan result, 1)
					finished := make(chan struct{})
					go func() {
						defer close(finished)
						in, err := rt.Instantiate(ctx, mod, WithImports(im))
						done <- result{in, err}
					}()
					defer func() {
						cancel()
						unblock()
						select {
						case <-finished:
						case <-time.After(5 * time.Second):
							t.Error("imported Go start did not join")
						}
					}()
					resolver := new(CallerResolver)
					resolver.rt.Store(rt)
					var caller Caller
					select {
					case caller = <-entered:
					case <-time.After(5 * time.Second):
						t.Fatal("imported start did not park")
					}
					active := caller.in
					callbackContext, err := resolver.InvocationContext(caller)
					if err != nil {
						t.Fatal(err)
					}
					if !active.constructionIsActive() || active.resourcesClosed || !caller.valid() {
						t.Fatal("parked construction")
					}
					switch action {
					case "cancel":
						cancel()
					case "instance-close":
						err = active.Close()
					case "runtime-close":
						err = rt.Close()
					}
					if err != nil {
						t.Fatal(err)
					}
					if action == "cancel" {
						select {
						case <-callbackContext.Done():
						case <-time.After(5 * time.Second):
							t.Fatal("parked Go callback context not canceled")
						}
					}
					provider.Close()
					pc.Close()
					if provider.resourcesClosed || active.resourcesClosed || owned.(*HostFuncRef).Close() == nil {
						t.Fatal("parked imported start lost roots")
					}
					unblock()
					select {
					case outcome := <-done:
						if outcome.in != nil || outcome.err == nil {
							if outcome.in != nil {
								outcome.in.Close()
							}
							t.Fatal("closed/canceled imported start returned instance", outcome.err)
						}
						if action == "cancel" && !errors.Is(outcome.err, context.Canceled) {
							t.Fatal(outcome.err)
						}
					case <-time.After(5 * time.Second):
						t.Fatal("imported start unwind timeout")
					}
					if caller.valid() || active.constructionIsActive() || !active.resourcesClosed || readGlobalObject(provider.globalCells[1], ValI32) != 12 {
						t.Fatal("imported start completion/scope/side effect")
					}
					if err := owned.(*HostFuncRef).Close(); err != nil {
						t.Fatal(err)
					}
					select {
					case <-callbackContext.Done():
					case <-time.After(5 * time.Second):
						t.Fatal("completed callback context did not expire")
					}
					if _, err := resolver.InvocationContext(caller); err == nil {
						t.Fatal("retained imported-start caller resolved")
					}
					grower.Close()
					if !provider.resourcesClosed {
						t.Fatal("provider roots survived final borrower")
					}
				})
			}
		}
	}
}

func TestRV64PublicReferenceFunctionImportedStartNative(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, shared := range []bool{false, true} {
				for _, behavior := range []string{"success", "retire", "trap", "native-trap", "cancel", "instance-close"} {
					t.Run(fmt.Sprintf("artifact%t-vector%t-shared%t-%s", artifact, vector, shared, behavior), func(t *testing.T) {
						rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
						defer rv64HostCertificateCloseRuntime(t, rt)
						provider, _, memories, im, _ := rv64ReferenceStartProvider(t, rt, artifact, vector, false, false)
						pc := rv64NativeTailCompile(t, fmt.Sprintf("reference-imported-start-v%t-wfalse", vector), wasmtest.ClosedReferenceImportedStart(vector, false), artifact)
						nativeID := fmt.Sprintf("reference-imported-start-native-s%t", shared)
						var tail []byte
						if behavior == "native-trap" {
							nativeID = fmt.Sprintf("reference-imported-start-native-trap-s%t", shared)
							tail = []byte{0x00}
						}
						nc := rv64NativeTailCompile(t, nativeID, rv64PublicNativeStartProducerModule(shared, tail), artifact)
						ctx, cancel := context.WithCancel(context.Background())
						defer cancel()
						cause := errors.New("private native imported start trap")
						var active, producer, relay *Instance
						var relayCode *Compiled
						var saved Caller
						calls := 0
						host := CallerHostCallFunc(func(caller Caller, call HostCall) {
							calls++
							saved = caller
							if active == nil {
								panic("native imported start missing constructing root")
							}
							reservation := active.constructionReservationSnapshot()
							capability := rv64ReferenceImportTailCapability(active)
							if active == nil || caller.in != producer || !active.constructionIsActive() || caller.reservation != reservation || reservation == nil || capability == 0 || !rv64ReferenceImportedStartRows(active) || call.ParamCount() != 0 || call.ResultCount() != 0 || (shared && caller.Memory()[48] != 7) || (!shared && producer.jm == active.jm) {
								panic("native imported start ownership/context")
							}
							if _, err := active.InvokeFromHost(context.Background(), caller, "initialize"); err != nil {
								panic(HostTrap{Err: err})
							}
							if got, err := producer.InvokeFromHost(context.Background(), caller, "safe"); err != nil || !slices.Equal(got, []uint64{23}) {
								panic("native producer self-reentry")
							}
							if !caller.valid() || rv64ReferenceImportTailCapability(active) != capability || currentInvocationReservation(active) != reservation {
								panic("native imported-start outer restoration")
							}
							runtime.GC()
							switch behavior {
							case "retire":
								relay.Close()
								relayCode.Close()
								producer.Close()
								nc.Close()
								provider.Close()
								pc.Close()
								if producer.resourcesClosed || active.resourcesClosed {
									panic("retired native start lost roots")
								}
							case "trap":
								panic(HostTrap{Err: cause})
							case "cancel":
								cancel()
							case "instance-close":
								if err := active.Close(); err != nil {
									panic(err)
								}
							}
							if behavior == "cancel" || behavior == "instance-close" {
								deadline := time.Now().Add(5 * time.Second)
								for atomic.LoadUint32((*uint32)(unsafe.Pointer(&active.trap[0]))) != uint32(coreruntime.TrapInterrupted) && time.Now().Before(deadline) {
									runtime.Gosched()
								}
								if atomic.LoadUint32((*uint32)(unsafe.Pointer(&active.trap[0]))) != uint32(coreruntime.TrapInterrupted) {
									panic("native imported-start interrupt not delivered")
								}
							}
						})
						flag, err := provider.ExportedGlobalObject("counter")
						if err != nil {
							t.Fatal(err)
						}
						ni := NewImports().Global("env", "flag", flag).Function("env", "host", host)
						if shared {
							ni.Memory("env", "memory", memories[0])
						}
						producer = rv64NativeTokenInstantiate(t, rt, nc, ni)
						roots := producer.referenceLifetime().snapshot().ResourceRoots
						target, err := producer.ExportedFunc("producer_boot")
						if err != nil {
							t.Fatal(err)
						}
						if behavior == "retire" {
							relaySource := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))), wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("alias", 0, 0))))
							relayCode = rv64NativeTailCompile(t, "reference-imported-start-native-relay", relaySource, artifact)
							relay = rv64NativeTokenInstantiate(t, rt, relayCode, NewImports().Function("env", "target", target))
							target, err = relay.ExportedFunc("alias")
							if err != nil {
								t.Fatal(err)
							}
						}
						gate := newPluginCallGate("rv64-native-imported-start")
						rt.storeHooks(&hookRegistry{operationGates: []*pluginCallGate{gate}, afterCreate: []func(InstantiationEvent) error{func(event InstantiationEvent) error { active = event.Instance.value; return nil }}})
						im.Function("env.calls", "unused.fn", func(int32) int32 { panic("unused import") }).Function("env.calls", "start.fn", target).Function("env.calls", "finish.fn", func(v int32) int32 { return v + 1 })
						mod, err := rt.Module(pc)
						if err != nil {
							t.Fatal(err)
						}
						defer mod.Close()
						in, err := rt.Instantiate(ctx, mod, WithImports(im))
						success := behavior == "success" || behavior == "retire"
						if success {
							if err != nil || in != active {
								t.Fatal("native selected constructor", err)
							}
							rv64ReferenceImportedStartCheck(t, in, vector)
							if readGlobalObject(provider.globalCells[1], ValI32) != 13 {
								t.Fatal("native start continuation/order")
							}
							in.Close()
						} else {
							if in != nil {
								in.Close()
								t.Fatal("failed native start returned instance")
							}
							if err == nil {
								t.Fatal("failed native start succeeded")
							}
							if behavior == "trap" && !errors.Is(err, cause) {
								t.Fatal(err)
							}
							if behavior == "native-trap" {
								ownedTableTrap(t, err, coreruntime.TrapUnreachable)
							}
							if behavior == "cancel" && !errors.Is(err, context.Canceled) {
								t.Fatal(err)
							}
							if readGlobalObject(provider.globalCells[1], ValI32) != 12 {
								t.Fatal("failed native start continued or lost effects")
							}
						}
						if calls != 1 || saved.valid() || !active.resourcesClosed || active.constructionIsActive() || gate.state.Load() != 0 || producer.referenceLifetime().snapshot().ResourceRoots != roots {
							t.Fatal("native start caller/root/construction release")
						}
						if behavior == "retire" {
							rv64ReferenceImportedStartRetired(t, producer, nc)
							rv64ReferenceImportedStartRetired(t, relay, relayCode)
						}
					})
				}
			}
		}
	}
}

func rv64ReferenceImportedStartRetired(t *testing.T, producer *Instance, code *Compiled) {
	t.Helper()
	if !producer.resourcesClosed {
		t.Fatal("retired native start producer retained physical resources")
	}
	cache := code.loadCodeCache()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if !cache.closed || cache.refs != 0 || cache.base != 0 || len(cache.mem) != 0 {
		t.Fatal("retired native start producer retained executable code", cache.closed, cache.refs, cache.base, len(cache.mem))
	}
}

func TestRV64PublicReferenceFunctionImportedStartNativeParked(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-vector%t", artifact, vector), func(t *testing.T) {
				owners := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
				defer rv64HostCertificateCloseRuntime(t, owners)
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
				defer rv64HostCertificateCloseRuntime(t, rt)
				provider, grower, memories, im, _ := rv64ReferenceStartProvider(t, owners, artifact, vector, false, false)
				pc := rv64NativeTailCompile(t, fmt.Sprintf("reference-imported-start-v%t-wfalse", vector), wasmtest.ClosedReferenceImportedStart(vector, false), artifact)
				nc := rv64NativeTailCompile(t, "reference-imported-start-native-strue", rv64PublicNativeStartProducerModule(true, nil), artifact)
				entered, release := make(chan Caller, 1), make(chan struct{})
				var once sync.Once
				unblock := func() { once.Do(func() { close(release) }) }
				defer unblock()
				var active, producer *Instance
				calls := 0
				host := CallerHostCallFunc(func(caller Caller, call HostCall) {
					calls++
					if active == nil || caller.in != producer || !active.constructionIsActive() || caller.reservation != active.constructionReservationSnapshot() || caller.reservation == nil || call.ParamCount() != 0 || call.ResultCount() != 0 || caller.Memory()[48] != 7 || !rv64ReferenceImportedStartRows(active) {
						panic("parked native imported-start constructor identity")
					}
					capability := rv64ReferenceImportTailCapability(active)
					if capability == 0 {
						panic("parked native imported-start missing static tail capability")
					}
					if _, err := active.InvokeFromHost(context.Background(), caller, "initialize"); err != nil {
						panic(HostTrap{Err: err})
					}
					if got, err := producer.InvokeFromHost(context.Background(), caller, "safe"); err != nil || !slices.Equal(got, []uint64{23}) {
						panic("parked native producer self-entry")
					}
					if !caller.valid() || currentInvocationReservation(active) != caller.reservation || rv64ReferenceImportTailCapability(active) != capability {
						panic("parked native imported-start outer restoration")
					}
					entered <- caller
					<-release
					runtime.GC()
				})
				counter, err := provider.ExportedGlobalObject("counter")
				if err != nil {
					t.Fatal(err)
				}
				producer = rv64NativeTokenInstantiate(t, owners, nc, NewImports().Global("env", "flag", counter).Memory("env", "memory", memories[0]).Function("env", "host", host))
				target, err := producer.ExportedFunc("producer_boot")
				if err != nil {
					t.Fatal(err)
				}
				gate := newPluginCallGate("rv64-native-imported-start-parked")
				rt.storeHooks(&hookRegistry{operationGates: []*pluginCallGate{gate}, afterCreate: []func(InstantiationEvent) error{func(event InstantiationEvent) error { active = event.Instance.value; return nil }}})
				im.Function("env.calls", "unused.fn", func(int32) int32 { panic("unused import") }).Function("env.calls", "start.fn", target).Function("env.calls", "finish.fn", func(v int32) int32 { return v + 1 })
				mod, err := rt.Module(pc)
				if err != nil {
					t.Fatal(err)
				}
				defer mod.Close()
				type result struct {
					in  *Instance
					err error
				}
				done := make(chan result, 1)
				finished := make(chan struct{})
				go func() {
					defer close(finished)
					in, err := rt.Instantiate(context.Background(), mod, WithImports(im))
					done <- result{in, err}
				}()
				defer func() {
					unblock()
					select {
					case <-finished:
					case <-time.After(5 * time.Second):
						t.Error("parked native imported-start constructor did not join")
					}
				}()
				var caller Caller
				select {
				case caller = <-entered:
				case <-time.After(5 * time.Second):
					t.Fatal("native imported-start callback did not park")
				}
				for _, owner := range []*Instance{producer, provider} {
					if err := owner.Close(); err != nil {
						t.Fatal(err)
					}
				}
				if err := nc.Close(); err != nil {
					t.Fatal(err)
				}
				if err := pc.Close(); err != nil {
					t.Fatal(err)
				}
				if err := rt.Close(); err != nil {
					t.Fatal(err)
				}
				deadline := time.Now().Add(5 * time.Second)
				for atomic.LoadUint32((*uint32)(unsafe.Pointer(&active.trap[0]))) != uint32(coreruntime.TrapInterrupted) && time.Now().Before(deadline) {
					runtime.Gosched()
				}
				if atomic.LoadUint32((*uint32)(unsafe.Pointer(&active.trap[0]))) != uint32(coreruntime.TrapInterrupted) || !active.constructionIsActive() || active.resourcesClosed || producer.resourcesClosed || provider.resourcesClosed || !caller.valid() || gate.state.Load() == 0 {
					t.Fatal("closed Runtime lost parked native construction roots or interrupt")
				}
				cache := nc.loadCodeCache()
				cache.mu.Lock()
				retained := cache.closed && cache.refs != 0 && cache.base != 0 && len(cache.mem) != 0
				cache.mu.Unlock()
				if !retained {
					t.Fatal("parked retired producer lost executable code")
				}
				unblock()
				select {
				case outcome := <-done:
					if outcome.in != nil {
						outcome.in.Close()
						t.Fatal("closed Runtime returned constructed instance")
					}
					ownedTableTrap(t, outcome.err, coreruntime.TrapInterrupted)
				case <-time.After(5 * time.Second):
					t.Fatal("parked native start unwind timeout")
				}
				wait, stop := context.WithTimeout(context.Background(), 5*time.Second)
				defer stop()
				if err := rt.WaitClosed(wait); err != nil {
					t.Fatal(err)
				}
				if calls != 1 || caller.valid() || active.constructionIsActive() || !active.resourcesClosed || gate.state.Load() != 0 || active.invocationState.Load()&instanceInvocationCount != 0 || active.pluginState.Load().railshotInvocation.tailDispatcher != nil || readGlobalObject(provider.globalCells[1], ValI32) != 12 || provider.jm.HostBytes()[49] != 42 || provider.jm.HostBytes()[2] != 0 {
					t.Fatal("parked native start continued or retained constructor/scope resources")
				}
				rv64ReferenceImportedStartRetired(t, producer, nc)
				if provider.resourcesClosed {
					t.Fatal("independent grower lost retired resource provider")
				}
				grower.Close()
				if !provider.resourcesClosed {
					t.Fatal("resource provider survived final borrower")
				}
			})
		}
	}
}

func TestRV64PublicReferenceFunctionImportedStartWideNative(t *testing.T) {
	wide := make([]wasm.ValType, 128)
	var identity, boot []byte
	want := rv64PublicVector(0x8899aabbccddeeff, 0x7ff0000000000001)
	for i := range wide {
		wide[i] = wasm.V128
		identity = append(identity, 0x20)
		identity = append(identity, wasmtest.ULEB(uint32(i))...)
		value := make([]byte, 16)
		if i == 127 {
			copy(value, want[:])
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
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			provider, _, _, im, _ := rv64ReferenceStartProvider(t, rt, artifact, true, true, false)
			witness, err := provider.ExportedGlobalObject("payload")
			if err != nil {
				t.Fatal(err)
			}
			counter, err := provider.ExportedGlobalObject("counter")
			if err != nil {
				t.Fatal(err)
			}
			nc := rv64NativeTailCompile(t, "reference-imported-start-wide-native", source, artifact)
			producer := rv64NativeTokenInstantiate(t, rt, nc, NewImports().Global("env", "witness", witness).Global("env", "counter", counter))
			target, err := producer.ExportedFunc("boot")
			if err != nil {
				t.Fatal(err)
			}
			pc := rv64NativeTailCompile(t, "reference-imported-start-vtrue-wtrue", wasmtest.ClosedReferenceImportedStart(true, true), artifact)
			im.Function("env.calls", "start.fn", target).Function("env.calls", "unused.fn", func(v int32) int32 { return v }).Function("env.calls", "finish.fn", func(v int32) int32 { return v + 1 })
			for attempt := range 2 {
				in := rv64NativeTokenInstantiate(t, rt, pc, im)
				if witness.GetV128() != want || counter.Get() != uint64(12+attempt) || readGlobalObject(in.globalCells[2], ValI32) != 0 {
					t.Fatal("selected wide native bridge/once")
				}
				for range 3 {
					if _, err := in.Invoke("get"); err != nil {
						t.Fatal(err)
					}
				}
				if counter.Get() != uint64(12+attempt) {
					t.Fatal("wide native start reran")
				}
				in.Close()
			}
		})
	}
}
