package wago

import (
	"context"
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestPublicFuncrefIngressRejectsForgedNonNullBeforeNativeExecution(t *testing.T) {
	tests := []struct {
		name string
		call func(*Instance, uint64) ([]uint64, error)
	}{
		{
			name: "Invoke",
			call: func(in *Instance, forged uint64) ([]uint64, error) {
				return in.Invoke("sink", forged)
			},
		},
		{
			name: "Call",
			call: func(in *Instance, forged uint64) ([]uint64, error) {
				out, err := in.Call(context.Background(), "sink", ValueOf(ValFuncRef, forged))
				if out != nil {
					return []uint64{1}, err
				}
				return nil, err
			},
		},
		{
			name: "invokeLocal",
			call: func(in *Instance, forged uint64) ([]uint64, error) {
				return in.invokeLocal(0, []uint64{forged})
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := instantiateFuncrefBoundaryTestModule(t, funcrefIngressBoundaryModule())
			defer in.Close()
			if len(in.funcRefDescs) < 2*coreruntime.TableEntryBytes {
				t.Fatalf("funcref descriptor arena = %d bytes, want at least two entries", len(in.funcRefDescs))
			}
			forged := uint64(uintptr(unsafe.Pointer(&in.funcRefDescs[coreruntime.TableEntryBytes])))

			out, err := tc.call(in, forged)
			if err == nil || !strings.Contains(err.Error(), "non-null funcref argument") {
				t.Fatalf("forged funcref call = %v, %v; want public-boundary rejection", out, err)
			}
			if out != nil {
				t.Fatalf("forged funcref call returned %v, want nil", out)
			}
			if marker, markerErr := in.Global("marker"); markerErr != nil || AsI32(marker) != 0 {
				t.Fatalf("marker after rejected call = %v, %v; want 0 (native body not entered)", marker, markerErr)
			}
		})
	}
}

func TestPublicFuncrefEgressRejectsDescriptorWithoutExposingBits(t *testing.T) {
	tests := []struct {
		name string
		call func(*Instance) ([]uint64, error)
	}{
		{
			name: "Invoke",
			call: func(in *Instance) ([]uint64, error) {
				return in.Invoke("get", nil...)
			},
		},
		{
			name: "Call",
			call: func(in *Instance) ([]uint64, error) {
				out, err := in.Call(context.Background(), "get")
				if out != nil {
					return []uint64{1}, err
				}
				return nil, err
			},
		},
		{
			name: "invokeLocal",
			call: func(in *Instance) ([]uint64, error) {
				return in.invokeLocal(1, nil)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := instantiateFuncrefBoundaryTestModule(t, funcrefEgressBoundaryModule())
			defer in.Close()

			out, err := tc.call(in)
			if err == nil || !strings.Contains(err.Error(), "non-null funcref result") {
				t.Fatalf("non-null funcref result error = %v; want public-boundary rejection", err)
			}
			if out != nil {
				t.Fatal("non-null funcref result exposed raw slots")
			}
		})
	}
}

func TestPublicFuncrefBoundaryContinuesToAcceptNull(t *testing.T) {
	in := instantiateFuncrefBoundaryTestModule(t, funcrefIngressBoundaryModule())
	defer in.Close()

	if out, err := in.Invoke("sink", 0); err != nil || len(out) != 0 {
		t.Fatalf("Invoke sink(null) = %v, %v; want success", out, err)
	}
	if marker, err := in.Global("marker"); err != nil || AsI32(marker) != 1 {
		t.Fatalf("marker after sink(null) = %v, %v; want 1", marker, err)
	}
}

func instantiateFuncrefBoundaryTestModule(t *testing.T, mod []byte) *Instance {
	t.Helper()
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	in, err := Instantiate(c)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	return in
}

func funcrefIngressBoundaryModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x01, 0x01})), // funcref table min=1 max=1
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x00, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("sink", 0, 0),
			wasmtest.ExportEntry("marker", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x41, 0x01, // i32.const 1
			0x24, 0x00, // global.set 0
			0x41, 0x00, // i32.const 0
			0x20, 0x00, // local.get 0
			0x26, 0x00, // table.set 0
			0x0b,
		}))),
	)
}

func funcrefEgressBoundaryModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x00, 0x00})), // funcref table min=0 max=0
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec(append([]byte{0x03, 0x00}, wasmtest.Vec(wasmtest.ULEB(0))...))), // declarative func 0
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x0b}),
			wasmtest.Code([]byte{0xd2, 0x00, 0x0b}), // ref.func 0
		)),
	)
}
