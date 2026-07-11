//go:build linux && amd64 && !tinygo

package wago

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/testutil/wasmtest"
)

type managedWrapperExtension struct {
	manager *InstanceManager
	calls   int
}

func (*managedWrapperExtension) Info() ExtensionInfo {
	return ExtensionInfo{ID: "test.managed-wrapper", RequiresCapabilities: []PluginCapability{PluginManagedInstances, PluginHostImports}}
}

func (e *managedWrapperExtension) Register(reg *Registry) error {
	manager, err := reg.ManagedInstances()
	if err != nil {
		return err
	}
	host, err := reg.HostImports()
	if err != nil {
		return err
	}
	e.manager = manager
	host.Module("env").Func("tick", func(_ HostModule, _ []uint64, results []uint64) {
		e.calls++
		results[0] = 1
	}).Results(ValI64)
	return nil
}

func TestManagedVoidTableInvokesSyncHostWrapperDescriptor(t *testing.T) {
	ext := &managedWrapperExtension{}
	rt := NewRuntime()
	defer rt.Close()
	if err := rt.Use(ext); err != nil {
		t.Fatalf("Use: %v", err)
	}

	imp := append(append(wasmtest.Name("env"), wasmtest.Name("tick")...), 0x00, 0x00)
	mod, err := rt.Compile(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
		wasmtest.Section(9, wasmtest.Vec(append([]byte{0x00, 0x41, 0x00, 0x0b, 0x01}, wasmtest.ULEB(1)...))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x1a, 0x0b}))),
	))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	owned, err := ext.manager.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer owned.Close()
	in := owned.Instance()
	if in == nil || !in.syncMode || !in.c.syncHostImports {
		t.Fatalf("managed instance sync mode = instance %p, mode %t, imports %t", in, in != nil && in.syncMode, in != nil && in.c.syncHostImports)
	}
	desc := in.tableDescriptor(0)
	entry := desc[8 : 8+coreruntime.TableEntryBytes]
	if got := binary.LittleEndian.Uint32(entry[coreruntime.TableEntrySigIDOffset:]); got != wasm.StructuralFuncTypeID(&wasm.CompType{Kind: wasm.CompFunc}) {
		t.Fatalf("table signature = %d, want () -> ()", got)
	}
	if home := binary.LittleEndian.Uint64(entry[coreruntime.TableEntryHomeLinMemOffset:]); home>>63 != 0 {
		t.Fatalf("sync-host table entry retained register-ABI tag: %#x", home)
	}
	if err := owned.InvokeVoidTable(context.Background(), 0); err != nil {
		t.Fatalf("InvokeVoidTable: %v", err)
	}
	if ext.calls != 1 {
		t.Fatalf("host calls = %d, want 1", ext.calls)
	}
}
