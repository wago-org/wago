//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64DynamicHostBinding(t *testing.T, rt *Runtime, owned bool, host any, sig FuncSig) any {
	t.Helper()
	if !owned {
		return host
	}
	owner, err := rt.NewHostFuncRef(host, sig)
	if err != nil {
		t.Fatal(err)
	}
	// Register before instances so their callback leases retire first. Runtime
	// teardown is joined explicitly by each test before these cleanups run.
	t.Cleanup(func() {
		if err := owner.Close(); err != nil {
			t.Error("dynamic host owner cleanup", err)
		}
	})
	return owner
}

func TestRV64PublicDynamicHostTailTransport(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, owned := range []bool{false, true} {
			for _, reference := range []bool{false, true} {
				for _, count := range []int{0, 4, 64} {
					t.Run(fmt.Sprintf("artifact%t-owned%t-reference%t-count%d", artifact, owned, reference, count), func(t *testing.T) {
						types := make([]wasm.ValType, count)
						input, args, output, want := make([]uint64, count), make([]uint64, count), make([]uint64, count), make([]uint64, count)
						for i := range types {
							types[i] = []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}[i%4]
							input[i] = []uint64{0xcafe123480000001, 0xfedcba9876543210, 0xcafe12347f800123, 0x7ff0000000000001}[i%4]
							output[i] = []uint64{0xeeeeeeeefffffff7, 0xffedcba987654321, 0xeeeeeeee80000000, 0xfff8000000000123}[i%4]
							args[i], want[i] = input[i], output[i]
							if i%4 == 0 || i%4 == 2 {
								args[i], want[i] = uint64(uint32(args[i])), uint64(uint32(want[i]))
							}
						}
						source := rv64DynamicNativeImportModule(types, true, true)
						if reference {
							source = rv64PublicReferenceTailModule(types, true)
						}
						c := rv64NativeTailCompile(t, fmt.Sprintf("dynamic-host-count%d-reference%t", count, reference), source, artifact)
						rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
						defer rv64HostCertificateCloseRuntime(t, rt)
						calls := 0
						host := HostCallFunc(func(call HostCall) {
							calls++
							if !slices.Equal(call.ParamSlots(), args) {
								panic("dynamic host arguments")
							}
							copy(call.ResultSlots(), output)
						})
						binding := rv64DynamicHostBinding(t, rt, owned, host, c.importFuncSigs[0])
						in := rv64HostTailInstantiate(t, rt, c, NewImports().Function("env", "target", binding))
						if in.pluginState.Load().railshotInvocation.tailDispatcher != nil {
							t.Fatal("mixed dynamic module constructed competing static factory")
						}
						selectors := []uint64{0, 1, 2}
						if reference {
							selectors = []uint64{0, 0, 0}
						}
						for _, selector := range selectors {
							got, err := in.Invoke("call", append(slices.Clone(input), 0xcafe123400000000|selector)...)
							if err != nil || !slices.Equal(got, want) {
								t.Fatal("dynamic host transport", selector, got, want, err)
							}
							if state := &in.pluginState.Load().railshotInvocation; state.tailGenerationBudget.count != 0 || rv64EntryCapability(in) != 0 {
								t.Fatal("dynamic host cold exit retained generation")
							}
						}
						if calls != 3 {
							t.Fatal("dynamic host dispatch count", calls)
						}
						if _, err := in.Invoke("call", append(slices.Clone(input), 3)...); err == nil || calls != 3 {
							t.Fatal("invalid reference reached host", err, calls)
						}
						if got, err := in.Invoke("call", append(slices.Clone(input), 0)...); err != nil || !slices.Equal(got, want) {
							t.Fatal("reuse after invalid reference", got, err)
						}
					})
				}
			}
		}
	}
}

func TestRV64PublicDynamicHostTailReentry(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, owned := range []bool{false, true} {
			for _, reference := range []bool{false, true} {
				t.Run(fmt.Sprintf("artifact%t-owned%t-reference%t", artifact, owned, reference), func(t *testing.T) {
					rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
					defer rv64HostCertificateCloseRuntime(t, rt)
					var in *Instance
					depth, calls := 0, 0
					fail := false
					cause := errors.New("dynamic host deliberate trap")
					host := CallerHostCallFunc(func(caller Caller, call HostCall) {
						calls++
						if depth == 0 {
							view := rv64InvocationViewForTest(in)
							before := bytes.Clone(view)
							state := &in.pluginState.Load().railshotInvocation
							if railshotParkedNativeInvocation(in) == 0 || state.tableDepth == 0 || state.tailGenerationBudget.count == 0 || rv64EntryCapability(in) == 0 {
								panic("dynamic host lost parked generation")
							}
							depth++
							got, err := in.InvokeFromHost(context.Background(), caller, "call", 8, 0)
							depth--
							if err != nil || !slices.Equal(got, []uint64{9}) || !bytes.Equal(view, before) {
								panic(fmt.Sprintf("dynamic host reentry %v %v", got, err))
							}
							runtime.GC()
							if fail {
								panic(HostTrap{Err: cause})
							}
						}
						call.SetI32(0, call.I32(0)+1)
					})
					source := rv64DynamicNativeImportModule([]wasm.ValType{wasm.I32}, true, true)
					if reference {
						source = rv64PublicReferenceTailModule([]wasm.ValType{wasm.I32}, true)
					}
					c := rv64NativeTailCompile(t, fmt.Sprintf("dynamic-host-reentry-reference%t", reference), source, artifact)
					binding := rv64DynamicHostBinding(t, rt, owned, host, c.importFuncSigs[0])
					in = rv64HostTailInstantiate(t, rt, c, NewImports().Function("env", "target", binding))
					for _, trap := range []bool{false, true, false} {
						fail = trap
						got, err := in.Invoke("call", 0xcafe123400000007, 0)
						if trap {
							if !errors.Is(err, cause) || len(got) != 0 {
								t.Fatal("dynamic host cold trap", got, err)
							}
						} else if err != nil || !slices.Equal(got, []uint64{8}) {
							t.Fatal("dynamic host resume", got, err)
						}
						if in.pluginState.Load().railshotInvocation.tailGenerationBudget.count != 0 || rv64EntryCapability(in) != 0 {
							t.Fatal("nested generation escaped cold exit")
						}
					}
					if calls != 6 {
						t.Fatal("nested dispatch count", calls)
					}
				})
			}
		}
	}
}

func TestRV64PrivateTailHostGeneration(t *testing.T) {
	for _, owned := range []bool{false, true} {
		t.Run(fmt.Sprintf("owned%t", owned), func(t *testing.T) {
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			c := rv64NativeTailCompile(t, "dynamic-host-generation", rv64DynamicNativeImportModule([]wasm.ValType{wasm.I32}, true, true), false)
			binding := rv64DynamicHostBinding(t, rt, owned, func(v int32) int32 { return v + 1 }, c.importFuncSigs[0])
			in := rv64HostTailInstantiate(t, rt, c, NewImports().Function("env", "target", binding))
			var scope rv64NativeTableTargets
			if err := rv64TableTargetRefresh(&scope, in); err != nil {
				t.Fatal(err)
			}
			defer scope.release()
			var g *rv64NativeTailGeneration
			defer func() {
				if err := rv64GenerationLockedForTest(func() error { return g.Close() }); err != nil {
					t.Error(err)
				}
			}()
			create := func(limits rv64NativeTailGenerationLimits) error {
				return rv64GenerationLockedForTest(func() error {
					var err error
					g, err = rv64NewDefinedTailGeneration(in, &scope, rv64NativeTailModule(c).Plans, limits)
					return err
				})
			}
			if err := create(rv64NativeTailGenerationLimits{railshot.MaxCodeBytes, railshot.MaxModuleBytes}); err != nil {
				t.Fatal(err)
			}
			exact := rv64NativeTailGenerationLimits{g.codeBytes, g.metadataBytes}
			if err := rv64GenerationLockedForTest(g.Close); err != nil {
				t.Fatal(err)
			}
			for _, limits := range []rv64NativeTailGenerationLimits{{exact.code - 1, exact.metadata}, {exact.code, exact.metadata - 1}} {
				if err := create(limits); g != nil || !errors.Is(err, railshot.ErrUnsupported) || scope.tailBudget.count != 0 {
					t.Fatal("host quota failed before construction", g, err)
				}
			}
			if err := create(exact); err != nil {
				t.Fatal("exact host generation charge", err)
			}
			activate := func() error {
				return rv64GenerationLockedForTest(func() error { return rv64ActivateNativeTailGenerations([]*rv64NativeTailGeneration{g}) })
			}
			if err := activate(); err != nil {
				t.Fatal(err)
			}
			before := rv64EntryCapability(in)
			saved := in.syncHosts[0].sig
			in.syncHosts[0].sig = &FuncSig{Params: []ValType{ValI64}, Results: []ValType{ValI32}}
			err := activate()
			in.syncHosts[0].sig = saved
			if err == nil || rv64EntryCapability(in) != before {
				t.Fatal("changed host binding refreshed callable generation", err)
			}
			if n := testing.AllocsPerRun(4, func() {
				if err := activate(); err != nil {
					panic(err)
				}
			}); n != 0 {
				t.Fatal("warm host generation allocated", n)
			}
		})
	}
}

func rv64DynamicHostShapeModule(params, results []wasm.ValType, reference, swar bool) []byte {
	var body []byte
	if swar {
		body = append(body, 0xfd, 0x0c)
		body = append(body, make([]byte, 16)...)
		body = append(body, 0x1a)
	}
	for i := range params {
		body = append(body, 0x20, byte(i))
	}
	if reference {
		body = append(body, 0xd2, 0, 0x15, 0)
	} else {
		body = append(body, 0x41, 0, 0x13, 0, 0)
	}
	body = append(body, 0x0b)
	im := append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results))),
		wasmtest.Section(2, wasmtest.Vec(im)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(4, []byte{1, 0x70, 0, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))), wasmtest.Section(9, []byte{1, 0, 0x41, 0, 0x0b, 1, 0}),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
}

func TestRV64PublicDynamicHostTailAsymmetricSWAR(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, owned := range []bool{false, true} {
			for _, reference := range []bool{false, true} {
				for _, shape := range []string{"noargs", "noresults", "mixed"} {
					t.Run(fmt.Sprintf("artifact%t-owned%t-reference%t-%s", artifact, owned, reference, shape), func(t *testing.T) {
						params, results := []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.F32, wasm.I64}
						input, want := []uint64{0xcafe123480000001}, []uint64{0x7f800123, 0xfeedabcd80000001}
						if shape == "noargs" {
							params, input = nil, nil
						} else if shape == "noresults" {
							results, want = nil, nil
						}
						c := rv64NativeTailCompile(t, fmt.Sprintf("dynamic-host-swar-%s-reference%t", shape, reference), rv64DynamicHostShapeModule(params, results, reference, true), artifact)
						if m := rv64NativeTailModule(c); m == nil || len(m.Plans) != 1 || m.Plans[0].SourceStride != 16 {
							t.Fatal("dynamic host omitted SWAR source stride")
						}
						rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
						defer rv64HostCertificateCloseRuntime(t, rt)
						calls := 0
						host := HostCallFunc(func(call HostCall) {
							calls++
							if len(call.ParamSlots()) != len(params) || len(params) != 0 && call.ParamSlots()[0] != 0x80000001 {
								panic("asymmetric host inputs")
							}
							if len(results) != 0 {
								copy(call.ResultSlots(), []uint64{0xeeeeeeee7f800123, 0xfeedabcd80000001})
							}
						})
						binding := rv64DynamicHostBinding(t, rt, owned, host, c.importFuncSigs[0])
						in := rv64HostTailInstantiate(t, rt, c, NewImports().Function("env", "target", binding))
						if got, err := in.Invoke("call", input...); err != nil || !slices.Equal(got, want) || calls != 1 {
							t.Fatal("asymmetric dynamic host transport", got, want, calls, err)
						}
					})
				}
			}
		}
	}
}

func TestRV64PublicDynamicHostTailForeignOwner(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, owned := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-owned%t", artifact, owned), func(t *testing.T) {
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
				defer rv64HostCertificateCloseRuntime(t, rt)
				types := []wasm.ValType{wasm.I32}
				pc := rv64NativeTailCompile(t, "dynamic-host-foreign-producer", rv64DynamicNativeImportModule(types, true, true), artifact)
				c := rv64NativeTailCompile(t, "dynamic-host-foreign-consumer", rv64PublicDynamicTailModule(types, true), artifact)
				var producers, consumers []*Instance
				for _, delta := range []int32{1, 2} {
					binding := rv64DynamicHostBinding(t, rt, owned, func(v int32) int32 { return v + delta }, pc.importFuncSigs[0])
					producer := rv64HostTailInstantiate(t, rt, pc, NewImports().Function("env", "target", binding))
					table, err := producer.ExportedTable("table")
					if err != nil {
						t.Fatal(err)
					}
					consumer := rv64HostTailInstantiate(t, rt, c, NewImports().Table("env", "table", table))
					producers, consumers = append(producers, producer), append(consumers, consumer)
				}
				// Both callback bindings coexist over the same compiled cache.
				for i, producer := range producers {
					if err := producer.Close(); err != nil {
						t.Fatal(err)
					}
					if got, err := consumers[i].Invoke("call", 40, 0); err != nil || !slices.Equal(got, []uint64{41 + uint64(i)}) {
						t.Fatal("foreign host exact callback after logical close", got, err)
					}
				}
				if err := pc.Close(); err != nil {
					t.Fatal(err)
				}
				for i, consumer := range consumers {
					if got, err := consumer.Invoke("call", 50, 0); err != nil || !slices.Equal(got, []uint64{51 + uint64(i)}) {
						t.Fatal("foreign host after compiled close", got, err)
					}
				}
			})
		}
	}
}

func rv64DynamicHostCloseModule(reference bool) []byte {
	im := append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0)
	tail := []byte{0x20, 0}
	if reference {
		tail = append(tail, 0xd2, 0, 0x15, 0, 0x0b)
	} else {
		tail = append(tail, 0x41, 0, 0x13, 0, 0, 0x0b)
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "flag", wasm.I32, true), im)),
		wasmtest.Section(3, []byte{2, 0, 0}), wasmtest.Section(4, []byte{1, 0x70, 0, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 2))), wasmtest.Section(9, []byte{1, 0, 0x41, 0, 0x0b, 1, 0}),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tail), wasmtest.Code([]byte{0x20, 0, 0x10, 1, 0x24, 0, 0x23, 0, 0x0b}))))
}

func TestRV64PublicDynamicHostTailCancellationAndClose(t *testing.T) {
	for _, owned := range []bool{false, true} {
		for _, reference := range []bool{false, true} {
			for _, closeRoot := range []bool{false, true} {
				t.Run(fmt.Sprintf("artifacttrue-owned%t-reference%t-close%t", owned, reference, closeRoot), func(t *testing.T) {
					rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
					defer rv64HostCertificateCloseRuntime(t, rt)
					flag := NewGlobalI32(0, true)
					defer flag.Close()
					entered, release := make(chan struct{}), make(chan struct{})
					var once sync.Once
					unblock := func() { once.Do(func() { close(release) }) }
					defer unblock()
					c := rv64NativeTailCompile(t, fmt.Sprintf("dynamic-host-close-reference%t", reference), rv64DynamicHostCloseModule(reference), true)
					host := func(v int32) int32 { close(entered); <-release; return v + 1 }
					binding := rv64DynamicHostBinding(t, rt, owned, host, c.importFuncSigs[0])
					in := rv64HostTailInstantiate(t, rt, c, NewImports().Function("env", "target", binding).Global("env", "flag", flag))
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					done, finished := make(chan error, 1), make(chan struct{})
					go func() { defer close(finished); _, err := in.InvokeContext(ctx, "call", 7); done <- err }()
					defer func() {
						cancel()
						unblock()
						select {
						case <-finished:
						case <-time.After(5 * time.Second):
							t.Error("dynamic host cleanup delayed")
						}
					}()
					select {
					case <-entered:
					case <-time.After(5 * time.Second):
						t.Fatal("dynamic host did not park")
					}
					if err := c.Close(); err != nil {
						t.Fatal(err)
					}
					if closeRoot {
						if err := in.Close(); err != nil {
							t.Fatal(err)
						}
					} else {
						cancel()
					}
					if in.jm == nil || in.pluginState.Load().railshotInvocation.tailGenerationBudget.count == 0 {
						t.Fatal("parked dynamic host lost generation")
					}
					if owner, ok := binding.(*HostFuncRef); ok && owner.Close() == nil {
						t.Fatal("parked owned callback released")
					}
					deadline := time.Now().Add(5 * time.Second)
					for atomic.LoadUint32((*uint32)(unsafe.Pointer(&in.trap[0]))) != uint32(coreruntime.TrapInterrupted) && time.Now().Before(deadline) {
						runtime.Gosched()
					}
					if atomic.LoadUint32((*uint32)(unsafe.Pointer(&in.trap[0]))) != uint32(coreruntime.TrapInterrupted) {
						t.Fatal("dynamic host interruption not delivered")
					}
					unblock()
					select {
					case err := <-done:
						var trap *coreruntime.TrapError
						if closeRoot {
							if !errors.As(err, &trap) || trap.Code != coreruntime.TrapInterrupted {
								t.Fatal("dynamic host Close unwind", err)
							}
						} else if !errors.Is(err, context.Canceled) {
							t.Fatal("dynamic host cancel unwind", err)
						}
					case <-time.After(5 * time.Second):
						t.Fatal("dynamic host failed to unwind")
					}
					if flag.Get() != 0 || in.pluginState.Load().railshotInvocation.tailGenerationBudget.count != 0 || in.nativeContext != 0 && rv64EntryCapability(in) != 0 {
						t.Fatal("interrupted tail ran continuation or leaked generation")
					}
					if err := in.Close(); err != nil {
						t.Fatal(err)
					}
					if owner, ok := binding.(*HostFuncRef); ok && owner.Close() != nil {
						t.Fatal("completed owned callback retained")
					}
				})
			}
		}
	}
}
