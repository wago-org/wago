//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64TableEntryHostModule(shared bool) []byte {
	imports := [][]byte{rv64VectorFunctionImport("prepare", 0), rv64VectorFunctionImport("target", 1), rv64VectorFunctionImport("inspect", 0)}
	if shared {
		imports = append([][]byte{rv64VectorMemoryImport()}, imports...)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(imports...)),
		wasmtest.Section(3, []byte{2, 2, 2}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 3), wasmtest.ExportEntry("safe", 0, 4))),
		// Invoke the table-bearing producer between callbacks. Its replaced row
		// names the retired foreign target; verify its result before the next park.
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x41, 5, 0x41, 0, 0x10, 1, 0x41, 5, 0x47, 0x04, 0x40, 0, 0x0b, 0x10, 2, 0, 0x0b}), wasmtest.Code([]byte{0x41, 9, 0x0b}))),
	)
}

func TestRV64TableAttachmentLookupIdentityAndWarmAllocation(t *testing.T) {
	for _, module := range []string{"env", "a.b:c\x00λ", strings.Repeat("m", 300), strings.Repeat("m", 1100)} {
		name := "f.with.dot"
		target := &Instance{c: &Compiled{}}
		export := &InstanceExport{inst: target}
		owner := &Instance{c: &Compiled{Imports: []string{module + "." + name}}, imports: resolvedImports{
			importBindingMapKey(module, name):            export,
			importBindingMapKey(module+".f", "with.dot"): &InstanceExport{},
		}}
		owner.c.validateMemo = &validateMemo{importModuleEnds: []uint64{exactImportModuleEnd(module)}}
		for i := 0; i < 10000; i++ {
			owner.imports[importBindingMapKey("unused", strconv.Itoa(i))] = &InstanceExport{}
		}
		if got, ok := rv64NativeFunctionAttachment(owner, 0); !ok || got != export {
			t.Fatal("attachment identity", len(module), got, ok)
		}
		if len(module) > 256 {
			keys := owner.pluginState.Load().railshotInvocation.longImportKeys
			if len(keys) != 1 || keys[0] != importBindingMapKey(module, name) {
				t.Fatal("lookup cache includes undeclared bindings")
			}
		}
		if allocs := testing.AllocsPerRun(100, func() {
			got, ok := rv64NativeFunctionAttachment(owner, 0)
			if !ok || got != export {
				panic("attachment lookup")
			}
		}); allocs != 0 {
			t.Fatal("warm attachment lookup allocations", len(module), allocs)
		}
	}
}

func TestRV64OwnedTableUnsharedPreparedEntryUsesScope(t *testing.T) {
	c := ownedTableCompile(t, publicOwnedTableModule(7, false, 3, 0, 1, 2))
	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if !in.syncMode || in.nativeControlIsShared() || in.c.tableCount() == 0 {
		t.Fatal("owned-table cooperative control fixture")
	}
	// Test the metadata exclusion independently of the new public profile's
	// mandatory synchronous control, which itself selects General.
	selector := &Instance{c: in.c}
	if selector.syncMode || selector.nativeControlIsShared() || selector.preparedEntryMode() != preparedEntryGeneral || selector.preparedMemoryFreeEntryMode() != preparedEntryGeneral {
		t.Fatal("table metadata bypasses RV64 scoped admission")
	}
	fn, err := in.WasmFunc("call")
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range []func() ([]uint64, error){func() ([]uint64, error) { return in.Invoke("call", 0, 5) }, func() ([]uint64, error) { return fn.Invoke(0, 5) }} {
		if got, err := call(); err != nil || !slices.Equal(got, []uint64{12}) {
			t.Fatal("scoped unshared table entry", got, err)
		}
		pool := &in.pluginState.Load().railshotInvocation
		if pool.tableTargets[0] == nil || pool.tableDepth != 0 || pool.tableTargets[0].count != 0 {
			t.Fatal("unshared prepared entry omitted or retained scope")
		}
	}
}

func TestRV64TableEntryPreparedAdmissionAndMigratedLeaseRelease(t *testing.T) {
	root, producer, table, _ := rv64TableTargetFixture(t, false, false, false)
	if !table.retainProducerInstance(producer) {
		t.Fatal("retained table producer")
	}
	if root.preparedEntryMode() != preparedEntryGeneral || root.preparedMemoryFreeEntryMode() != preparedEntryGeneral {
		t.Fatal("RV64 table entry bypasses attachment scope")
	}
	plugin := root.lockInvocation(0)
	defer plugin.unlockInvocation()
	lease, err := root.beginNativeEntry()
	if err != nil {
		t.Fatal(err)
	}
	// A prepared host lease can migrate from the local mutex while parked.
	// Exercise that cleanup branch with an actual acquired target scope and
	// the process-wide mutex held, rather than clearing only a synthetic slot.
	if lease.local != nil {
		lease.local.Unlock()
		nativeExecutionMu.Lock()
	}
	state := preparedSessionState{fn: &WasmFunc{in: root}, entry: lease, host: true}
	plugin.nativeShareMu.Lock()
	plugin.preparedHostGate = &state.hostGate
	plugin.nativeShareMu.Unlock()
	state.dropHostLease(true)
	if plugin.railshotInvocation.tableDepth != 0 || state.entry.tables.targets != nil || state.host || plugin.preparedHostGate != nil {
		t.Fatal("migrated prepared lease retained table roots")
	}
	lease, err = root.beginNativeEntry()
	if err != nil {
		t.Fatal("reuse after migration cleanup", err)
	}
	root.unlockNativeEntry(lease)
}

func TestRV64TableEntryHostResumeMergeNestedAndColdLifetime(t *testing.T) {
	for _, shared := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("shared%t-artifact%t", shared, artifact), func(t *testing.T) {
				var stateImports *Imports
				if shared {
					_, memory := importedMemoryProvider(t)
					defer memory.Close()
					stateImports = NewImports().Memory("env", "memory", memory)
				}
				c := rv64PublicStartCompile(t, rv64PublicTableExportModule(false, shared), artifact)
				producer, err := Instantiate(c, stateImports)
				if err != nil {
					t.Fatal(err)
				}
				defer producer.Close()
				table, err := producer.ExportedTable("table")
				if err != nil {
					t.Fatal(err)
				}
				replacement, err := Instantiate(c, stateImports)
				if err != nil {
					t.Fatal(err)
				}
				defer replacement.Close()
				identity := replacement.nativeContext
				export, err := producer.ExportedFunc("call")
				if err != nil {
					t.Fatal(err)
				}
				var root *Instance
				prepare := CallerHostCallFunc(func(caller Caller, _ HostCall) {
					pool := &root.pluginState.Load().railshotInvocation
					if pool.tableDepth != 1 || pool.tableTargets[0].count != 2 {
						panic("missing initial table target entry scope")
					}
					nativeExecutionMu.Lock()
					copy(table.desc[8:40], replacement.funcRefDescs[80:112])
					nativeExecutionMu.Unlock()
					if !table.retainProducerInstance(replacement) {
						panic("replacement physical table root")
					}
					if got, err := root.InvokeFromHost(context.Background(), caller, "safe"); err != nil || !slices.Equal(got, []uint64{9}) {
						panic("nested scoped table entry")
					}
					if pool.tableDepth != 1 || pool.tableTargets[1] == nil || pool.tableTargets[1].count != 0 {
						panic("nested entry leaked target roots")
					}
					if err := replacement.Close(); err != nil {
						panic(err)
					}
				})
				inspect := HostCallFunc(func(_ HostCall) {
					pool := &root.pluginState.Load().railshotInvocation
					if pool.tableDepth != 1 || pool.tableTargets[0].count != 3 || railshotAttachedNativeOwner(root, identity) != replacement {
						panic("native resume omitted replacement owner")
					}
					if _, err := producer.Invoke("clear", 0); err != nil {
						panic(err)
					}
					runtime.GC()
					if replacement.jm == nil || railshotAttachedNativeOwner(root, identity) != replacement {
						panic("parked activation lost retired replaced target")
					}
				})
				imports := NewImports().Function("env", "prepare", prepare).Function("env", "target", export).Function("env", "inspect", inspect)
				if shared {
					memory, _ := stateImports.Lookup("env", "memory")
					imports.Memory("env", "memory", memory.(*Memory))
				}
				root, err = Instantiate(rv64PublicStartCompile(t, rv64TableEntryHostModule(shared), artifact), imports)
				if err != nil {
					t.Fatal(err)
				}
				defer root.Close()
				if shared && (root.jm != producer.jm || root.nativeContext == producer.nativeContext || root.nativeContext == identity) {
					t.Fatal("shared home is not activation identity")
				}
				_, err = root.Invoke("call")
				var trap *coreruntime.TrapError
				if !errors.As(err, &trap) || trap.Code != coreruntime.TrapUnreachable || len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != 3 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 18 {
					t.Fatal("cold scoped root provenance", err)
				}
				if root.pluginState.Load().railshotInvocation.tableDepth != 0 || replacement.jm != nil {
					t.Fatal("cold entry did not release final target storage")
				}
				if got, err := root.Invoke("safe"); err != nil || !slices.Equal(got, []uint64{9}) {
					t.Fatal("reuse after scoped cold exit", got, err)
				}
			})
		}
	}
}

func TestRV64TableEntryDepthBoundAndPartialFailureRelease(t *testing.T) {
	root, producer, table, _ := rv64TableTargetFixture(t, false, false, false)
	if !table.retainProducerInstance(producer) {
		t.Fatal("table root")
	}
	state := root.lockInvocation(0)
	defer state.unlockInvocation()
	var leases [rv64MaxTableEntryDepth]railshotTableEntryLease
	nativeExecutionMu.Lock()
	for i := range leases {
		lease, err := railshotAcquireNativeTableTargets(root)
		leases[i] = lease
		if err != nil {
			nativeExecutionMu.Unlock()
			t.Fatal("table entry depth", i, err)
		}
	}
	extra, err := railshotAcquireNativeTableTargets(root)
	nativeExecutionMu.Unlock()
	if err == nil || extra.targets != nil {
		t.Fatal("entry depth overflow acquired roots")
	}
	for i := len(leases) - 1; i >= 0; i-- {
		railshotReleaseNativeTableTargets(leases[i])
	}
	if state.railshotInvocation.tableDepth != 0 {
		t.Fatal("entry depth leaked")
	}
	// An entry rejected before guest execution must unlock and release its
	// partial graph, leaving both physical root counts and the slot reusable.
	table.mu.Lock()
	saved := slices.Clone(table.desc[8:40])
	table.desc[8+24] ^= 8
	table.mu.Unlock()
	root.lifeMu.Lock()
	before := root.resourceRefs
	root.lifeMu.Unlock()
	if lease, err := root.beginNativeEntry(); err == nil {
		root.unlockNativeEntry(lease)
		t.Fatal("invalid table entry admitted")
	}
	root.lifeMu.Lock()
	after := root.resourceRefs
	root.lifeMu.Unlock()
	copy(table.desc[8:40], saved)
	if before != after || state.railshotInvocation.tableDepth != 0 {
		t.Fatal("failed entry leaked roots", before, after)
	}
	if lease, err := root.beginNativeEntry(); err != nil {
		t.Fatal("entry reuse", err)
	} else {
		root.unlockNativeEntry(lease)
	}
}
