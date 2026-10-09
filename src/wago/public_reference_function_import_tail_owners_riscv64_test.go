//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64ReferenceImportTailConsumer(vector, shared, wide bool, route string) []byte {
	typ := wasm.F64
	if vector {
		typ = wasm.V128
	}
	params := []wasm.ValType{wasm.I32, typ, wasm.I32, wasm.I32}
	results := []wasm.ValType{typ, wasm.I32, wasm.I32}
	imports := [][]byte{append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0)}
	if shared {
		flag := byte(1)
		if wide {
			flag = 5
		}
		imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, flag, 1, 3))
	}
	body := []byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3}
	switch route {
	case "ordinary":
		body = append(body, 0x10, 0)
	case "static-tail":
		body = append(body, 0x12, 0)
	case "reference-call":
		body = append(body, 0xd2, 0, 0x14, 0)
	case "reference-tail":
		body = append(body, 0xd2, 0, 0x15, 0)
	default:
		panic("invalid imported-tail consumer")
	}
	if route == "ordinary" || route == "reference-call" {
		body = append(body, 0x23, 0, 0x41)
		body = append(body, wasmtest.SLEB32(113)...)
		body = append(body, 0x47, 4, 0x40, 0, 0x0b)
	}
	body = append(body, 0x0b)
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{2, 0, 1})}
	if !shared {
		sections = append(sections, wasmtest.Section(5, []byte{1, 5, 1, 3}))
	}
	init := append(append([]byte{0x41}, wasmtest.SLEB32(113)...), 0x0b)
	sections = append(sections, wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, init))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1), wasmtest.ExportEntry("probe", 0, 2))), wasmtest.Section(9, []byte{1, 3, 0, 1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code([]byte{0x23, 0, 0x0b}))))
	return wasmtest.Module(sections...)
}

func rv64ReferenceImportTailArgs(vector bool, choice, null uint64) ([]uint64, []uint64) {
	args := []uint64{0xdeadbeeffffffffd, 0x7ff0000000000001}
	if vector {
		args = append(args, 0x80000000deadbeef)
	}
	payload := slices.Clone(args[1:])
	args = append(args, 0xdeadbeef00000000|choice, 0xcafefeed00000000|null)
	return args, payload
}

func rv64ReferenceImportTailCapability(in *Instance) uint64 {
	return binary.LittleEndian.Uint64(rv64NativeInvocationContext(in)[abi.RV64TailCapabilityOffset:])
}

func TestRV64PublicReferenceFunctionImportedTailsForeignOwners(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, wide := range []bool{false, true} {
				for _, sharedHome := range []bool{false, true} {
					for _, route := range []string{"ordinary", "static-tail", "reference-call", "reference-tail"} {
						t.Run(fmt.Sprintf("artifact%t-vector%t-wide%t-shared%t-%s", artifact, vector, wide, sharedHome, route), func(t *testing.T) {
							rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
							defer rv64HostCertificateCloseRuntime(t, rt)
							rc := rv64NativeTailCompile(t, fmt.Sprintf("import-tail-resource-provider-v%t-w%t", vector, wide), wasmtest.ClosedNumericImportResources(vector, wide), artifact)
							resource := rv64NativeTokenInstantiate(t, rt, rc, NewImports())
							im, memories := rv64ReferenceResourceImportBindings(t, resource)
							for _, memory := range memories {
								defer memory.Close()
							}
							gc := rv64NativeTailCompile(t, fmt.Sprintf("import-tail-grower-w%t", !wide), rv64ReferenceResourceGrower(!wide), artifact)
							grower := rv64NativeTokenInstantiate(t, rt, gc, NewImports().Memory("env", "memory", memories[1]))
							nc := rv64NativeTailCompile(t, "reference-imports-native", rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), artifact)
							native := rv64NativeTokenInstantiate(t, rt, nc, NewImports())
							binding, err := native.ExportedFunc("identity")
							if err != nil {
								t.Fatal(err)
							}
							relayCode := rv64NativeTailCompile(t, "import-tail-native-relay", rv64ReferenceImportRelay(), artifact)
							var relays []*Instance
							for range 2 {
								relay := rv64NativeTokenInstantiate(t, rt, relayCode, NewImports().Function("env", "target", binding))
								relays = append(relays, relay)
								binding, err = relay.ExportedFunc("alias")
								if err != nil {
									t.Fatal(err)
								}
							}
							entered, release := make(chan struct{}), make(chan struct{})
							var parkOnce, releaseOnce sync.Once
							unblock := func() { releaseOnce.Do(func() { close(release) }) }
							defer unblock()
							calls := 0
							shared := HostCallFunc(func(call HostCall) {
								calls++
								if len(call.ParamSlots()) != 1 || len(call.ResultSlots()) != 1 || call.ParamSlots()[0] != 0xfffffffd {
									panic("private resource tail shared transport")
								}
								parkOnce.Do(func() { close(entered); <-release })
								runtime.GC()
								call.ResultSlots()[0] = 0xcafebeeffffffffe
							})
							pc := rv64NativeTailCompile(t, fmt.Sprintf("import-tail-resources-v%t-w%t", vector, wide), wasmtest.ClosedReferenceResourceImportTails(vector, true, wide), artifact)
							owned := rv64DynamicHostBinding(t, rt, true, HostCallFunc(func(call HostCall) {
								calls++
								if len(call.ParamSlots()) != 1 || len(call.ResultSlots()) != 1 || call.ParamSlots()[0] != 0xfffffffd {
									panic("private resource tail owned transport")
								}
								call.ResultSlots()[0] = 0xcafebeef00000001
							}), pc.importFuncSigs[2])
							selected := rv64NativeTokenInstantiate(t, rt, pc, im.Function("env.calls", "native.fn", binding).Function("env.calls", "shared.fn", shared).Function("env.calls", "owned.fn", owned))
							capability := rv64ReferenceImportTailCapability(selected)
							if capability == 0 || selected.pluginState.Load().railshotInvocation.tailDispatcher == nil || selected.jm != resource.jm || selected.memoryDir.memories[1].jobMemory() != selected.memoryDir.memories[2].jobMemory() {
								t.Fatal("selected static/resource factory")
							}
							target, err := selected.ExportedFunc("run")
							if err != nil {
								t.Fatal(err)
							}
							cc := rv64NativeTailCompile(t, fmt.Sprintf("import-tail-caller-v%t-s%t-w%t-%s", vector, sharedHome, wide, route), rv64ReferenceImportTailConsumer(vector, sharedHome, wide, route), artifact)
							ci := NewImports().Function("env", "target", target)
							if sharedHome {
								ci.Memory("env", "memory", memories[0])
							}
							caller := rv64NativeTokenInstantiate(t, rt, cc, ci)
							if (caller.jm == selected.jm) != sharedHome {
								t.Fatal("foreign source memory home")
							}
							args, payload := rv64ReferenceImportTailArgs(vector, 1, 0)
							type result struct {
								values []uint64
								err    error
							}
							done := make(chan result, 1)
							finished := make(chan struct{})
							go func() {
								defer close(finished)
								values, err := caller.Invoke("call", args...)
								done <- result{values, err}
							}()
							defer func() {
								unblock()
								select {
								case <-finished:
								case <-time.After(5 * time.Second):
									t.Error("resource tail failed to join")
								}
							}()
							select {
							case <-entered:
							case <-time.After(5 * time.Second):
								t.Fatal("resource tail did not park")
							}
							if got, err := grower.Invoke("grow", 1); err != nil || !slices.Equal(got, []uint64{1}) {
								t.Fatal("parked tail alias grow", got, err)
							}
							raw := []uint64{0x7ff0000000000042}
							if vector {
								raw = append(raw, 0x80000000aabbccdd)
							}
							memory := selected.memoryDir.memories[2].jobMemory().HostBytes()
							for i, value := range raw {
								binary.LittleEndian.PutUint64(memory[65536+64+8*i:], value)
							}
							for _, owner := range append([]*Instance{selected, resource, native}, relays...) {
								if err := owner.Close(); err != nil {
									t.Fatal(err)
								}
								if (owner == selected || owner == resource || owner == native) && owner.resourcesClosed {
									t.Fatal("parked private tail lost physical owner")
								}
							}
							for _, code := range []*Compiled{pc, rc, nc, relayCode} {
								if err := code.Close(); err != nil {
									t.Fatal(err)
								}
							}
							unblock()
							select {
							case result := <-done:
								if result.err != nil || !slices.Equal(result.values, append(slices.Clone(payload), 0xfffffffe, 113)) {
									t.Fatal("retired resource private-tail continuation", result.values, result.err)
								}
							case <-time.After(5 * time.Second):
								t.Fatal("resource tail resume timeout")
							}
							if readGlobalObject(selected.globalCells[2], ValI32) != 113 || readGlobalObject(selected.globalCells[3], ValI64) != 2 || readGlobalObject(selected.globalCells[0], ValI32) != 12 {
								t.Fatal("tail borrowed cell/size continuation")
							}
							if vector {
								for _, index := range []int{1, 4} {
									if got := readGlobalObjectV128(selected.globalCells[index]); !slices.Equal(got[:], memory[65536+64:65536+80]) {
										t.Fatal("tail raw borrowed vector")
									}
								}
							} else if readGlobalObject(selected.globalCells[1], ValF64) != raw[0] || readGlobalObject(selected.globalCells[4], ValF64) != raw[0] {
								t.Fatal("tail raw borrowed NaN")
							}
							if rv64ReferenceImportTailCapability(selected) != capability || selected.pluginState.Load().railshotInvocation.tailDispatcher == nil {
								t.Fatal("selected static capability not restored")
							}
							for _, choice := range []uint64{0, 1, 2} {
								for _, null := range []uint64{0, 1} {
									args, payload := rv64ReferenceImportTailArgs(vector, choice, null)
									before := calls
									want := append(payload, []uint64{0xfffffffd, 0xfffffffe, 1}[choice], 113)
									if got, err := caller.Invoke("call", args...); err != nil || !slices.Equal(got, want) || calls-before != map[bool]int{false: 0, true: 1}[choice != 0] {
										t.Fatal("retired native relay/private tail reuse", choice, null, got, err)
									}
									if rv64ReferenceImportTailCapability(selected) != capability {
										t.Fatal("reused static capability not restored")
									}
								}
							}
							if got, err := caller.Invoke("probe"); err != nil || !slices.Equal(got, []uint64{113}) {
								t.Fatal("caller home/context restoration", got, err)
							}
							for _, owner := range []*Instance{caller, selected, native} {
								if owner.pluginState.Load().railshotInvocation.tailGenerationBudget.count != 0 || owner.pluginState.Load().railshotInvocation.definedTailOwner != nil {
									t.Fatal("tail generation retained after return")
								}
							}
							if err := caller.Close(); err != nil {
								t.Fatal(err)
							}
							if !selected.resourcesClosed || !native.resourcesClosed || resource.resourcesClosed {
								t.Fatal("proper-tail final owner/resource borrower lifetime")
							}
							for _, relay := range relays {
								if !relay.resourcesClosed {
									t.Fatal("native tail relay retained")
								}
							}
							if err := grower.Close(); err != nil {
								t.Fatal(err)
							}
							if !resource.resourcesClosed {
								t.Fatal("proper-tail resource survived last borrower")
							}
							if err := owned.(*HostFuncRef).Close(); err != nil {
								t.Fatal("proper-tail callback retained", err)
							}
						})
					}
				}
			}
		}
	}
}
