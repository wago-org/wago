//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
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

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64PublicReferenceFunctionNumericCallsParked(t *testing.T) {
	rv64ReferenceFunctionNumericCallsParked(t, false)
}
func TestRV64PublicReferenceFunctionOwnedTableParked(t *testing.T) {
	rv64ReferenceFunctionNumericCallsParked(t, true)
}
func rv64ReferenceFunctionNumericCallsParked(t *testing.T, ownedTable bool) {
	t.Helper()
	product := "numeric-calls"
	if ownedTable {
		product = "owned-table"
	}
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, start := range []int{1, 2} {
				for _, action := range []string{"resume", "cancel", "instance-close", "runtime-close", "host-trap"} {
					t.Run(fmt.Sprintf("artifact%t-vector%t-start%d-%s", artifact, vector, start, action), func(t *testing.T) {
						owners := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
						defer rv64HostCertificateCloseRuntime(t, owners)
						rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
						defer rv64HostCertificateCloseRuntime(t, rt)
						provider, grower, _, im, _ := rv64ReferenceStartProvider(t, owners, artifact, vector, true, false)
						nc := rv64NativeTailCompile(t, "reference-imports-native", rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), artifact)
						native := rv64NativeTokenInstantiate(t, owners, nc, NewImports())
						nf, err := native.ExportedFunc("identity")
						if err != nil {
							t.Fatal(err)
						}
						pc := rv64NativeTailCompile(t, fmt.Sprintf("reference-%s-v%t-ttrue-wtrue-s%d-target0", product, vector, start), rv64ReferenceNumericCallsFixture(ownedTable, false, vector, true, true, start, 0), artifact)
						ctx, cancel := context.WithCancel(context.Background())
						defer cancel()
						entered, release := make(chan Caller, 1), make(chan struct{})
						var once sync.Once
						unblock := func() { once.Do(func() { close(release) }) }
						defer unblock()
						cause := errors.New("private local-cell parked trap")
						calls := 0
						probe := CallerHostCallFunc(func(caller Caller, _ HostCall) {
							calls++
							in := caller.in
							if readGlobalObject(in.globalCells[6], ValI32) == 0 {
								if _, err := in.InvokeFromHost(ctx, caller, "initialize"); err != nil {
									panic(HostTrap{Err: err})
								}
								return
							}
							if !in.constructionIsActive() || !rv64ReferenceNumericCallsRows(in) || caller.reservation == nil {
								panic("parked private cell construction")
							}
							for _, cell := range []int{2, 3} {
								value := readGlobalObject(in.globalCells[cell], ValFuncRef)
								want := 0
								if cell == 3 {
									want = 3
								}
								if index, ok := in.funcrefDescriptorIndex(value); !ok || index != want || value>>32 == 0 {
									panic("distinct parked private references")
								}
							}
							if _, err := in.InvokeFromHost(context.Background(), caller, "clear"); err != nil {
								panic(HostTrap{Err: err})
							}
							entered <- caller
							<-release
							runtime.GC()
							if action == "host-trap" {
								panic(HostTrap{Err: cause})
							}
						})
						owned := rv64DynamicHostBinding(t, rt, true, probe, pc.importFuncSigs[1])
						gate := newPluginCallGate("rv64-local-cell-park")
						rt.storeHooks(&hookRegistry{operationGates: []*pluginCallGate{gate}})
						im.Function("env.calls", "unused.fn", nf).Function("env.calls", "start.fn", owned).Function("env.calls", "finish.fn", func(v int32) int32 { return v + 1 })
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
								t.Error("private cell constructor did not join")
							}
						}()
						var caller Caller
						select {
						case caller = <-entered:
						case <-time.After(5 * time.Second):
							t.Fatal("private cell callback did not park")
						}
						active := caller.in
						for _, cell := range []int{2, 3, 4, 5} {
							if readGlobalObject(active.globalCells[cell], ValFuncRef) != 0 {
								t.Fatal("current/snapshot cell unexpectedly retained carrier while parked", cell)
							}
						}
						for _, owner := range []*Instance{native, provider} {
							if err := owner.Close(); err != nil {
								t.Fatal(err)
							}
						}
						nc.Close()
						pc.Close()
						if native.resourcesClosed || provider.resourcesClosed || active.resourcesClosed || !caller.valid() || owned.(*HostFuncRef).Close() == nil {
							t.Fatal("parked private cells lost physical owners")
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
						if action == "cancel" || action == "instance-close" || action == "runtime-close" {
							deadline := time.Now().Add(5 * time.Second)
							for atomic.LoadUint32((*uint32)(unsafe.Pointer(&active.trap[0]))) != uint32(coreruntime.TrapInterrupted) && time.Now().Before(deadline) {
								runtime.Gosched()
							}
							if atomic.LoadUint32((*uint32)(unsafe.Pointer(&active.trap[0]))) != uint32(coreruntime.TrapInterrupted) || !active.constructionIsActive() || active.resourcesClosed {
								t.Fatal("parked private cell interrupt/constructor lease")
							}
						}
						unblock()
						select {
						case outcome := <-done:
							if action == "resume" {
								if outcome.in != active || outcome.err != nil {
									t.Fatal("retired private cell resume", outcome.err)
								}
								rv64ReferenceNumericCallsCheck(t, active, vector, 3)
								active.Close()
							} else {
								if outcome.in != nil {
									outcome.in.Close()
									t.Fatal("failed private cell constructor returned instance")
								}
								if action == "cancel" {
									if !errors.Is(outcome.err, context.Canceled) {
										t.Fatal(outcome.err)
									}
								} else if action == "host-trap" {
									if !errors.Is(outcome.err, cause) {
										t.Fatal(outcome.err)
									}
								} else {
									ownedTableTrap(t, outcome.err, coreruntime.TrapInterrupted)
								}
								if provider.jm.HostBytes()[49] != 0 {
									t.Fatal("failed private cell constructor executed continuation")
								}
							}
						case <-time.After(5 * time.Second):
							t.Fatal("private cell unwind timeout")
						}
						wantCalls := 1
						if start == 2 {
							wantCalls++
						}
						if calls != wantCalls || caller.valid() || active.constructionIsActive() || !active.resourcesClosed || active.invocationState.Load()&instanceInvocationCount != 0 || gate.state.Load() != 0 || active.pluginState.Load().railshotInvocation.tailDispatcher != nil || readGlobalObject(provider.globalCells[1], ValI32) != 12 {
							t.Fatal("private cell join retained scope/effects")
						}
						rv64ReferenceImportedStartRetired(t, native, nc)
						if err := owned.(*HostFuncRef).Close(); err != nil {
							t.Fatal(err)
						}
						if action == "runtime-close" {
							wait, stop := context.WithTimeout(context.Background(), 5*time.Second)
							defer stop()
							if err := rt.WaitClosed(wait); err != nil {
								t.Fatal(err)
							}
						}
						grower.Close()
						if !provider.resourcesClosed {
							t.Fatal("parked cell resource provider retained")
						}
					})
				}
			}
		}
	}
}

func TestRV64PublicReferenceFunctionNumericCallsForeignOwners(t *testing.T) {
	rv64ReferenceFunctionNumericCallsForeignOwners(t, false)
}
func TestRV64PublicReferenceFunctionOwnedTableForeignOwners(t *testing.T) {
	rv64ReferenceFunctionNumericCallsForeignOwners(t, true)
}
func rv64ReferenceFunctionNumericCallsForeignOwners(t *testing.T, ownedTable bool) {
	t.Helper()
	product := "numeric-calls"
	if ownedTable {
		product = "owned-table"
	}
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, wide := range []bool{false, true} {
				for _, shared := range []bool{false, true} {
					t.Run(fmt.Sprintf("artifact%t-vector%t-wide%t-shared%t", artifact, vector, wide, shared), func(t *testing.T) {
						owners := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
						defer rv64HostCertificateCloseRuntime(t, owners)
						rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
						defer rv64HostCertificateCloseRuntime(t, rt)
						provider, grower, memories, im, _ := rv64ReferenceStartProvider(t, owners, artifact, vector, wide, false)
						nc := rv64NativeTailCompile(t, "reference-imports-native", rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), artifact)
						native := rv64NativeTokenInstantiate(t, owners, nc, NewImports())
						nf, err := native.ExportedFunc("identity")
						if err != nil {
							t.Fatal(err)
						}
						var selected, probe, bridge *Instance
						calls := 0
						var saved Caller
						callback := CallerHostCallFunc(func(caller Caller, call HostCall) {
							calls++
							saved = caller
							if caller.in != probe || selected == nil || call.ParamCount() != 0 || call.ResultCount() != 0 || selected.jm == probe.jm || !rv64ReferenceNumericCallsRows(selected) {
								panic("foreign native callback private cell owner")
							}
							if _, err := bridge.InvokeFromHost(context.Background(), caller, "clear"); err != nil {
								panic(HostTrap{Err: err})
							}
							if got, err := bridge.InvokeFromHost(context.Background(), caller, "safe"); err != nil || !slices.Equal(got, []uint64{23}) {
								panic("foreign cell native probe reentry")
							}
							if !caller.valid() || readGlobalObject(selected.globalCells[2], ValFuncRef) != 0 || readGlobalObject(selected.globalCells[3], ValFuncRef) != 0 {
								panic("foreign private cell scope/clear restoration")
							}
							runtime.GC()
						})
						counter, err := provider.ExportedGlobalObject("counter")
						if err != nil {
							t.Fatal(err)
						}
						probeCode := rv64NativeTailCompile(t, "reference-imported-start-native-sfalse", rv64PublicNativeStartProducerModule(false, nil), artifact)
						probe = rv64NativeTokenInstantiate(t, owners, probeCode, NewImports().Global("env", "flag", counter).Function("env", "host", callback))
						pf, err := probe.ExportedFunc("producer_boot")
						if err != nil {
							t.Fatal(err)
						}
						pc := rv64NativeTailCompile(t, fmt.Sprintf("reference-%s-import-init-v%t-w%t", product, vector, wide), rv64ReferenceNumericCallsFixture(ownedTable, true, vector, true, wide, 0, 0), artifact)
						selected = rv64NativeTokenInstantiate(t, rt, pc, im.Function("env.calls", "unused.fn", nf).Function("env.calls", "start.fn", pf).Function("env.calls", "finish.fn", func(v int32) int32 { return v + 1 }))
						initial := readGlobalObject(selected.globalCells[3], ValFuncRef)
						if index, ok := selected.funcrefDescriptorIndex(initial); !ok || index != 0 || initial>>32 == 0 || calls != 0 {
							t.Fatal("import initializer did not use own full-width slot", initial, index, ok)
						}
						run, err := selected.ExportedFunc("run")
						if err != nil {
							t.Fatal(err)
						}
						get, err := selected.ExportedFunc("get")
						if err != nil {
							t.Fatal(err)
						}
						typ := wasm.F64
						if vector {
							typ = wasm.V128
						}
						results := []wasm.ValType{typ, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}
						imports := [][]byte{append(append(wasmtest.Name("env"), wasmtest.Name("run")...), 0, 0), append(append(wasmtest.Name("env"), wasmtest.Name("get")...), 0, 1)}
						clear, err := selected.ExportedFunc("clear")
						if err != nil {
							t.Fatal(err)
						}
						safe, err := probe.ExportedFunc("safe")
						if err != nil {
							t.Fatal(err)
						}
						imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("clear")...), 0, 0), append(append(wasmtest.Name("env"), wasmtest.Name("safe")...), 0, 2))
						ci := NewImports().Function("env", "run", run).Function("env", "get", get).Function("env", "clear", clear).Function("env", "safe", safe)
						if shared {
							flag := byte(1)
							if wide {
								flag = 5
							}
							imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, flag, 1, 3))
							ci.Memory("env", "memory", memories[0])
						}
						consumer := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, results), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{4, 0, 1, 0, 2}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 4), wasmtest.ExportEntry("get", 0, 5), wasmtest.ExportEntry("clear", 0, 6), wasmtest.ExportEntry("safe", 0, 7))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x0b}), wasmtest.Code([]byte{0x10, 1, 0x0b}), wasmtest.Code([]byte{0x10, 2, 0x0b}), wasmtest.Code([]byte{0x10, 3, 0x0b}))))
						cc := rv64NativeTailCompile(t, fmt.Sprintf("reference-numeric-call-consumer-v%t-w%t-s%t", vector, wide, shared), consumer, artifact)
						caller := rv64NativeTokenInstantiate(t, rt, cc, ci)
						bridge = caller
						if (caller.jm == selected.jm) != shared {
							t.Fatal("foreign private cell home")
						}
						for _, owner := range []*Instance{selected, native, probe, provider} {
							if err := owner.Close(); err != nil {
								t.Fatal(err)
							}
							if owner.resourcesClosed {
								t.Fatal("foreign cell attachment lost physical owner")
							}
						}
						for _, code := range []*Compiled{pc, nc, probeCode} {
							if err := code.Close(); err != nil {
								t.Fatal(err)
							}
						}
						for attempt := range 2 {
							if _, err := caller.Invoke("run"); err != nil {
								t.Fatal("retired private cell foreign entry", err)
							}
							want := []uint64{0x7ff0000000000042}
							if vector {
								want = append(want, 0x80000000deadbeef)
							}
							null := uint64(0)
							if attempt == 1 {
								null = 1
							}
							want = append(want, 0xfffffffe, null, 0, 1, 1)
							if got, err := caller.Invoke("get"); err != nil || !slices.Equal(got, want) {
								t.Fatal("foreign private cell results", got, want, err)
							}
							// Inspect the retained producer only while the actual foreign
							// attachment owns its arena; never inspect after final Close.
							lower := readGlobalObject(selected.globalCells[4], ValFuncRef)
							local := readGlobalObject(selected.globalCells[5], ValFuncRef)
							targetValue := readGlobalObject(selected.globalCells[14], ValFuncRef)
							targetIndex, targetOK := selected.funcrefDescriptorIndex(targetValue)
							if !targetOK || targetIndex != 0 || targetValue>>32 == 0 || readGlobalObject(selected.globalCells[11], ValI64) != 0xfffffffd {
								t.Fatal("foreign numeric call_ref actual native target", targetValue, targetIndex, targetOK)
							}
							if lower != initial || attempt == 0 && local != initial || attempt == 1 && local != 0 || saved.valid() || calls != attempt+1 || readGlobalObject(provider.globalCells[1], ValI32) != uint64(13+2*attempt) {
								t.Fatal("foreign private cell exact identity/continuation")
							}
						}
						caller.Close()
						if !selected.resourcesClosed {
							t.Fatal("selected cells survived final foreign attachment")
						}
						rv64ReferenceImportedStartRetired(t, native, nc)
						rv64ReferenceImportedStartRetired(t, probe, probeCode)
						grower.Close()
						if !provider.resourcesClosed {
							t.Fatal("foreign private cell resource owner survived")
						}
					})
				}
			}
		}
	}
}
