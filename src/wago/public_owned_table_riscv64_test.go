//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64RebasedTableBulkFeatureRequirements(t *testing.T) {
	// These bodies contain no ref.null/ref.func instructions: table init/drop/
	// copy themselves must retain #855's reference-types requirement.
	for _, tc := range []struct {
		name string
		body []byte
	}{
		{"init", []byte{0x41, 0, 0x41, 0, 0x41, 0, 0xfc, 12, 0, 0, 0x0b}},
		{"drop", []byte{0xfc, 13, 0, 0x0b}},
		{"copy", []byte{0x41, 0, 0x41, 0, 0x41, 0, 0xfc, 14, 0, 0, 0x0b}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
				wasmtest.Section(3, []byte{1, 0}), ownedTableDeclaration(1),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
				wasmtest.Section(9, []byte{1, 1, 0, 0}),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tc.body))),
			)
			cfg := ownedTableConfig().WithFeature(CoreFeatureReferenceTypes, false)
			if c, err := Compile(cfg, source); c != nil || err == nil || !strings.Contains(err.Error(), "reference-types") {
				if c != nil {
					_ = c.Close()
				}
				t.Fatal("disabled table reference feature was not rejected", err)
			}
			for _, artifact := range []bool{false, true} {
				c := rv64PublicStartCompile(t, source, artifact)
				want := CoreFeatureReferenceTypes | CoreFeatureBulkMemoryOperations
				if c.executionView().requiredFeatures&want != want {
					t.Fatal("source/artifact lost table feature requirements", artifact, c.requiredFeatures)
				}
				in, err := Instantiate(c)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := in.Invoke("run"); err != nil {
					_ = in.Close()
					t.Fatal("enabled table bulk operation", artifact, err)
				}
				_ = in.Close()
			}
		})
	}
}

func TestRV64OwnedTableArtifactBoundaries(t *testing.T) {
	c := ownedTableCompile(t, publicOwnedTableModule(7, false, 3, 0, 1, 2))
	base := c.executionView()
	for _, tc := range []struct {
		name   string
		mutate func(*Compiled)
	}{
		{"legacy-native-table-ABI", func(v *Compiled) { v.nativeTableContextABI = false; v.nativeTableReferenceABI = false }},
		{"legacy-native-reference-ABI", func(v *Compiled) { v.nativeTableReferenceABI = false }},
		{"legacy-export-fallback", func(v *Compiled) { v.hasTableExportMetadata = false }},
		{"table-export-index", func(v *Compiled) { v.tableExports = map[string]int{"escape": 1} }},
		{"negative-table-export-index", func(v *Compiled) { v.tableExports = map[string]int{"escape": -1} }},
		{"table-export-count", func(v *Compiled) {
			v.tableExports = make(map[string]int, 1025)
			for i := 0; i < 1025; i++ {
				v.tableExports["table"+strconv.Itoa(i)] = 0
			}
		}},
		{"register-ABI", func(v *Compiled) { v.registerABIDisabled = false }},
		{"table-import", func(v *Compiled) { v.tableImport = "env.table" }},
		{"second-table", func(v *Compiled) { v.extraTables = []tableDef{{Size: 1, Type: ValFuncRef}} }},
		{"table64", func(v *Compiled) { v.TableAddr64 = true }},
		{"nondefault-init", func(v *Compiled) { v.HasTableInitFunc = true; v.TableInitFunc = 1 }},
		{"unknown-type", func(v *Compiled) { v.TableType = 255 }},
		{"externref-type", func(v *Compiled) { v.TableType = ValExternRef }},
		{"nonnull-type", func(v *Compiled) { v.ValueTypes[v.TableValueTypeIndex].Ref.Nullable = false }},
		{"exact-type", func(v *Compiled) { v.ValueTypes[v.TableValueTypeIndex].Ref.Exact = true }},
		{"defined-type", func(v *Compiled) { v.ValueTypes[v.TableValueTypeIndex].Ref.Heap.Defined = true }},
		{"too-large-min", func(v *Compiled) { v.TableSize = 1025 }},
		{"negative-min", func(v *Compiled) { v.TableSize = -1 }},
		{"too-large-max", func(v *Compiled) { v.TableMax = 1025 }},
		{"overflowing-max", func(v *Compiled) { v.TableMax = ^uint64(0) }},
		{"declared-zero-max", func(v *Compiled) { v.TableHasMax = true; v.TableMax = 0 }},
		{"declared-short-max", func(v *Compiled) { v.TableHasMax = true; v.TableMax = 2 }},
		{"short-runtime-capacity", func(v *Compiled) { v.TableHasMax = false; v.TableMax = 2 }},
		{"missing-key", func(v *Compiled) { v.FuncTypeID = v.FuncTypeID[:1] }},
		{"zero-key", func(v *Compiled) { v.FuncTypeID[1] = 0 }},
		{"same-width-wrong-key", func(v *Compiled) { v.FuncTypeID[1] = v.FuncTypeID[2] }},
		{"too-many-segments", func(v *Compiled) { v.Elems = make([]ElemInit, 257) }},
		{"too-many-values", func(v *Compiled) { v.TableSize = 1024; v.TableMax = 1024; v.Elems[0].Values = make([]RefInit, 1025) }},
		{"passive-elements", func(v *Compiled) { v.passiveElems = []ElemInit{{Mode: ElemModePassive}} }},
		{"passive-mode", func(v *Compiled) { v.Elems[0].Mode = ElemModePassive }},
		{"declarative-mode", func(v *Compiled) { v.Elems[0].Mode = ElemModeDeclarative }},
		{"indexed-elements", func(v *Compiled) { v.Elems[0].TableIndex = 1 }},
		{"typed-elements", func(v *Compiled) { v.Elems[0].HasValueType = true }},
		{"global-offset", func(v *Compiled) { v.Elems[0].Offset.HasGlobal = true }},
		{"extended-offset", func(v *Compiled) { v.Elems[0].Offset.Expr = []byte{0x41, 0, 0x0b} }},
		{"null-element", func(v *Compiled) { v.Elems[0].Values[0].Null = true }},
		{"global-element", func(v *Compiled) { v.Elems[0].Values[0].HasGlobal = true }},
		{"i31-element", func(v *Compiled) { v.Elems[0].Values[0].I31Wrap = true }},
		{"repeat-element", func(v *Compiled) { v.Elems[0].Values[0].RepeatPrevious = true }},
		{"expression-element", func(v *Compiled) { v.Elems[0].Values[0].Expr = []byte{0xd2, 1, 0x0b} }},
		{"unavailable-element", func(v *Compiled) { v.Elems[0].Values[0].FuncIndex = 3 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := *base
			v.ValueTypes = slices.Clone(base.ValueTypes)
			v.FuncTypeID = slices.Clone(base.FuncTypeID)
			v.Elems = slices.Clone(base.Elems)
			for i := range v.Elems {
				v.Elems[i].Values = slices.Clone(base.Elems[i].Values)
			}
			tc.mutate(&v)
			if err := railshotValidateCompiled(&v); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("unsupported table metadata escaped RV64 gate", err)
			}
		})
	}
}

func TestRV64OwnedTableRejectsForeignContextAndWrongTags(t *testing.T) {
	providerCode := importedGlobalCompile(t, false, 1, false)
	provider, err := Instantiate(providerCode)
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	memory, err := provider.ExportedMemory("memory")
	if err != nil {
		t.Fatal(err)
	}
	im := NewImports().Memory("env", "memory", memory)
	c := ownedTableCompile(t, publicOwnedTableModule(7, true, 3, 0, 1, 2))
	a, err := Instantiate(c, im)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Instantiate(c, im)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if a.jm != b.jm || a.nativeContext == b.nativeContext {
		t.Fatal("foreign context fixture")
	}
	table := unsafe.Slice((*byte)(offHeapPtr(a.tableDescPtr)), a.tableDescLen)
	other := unsafe.Slice((*byte)(offHeapPtr(b.tableDescPtr)), b.tableDescLen)
	entry := table[8 : 8+coreruntime.TableEntryBytes]
	saved := slices.Clone(entry)
	for _, tc := range []struct {
		name   string
		mutate func()
	}{
		{"wrong-tag", func() {
			home := binary.LittleEndian.Uint64(entry[16:])
			binary.LittleEndian.PutUint64(entry[16:], home&^abi.FuncRefHomeTagMask)
		}},
		{"combined-tags", func() {
			home := binary.LittleEndian.Uint64(entry[16:])
			binary.LittleEndian.PutUint64(entry[16:], home|abi.FuncRefInternalHomeTag)
		}},
		{"null-canonical", func() { binary.LittleEndian.PutUint64(entry[24:], 0) }},
		{"foreign-canonical", func() {
			binary.LittleEndian.PutUint64(entry[24:], uint64(uintptr(unsafe.Pointer(&b.funcRefDescs[2*coreruntime.FuncRefDescBytes]))))
		}},
		{"foreign-entry", func() { copy(entry, other[8:8+coreruntime.TableEntryBytes]) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy(entry, saved)
			tc.mutate()
			binary.LittleEndian.PutUint64(a.results, 0x123456789abcdef0)
			_, err := a.Invoke("call", 0, 5)
			rv64RejectedTableEntry(t, err)
			// Fully consistent sibling descriptors are now genuine bridge targets
			// when physically attached. Public entry rejects their missing roots;
			// malformed row guards remain independent machine witnesses.
			if tc.name != "foreign-canonical" && tc.name != "foreign-entry" {
				ownedTableTrap(t, rv64RawOwnedTableGuard(t, a, 0, 5), coreruntime.TrapIndirectOutOfBounds)
			}
			if binary.LittleEndian.Uint64(a.results) != 0x123456789abcdef0 {
				t.Fatal("bad ownership reached callee result writes")
			}
			copy(entry, saved)
			importedGlobalInvoke(t, a, "call", []uint64{12}, 0, 5)
		})
	}
	// Wrong signature wins over wrong ownership and retains the caller's results.
	copy(entry, table[8+coreruntime.TableEntryBytes:8+2*coreruntime.TableEntryBytes])
	binary.LittleEndian.PutUint64(entry[16:], 0)
	_, err = a.Invoke("call", 0, 5)
	rv64RejectedTableEntry(t, err)
	ownedTableTrap(t, rv64RawOwnedTableGuard(t, a, 0, 5), coreruntime.TrapIndirectWrongSig)
	copy(entry, saved)
	importedGlobalInvoke(t, a, "call", []uint64{12}, 0, 5)
}

func rv64RejectedTableEntry(t *testing.T, err error) {
	t.Helper()
	var trap *coreruntime.TrapError
	if err == nil || errors.As(err, &trap) {
		t.Fatal("corrupt row was not rejected before guest entry", err)
	}
}

// Deliberately bypass table attachment admission only in the original corrupt
// row fixtures, which keep every candidate allocation physically alive. This
// retains independent machine guard/trap-order coverage after public entry
// starts authenticating table ownership before executing any instruction.
func rv64RawOwnedTableGuard(t *testing.T, in *Instance, args ...uint64) error {
	t.Helper()
	state := in.lockInvocation(0)
	defer state.unlockInvocation()
	nativeExecutionMu.Lock()
	defer nativeExecutionMu.Unlock()
	if err := in.bindAndValidateNativeContext(); err != nil {
		t.Fatal(err)
	}
	for index, arg := range args {
		binary.LittleEndian.PutUint64(in.serArgs[index*8:], arg)
	}
	if err := refreshNativeControl(true, in.eng, in.jm, in.trap); err != nil {
		t.Fatal(err)
	}
	entry := in.base + uintptr(in.c.Entry[in.c.Exports["call"]-in.c.NumImports])
	return in.decorateTrap(in.eng.CallPrepared(entry, in.serArgs, in.jm.LinMemBase(), in.trap, in.results))
}

func TestRV64OwnedTableActiveCancellationCloseAndArtifact(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, []byte{3, 0, 1, 2}), ownedTableDeclaration(1), wasmtest.Section(5, []byte{1, 0, 1}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("spin", 0, 0), wasmtest.ExportEntry("value", 0, 2))), ownedTableElements(0, 1),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x03, 0x40, 0x41, 0, 0x11, 1, 0, 0x0c, 0, 0x0b, 0x0b}), wasmtest.Code([]byte{0x41, 0, 0x41, 1, 0x36, 2, 0, 0x0b}), wasmtest.Code([]byte{0x41, 7, 0x0b}))),
	)
	for _, action := range []string{"cancel", "close"} {
		t.Run(action, func(t *testing.T) {
			c := ownedTableCompile(t, data)
			blob, err := c.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := LoadTrustedArtifact(blob)
			if err != nil {
				t.Fatal(err)
			}
			defer loaded.Close()
			in, err := Instantiate(loaded)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if err := loaded.Close(); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := in.InvokeContext(ctx, "spin"); done <- err }()
			flag := (*uint32)(unsafe.Pointer(&in.jm.CurrentBytes()[0]))
			deadline := time.Now().Add(5 * time.Second)
			for atomic.LoadUint32(flag) == 0 && time.Now().Before(deadline) {
				runtime.Gosched()
			}
			entered := atomic.LoadUint32(flag) != 0
			if action == "close" {
				err = in.Close()
			} else {
				cancel()
			}
			select {
			case callErr := <-done:
				if !entered || err != nil {
					t.Fatal("indirect callee did not enter/close", entered, err)
				}
				if action == "close" {
					ownedTableTrap(t, callErr, coreruntime.TrapInterrupted)
				} else if !errors.Is(callErr, context.Canceled) {
					t.Fatal("active indirect context cancellation", callErr)
				}
			case <-time.After(5 * time.Second):
				stop := coreruntime.RequestInterruptAsync(in.trap)
				<-done
				stop()
				t.Fatal("indirect loop did not stop")
			}
			if action == "cancel" {
				importedGlobalInvoke(t, in, "value", []uint64{7})
			} else if in.hasPhysicalResources() {
				t.Fatal("close retained private descriptors after foreign completion")
			}
		})
	}
}
