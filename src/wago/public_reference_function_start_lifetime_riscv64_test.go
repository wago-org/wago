//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
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

func rv64ReferenceStartNative(t *testing.T, rt *Runtime, artifact bool) (*Instance, *InstanceExport) {
	t.Helper()
	c := rv64NativeTailCompile(t, "reference-imports-native", rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), artifact)
	in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
	f, err := in.ExportedFunc("identity")
	if err != nil {
		t.Fatal(err)
	}
	return in, f
}

func TestRV64PublicReferenceFunctionDefinedStartReentry(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-vector%t", artifact, vector), func(t *testing.T) {
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
				defer rv64HostCertificateCloseRuntime(t, rt)
				_, _, _, im, raw := rv64ReferenceStartProvider(t, rt, artifact, vector, true, true)
				native, nf := rv64ReferenceStartNative(t, rt, artifact)
				pc := rv64NativeTailCompile(t, fmt.Sprintf("reference-start-reentry-v%t", vector), wasmtest.ClosedReferenceDefinedStart(vector, true, true, 2, true, nil), artifact)
				calls := 0
				gate := newPluginCallGate("rv64-private-start-reentry")
				rt.storeHooks(&hookRegistry{operationGates: []*pluginCallGate{gate}})
				owned := rv64DynamicHostBinding(t, rt, true, CallerHostCallFunc(func(caller Caller, call HostCall) {
					calls++
					in := caller.in
					capability := rv64ReferenceImportTailCapability(in)
					reservation := currentInvocationReservation(in)
					if reservation == nil || reservation != in.constructionReservationSnapshot() || !reservation.allows(gate) || gate.state.Load() == 0 || !in.constructionIsActive() || capability == 0 || !rv64ReferenceStartRows(in) {
						panic("nested start not constructed")
					}
					got, err := in.InvokeFromHost(context.Background(), caller, "probe")
					if err != nil || !slices.Equal(got, []uint64{1}) {
						panic(fmt.Sprintf("nested constructor probe %v %v", got, err))
					}
					got, err = native.InvokeFromHost(context.Background(), caller, "identity", 0xdeadbeef80000001)
					if err != nil || !slices.Equal(got, []uint64{0x80000001}) {
						panic("nested foreign start probe")
					}
					if !caller.valid() || !in.constructionIsActive() || rv64ReferenceImportTailCapability(in) != capability || currentInvocationReservation(in) != reservation {
						panic("nested start scope restoration")
					}
					runtime.GC()
					call.ResultSlots()[0] = 0xcafebeefffffffff
				}), pc.importFuncSigs[2])
				in := rv64NativeTokenInstantiate(t, rt, pc, im.Function("env.calls", "native.fn", nf).Function("env.calls", "shared.fn", nf).Function("env.calls", "owned.fn", owned))
				rv64ReferenceStartState(t, in, vector, 0xffffffff, raw)
				if calls != 2 || gate.state.Load() != 0 {
					t.Fatal("nested entries reran start", calls)
				}
				in.Close()
				if err := owned.(*HostFuncRef).Close(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestRV64PublicReferenceFunctionDefinedStartParkedConstruction(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, action := range []string{"resume", "cancel", "instance-close", "runtime-close"} {
				t.Run(fmt.Sprintf("artifact%t-vector%t-%s", artifact, vector, action), func(t *testing.T) {
					owners := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
					defer rv64HostCertificateCloseRuntime(t, owners)
					rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
					defer rv64HostCertificateCloseRuntime(t, rt)
					provider, grower, memories, im, raw := rv64ReferenceStartProvider(t, owners, artifact, vector, true, false)
					native, nf := rv64ReferenceStartNative(t, owners, artifact)
					pc := rv64NativeTailCompile(t, fmt.Sprintf("reference-start-v%t-ttrue-wtrue-c2-pfalse", vector), wasmtest.ClosedReferenceDefinedStart(vector, true, true, 2, false, nil), artifact)
					entered, release := make(chan *Instance, 1), make(chan struct{})
					var once sync.Once
					unblock := func() { once.Do(func() { close(release) }) }
					defer unblock()
					calls := 0
					owned := rv64DynamicHostBinding(t, rt, true, CallerHostCallFunc(func(caller Caller, call HostCall) {
						calls++
						if calls == 1 {
							entered <- caller.in
							<-release
						}
						runtime.GC()
						call.ResultSlots()[0] = 0xcafebeefffffffff
					}), pc.importFuncSigs[2])
					im.Function("env.calls", "native.fn", nf).Function("env.calls", "shared.fn", nf).Function("env.calls", "owned.fn", owned)
					mod, err := rt.Module(pc)
					if err != nil {
						t.Fatal(err)
					}
					defer mod.Close()
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
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
							t.Error("constructor failed to join")
						}
					}()
					var active *Instance
					select {
					case active = <-entered:
					case <-time.After(5 * time.Second):
						t.Fatal("start failed to park")
					}
					capability := rv64ReferenceImportTailCapability(active)
					if !active.constructionIsActive() || active.resourcesClosed || !rv64ReferenceStartRows(active) || capability == 0 || active.invocationState.Load()&instanceInvocationCount == 0 {
						t.Fatal("parked construction lease")
					}
					if got, err := grower.Invoke("grow", 1); err != nil || !slices.Equal(got, []uint64{1}) {
						t.Fatal("parked start growth", got, err)
					}
					for i, value := range raw {
						binary.LittleEndian.PutUint64(memories[1].UnsafeBytes()[65536+64+8*i:], value)
					}
					for _, owner := range []*Instance{provider, native} {
						if err := owner.Close(); err != nil {
							t.Fatal(err)
						}
						if owner.resourcesClosed {
							t.Fatal("parked start lost physical import owner")
						}
					}
					if err := pc.Close(); err != nil {
						t.Fatal(err)
					}
					if owned.(*HostFuncRef).Close() == nil {
						t.Fatal("parked start released owned callback")
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
					if action != "resume" {
						deadline := time.Now().Add(5 * time.Second)
						for atomic.LoadUint32((*uint32)(unsafe.Pointer(&active.trap[0]))) != uint32(coreruntime.TrapInterrupted) && time.Now().Before(deadline) {
							runtime.Gosched()
						}
						if atomic.LoadUint32((*uint32)(unsafe.Pointer(&active.trap[0]))) != uint32(coreruntime.TrapInterrupted) {
							t.Fatal("constructor interrupt not delivered")
						}
						if !active.constructionIsActive() || active.resourcesClosed {
							t.Fatal("closed constructor freed parked stack")
						}
					}
					unblock()
					var outcome result
					select {
					case outcome = <-done:
					case <-time.After(5 * time.Second):
						t.Fatal("start unwind timeout")
					}
					if action == "resume" {
						if outcome.err != nil || outcome.in != active {
							t.Fatal("retired-owner start resume", outcome.err)
						}
						rv64ReferenceStartState(t, active, vector, 0xffffffff, raw)
						if rv64ReferenceImportTailCapability(active) != capability || calls != 2 {
							t.Fatal("resumed start static capability/callback count")
						}
						active.Close()
					} else {
						if outcome.in != nil {
							outcome.in.Close()
							t.Fatal("failed constructor returned instance")
						}
						if action == "cancel" {
							if !errors.Is(outcome.err, context.Canceled) {
								t.Fatal(outcome.err)
							}
						} else {
							ownedTableTrap(t, outcome.err, coreruntime.TrapInterrupted)
						}
						if calls != 1 || readGlobalObject(provider.globalCells[1], ValI32) != 12 || provider.jm.HostBytes()[49] != 42 || provider.jm.HostBytes()[50] != 0 {
							t.Fatal("failed start effects/continuation")
						}
					}
					if active.constructionIsActive() || !active.resourcesClosed || active.invocationState.Load()&instanceInvocationCount != 0 || active.pluginState.Load().railshotInvocation.tailDispatcher != nil || active.pluginState.Load().railshotInvocation.tailGenerationBudget.count != 0 || !native.resourcesClosed {
						t.Fatal("constructor physical/static/code roots survived join")
					}
					if action == "runtime-close" {
						wait, stop := context.WithTimeout(context.Background(), 5*time.Second)
						defer stop()
						if err := rt.WaitClosed(wait); err != nil {
							t.Fatal(err)
						}
					}
					if err := owned.(*HostFuncRef).Close(); err != nil {
						t.Fatal("constructor callback roots", err)
					}
					if provider.resourcesClosed {
						t.Fatal("independent grower lost retired provider")
					}
					grower.Close()
					if !provider.resourcesClosed {
						t.Fatal("provider roots survived last constructor borrower")
					}
				})
			}
		}
	}
}

func TestRV64PublicReferenceFunctionDefinedStartFailures(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, behavior := range []string{"unreachable", "division", "memory", "recursion", "host-trap", "host-panic", "pre-cancel", "initializer", "loop"} {
				t.Run(fmt.Sprintf("artifact%t-vector%t-%s", artifact, vector, behavior), func(t *testing.T) {
					rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig().WithNativeStackBytes(MinNativeStackBytes)))
					defer rv64HostCertificateCloseRuntime(t, rt)
					provider, _, memories, im, _ := rv64ReferenceStartProvider(t, rt, artifact, vector, false, true)
					_, nf := rv64ReferenceStartNative(t, rt, artifact)
					var fault []byte
					trap := coreruntime.TrapUnreachable
					switch behavior {
					case "unreachable":
						fault = []byte{0}
					case "division":
						fault = []byte{0x41, 1, 0x41, 0, 0x6d, 0x1a}
						trap = coreruntime.TrapDivZero
					case "memory":
						fault = []byte{0x41, 0x7f, 0x2d, 0, 0, 0x1a}
						trap = coreruntime.TrapLinMemOutOfBounds
					case "loop":
						fault = []byte{0x41, 0xc0, 0, 0x41, 1, 0x36, 2, 0, 3, 0x40, 0x0c, 0, 0x0b}
					case "recursion":
						fault = []byte{0x10, 3}
						trap = coreruntime.TrapStackFenceBreached
					}
					source := wasmtest.ClosedReferenceDefinedStart(vector, true, false, 2, false, fault)
					if behavior == "initializer" {
						source = wasmtest.ClosedReferenceDefinedStartBadData(vector, false)
					}
					pc := rv64NativeTailCompile(t, fmt.Sprintf("reference-start-failure-v%t-%s", vector, behavior), source, artifact)
					cause := errors.New("private start host trap")
					calls := 0
					var captured *Instance
					owned := rv64DynamicHostBinding(t, rt, true, CallerHostCallFunc(func(caller Caller, call HostCall) {
						calls++
						captured = caller.in
						if behavior == "host-trap" {
							panic(HostTrap{Err: cause})
						}
						if behavior == "host-panic" {
							panic(cause)
						}
						call.ResultSlots()[0] = 0xffffffff
					}), pc.importFuncSigs[2])
					im.Function("env.calls", "native.fn", nf).Function("env.calls", "shared.fn", nf).Function("env.calls", "owned.fn", owned)
					mod, err := rt.Module(pc)
					if err != nil {
						t.Fatal(err)
					}
					defer mod.Close()
					cache := pc.loadCodeCache()
					refs := cache.refs
					roots := provider.referenceLifetime().snapshot().ResourceRoots
					for attempt := range 2 {
						ctx, cancel := context.WithCancel(context.Background())
						if behavior == "loop" {
							cancel()
							ctx, cancel = context.WithTimeout(context.Background(), 200*time.Millisecond)
							binary.LittleEndian.PutUint32(memories[0].UnsafeBytes()[64:], 0)
						}
						if behavior == "pre-cancel" {
							cancel()
						}
						before := readGlobalObject(provider.globalCells[1], ValI32)
						in, err := rt.Instantiate(ctx, mod, WithImports(im))
						cancel()
						if in != nil {
							in.Close()
							t.Fatal("failed start returned instance")
						}
						if err == nil {
							t.Fatal("failed start succeeded")
						}
						if behavior == "host-trap" {
							if !errors.Is(err, cause) {
								t.Fatal("host trap cause", err)
							}
						} else if behavior == "pre-cancel" {
							if !errors.Is(err, context.Canceled) {
								t.Fatal(err)
							}
						} else if behavior == "loop" {
							if !errors.Is(err, context.DeadlineExceeded) || binary.LittleEndian.Uint32(memories[0].UnsafeBytes()[64:]) != 1 {
								t.Fatal("private start loop did not execute or interrupt", err)
							}
						} else if behavior == "host-panic" {
							if !errors.Is(err, cause) || captured == nil || calls != attempt+1 {
								t.Fatal("arbitrary host panic cause/callback", err, calls)
							}
						} else if behavior == "initializer" {
							if !strings.Contains(err.Error(), "active data segment 0 out of bounds on memory 0: offset 65536 + length 1 > memory size 65536") {
								t.Fatal("wrong initializer failure", err)
							}
						} else {
							ownedTableTrap(t, err, trap)
						}
						if behavior == "pre-cancel" || behavior == "initializer" {
							if calls != 0 || readGlobalObject(provider.globalCells[1], ValI32) != before || memories[0].UnsafeBytes()[49] != 0 {
								t.Fatal("failed pre-entry ran start")
							}
						} else if behavior != "recursion" {
							if readGlobalObject(provider.globalCells[1], ValI32) != before+1 || memories[0].UnsafeBytes()[48] != 7 || memories[0].UnsafeBytes()[49] != 42 || memories[0].UnsafeBytes()[50] != 0 {
								t.Fatal("failed start external effects rolled back or continued")
							}
						}
						if captured != nil && (!captured.resourcesClosed || captured.constructionIsActive() || captured.pluginState.Load().railshotInvocation.tailDispatcher != nil) {
							t.Fatal("failed host start retained construction")
						}
						if cache.refs != refs || provider.referenceLifetime().snapshot().ResourceRoots != roots {
							t.Fatal("failed constructor leaked code/provider roots", attempt)
						}
					}
					if err := owned.(*HostFuncRef).Close(); err != nil {
						t.Fatal(err)
					}
				})
			}
		}
	}
}
