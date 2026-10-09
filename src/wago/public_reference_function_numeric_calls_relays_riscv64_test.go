//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"fmt"
	"runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64ReferenceNumericCallsRelayModule(route string) []byte {
	imports := [][]byte{append(append(wasmtest.Name("env"), wasmtest.Name("initialize")...), 0, 0), append(append(wasmtest.Name("env"), wasmtest.Name("clear")...), 0, 0)}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))), wasmtest.Section(2, wasmtest.Vec(imports...))}
	if route == "reexport" {
		return wasmtest.Module(append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("clear", 0, 1))))...)
	}
	body := []byte{0x10, 0, 0x0b}
	switch route {
	case "static-tail":
		body = []byte{0x12, 0, 0x0b}
	case "reference-call":
		body = []byte{0xd2, 0, 0x14, 0, 0x0b}
	case "reference-tail":
		body = []byte{0xd2, 0, 0x15, 0, 0x0b}
	}
	sections = append(sections, wasmtest.Section(3, []byte{2, 0, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2), wasmtest.ExportEntry("clear", 0, 3))), wasmtest.Section(9, []byte{1, 3, 0, 1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code([]byte{0x10, 1, 0x0b}))))
	return wasmtest.Module(sections...)
}

func TestRV64PublicReferenceFunctionNumericCallsRetiredRelays(t *testing.T) {
	rv64ReferenceFunctionNumericCallsRetiredRelays(t, 0)
}
func TestRV64PublicReferenceFunctionOwnedTableRetiredRelays(t *testing.T) {
	rv64ReferenceFunctionNumericCallsRetiredRelays(t, 1)
}
func TestRV64PublicReferenceFunctionTableBulkRetiredRelays(t *testing.T) {
	rv64ReferenceFunctionNumericCallsRetiredRelays(t, 2)
}
func TestRV64PublicReferenceFunctionProperReferenceTailRetiredRelays(t *testing.T) {
	rv64ReferenceFunctionNumericCallsRetiredRelays(t, 3)
}
func TestRV64PublicReferenceFunctionProperIndirectTailRetiredRelays(t *testing.T) {
	rv64ReferenceFunctionNumericCallsRetiredRelays(t, 4)
}
func rv64ReferenceFunctionNumericCallsRetiredRelays(t *testing.T, tableStage int) {
	ownedTable := tableStage > 0
	t.Helper()
	product := "numeric-calls"
	if ownedTable {
		product = "owned-table"
	}
	if tableStage == 2 {
		product = "table-bulk"
	}
	if tableStage == 3 {
		product = "proper-reference-tail"
	}
	if tableStage == 4 {
		product = "proper-indirect-tail"
	}
	if tableStage == 5 {
		product = "expression-elements"
	}
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, route := range []string{"ordinary", "static-tail", "reference-call", "reference-tail", "reexport"} {
				t.Run(fmt.Sprintf("artifact%t-vector%t-%s", artifact, vector, route), func(t *testing.T) {
					rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
					defer rv64HostCertificateCloseRuntime(t, rt)
					provider, grower, _, im, _ := rv64ReferenceStartProvider(t, rt, artifact, vector, false, false)
					nc := rv64NativeTailCompile(t, "reference-imports-native", rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), artifact)
					native := rv64NativeTokenInstantiate(t, rt, nc, NewImports())
					nf, err := native.ExportedFunc("identity")
					if err != nil {
						t.Fatal(err)
					}
					pc := rv64NativeTailCompile(t, fmt.Sprintf("reference-%s-v%t-ttrue-wfalse-s0-target0", product, vector), rv64ReferenceNumericCallsFixture(tableStage, false, vector, true, false, 0, 0), artifact)
					calls := 0
					var bridge, selected *Instance
					var saved Caller
					callback := CallerHostCallFunc(func(caller Caller, call HostCall) {
						calls++
						saved = caller
						if caller.in != selected || call.ParamCount() != 0 || call.ResultCount() != 0 || selected == nil || !selected.closed || !rv64ReferenceNumericCallsRows(selected) {
							panic("retired relay callback identity")
						}
						if _, err := bridge.InvokeFromHost(context.Background(), caller, "clear"); err != nil {
							panic(HostTrap{Err: err})
						}
						runtime.GC()
					})
					selected = rv64NativeTokenInstantiate(t, rt, pc, im.Function("env.calls", "unused.fn", nf).Function("env.calls", "start.fn", callback).Function("env.calls", "finish.fn", func(v int32) int32 { return v + 1 }))
					init, err := selected.ExportedFunc("initialize")
					if err != nil {
						t.Fatal(err)
					}
					clear, err := selected.ExportedFunc("clear")
					if err != nil {
						t.Fatal(err)
					}
					cc := rv64NativeTailCompile(t, "reference-numeric-call-relay-"+route, rv64ReferenceNumericCallsRelayModule(route), artifact)
					bridge = rv64NativeTokenInstantiate(t, rt, cc, NewImports().Function("env", "initialize", init).Function("env", "clear", clear))
					capability := rv64ReferenceImportTailCapability(selected)
					bridgeCapability := rv64EntryCapability(bridge)
					for _, owner := range []*Instance{selected, native, provider} {
						if err := owner.Close(); err != nil || owner.resourcesClosed {
							t.Fatal("relay attachment physical owner", err)
						}
					}
					pc.Close()
					nc.Close()
					for attempt := 0; attempt < 2; attempt++ {
						if _, err := bridge.Invoke("run"); err != nil {
							t.Fatal("retired sparse owner through relay", err)
						}
						value := readGlobalObject(selected.globalCells[14], ValFuncRef)
						index, ok := selected.funcrefDescriptorIndex(value)
						wantSaved := 3
						if attempt == 1 {
							wantSaved = -1
						}
						local := readGlobalObject(selected.globalCells[5], ValFuncRef)
						localIndex, localOK := selected.funcrefDescriptorIndex(local)
						if !ok || index != 0 || value>>32 == 0 || readGlobalObject(selected.globalCells[11], ValI64) != 0xfffffffd || readGlobalObject(selected.globalCells[7], ValI32) != 0xfffffffe || wantSaved == 3 && (!localOK || localIndex != 3 || local>>32 == 0) || wantSaved < 0 && local != 0 || calls != attempt+1 || saved.valid() || readGlobalObject(provider.globalCells[1], ValI32) != uint64(12+attempt) {
							t.Fatal("retired relay actual target/carrier effects", value, local, calls)
						}
						for _, owner := range []*Instance{selected, bridge} {
							state := &owner.pluginState.Load().railshotInvocation
							if state.tableDepth != 0 || state.tailGenerationBudget.count != 0 {
								t.Fatal("relay reference activation leaked")
							}
						}
						if rv64ReferenceImportTailCapability(selected) != capability || rv64EntryCapability(bridge) != bridgeCapability {
							t.Fatal("dynamic root did not restore static capability")
						}
					}
					bridge.Close()
					if !selected.resourcesClosed {
						t.Fatal("relay retained selected producer")
					}
					rv64ReferenceImportedStartRetired(t, native, nc)
					grower.Close()
					if !provider.resourcesClosed {
						t.Fatal("relay resource provider retained")
					}
				})
			}
		}
	}
}
