//go:build amd64

package amd64

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestNativeSizeReportAccountsModuleAndFunctionBytesAMD64(t *testing.T) {
	oldCompact := nativeCompactionEnabled
	nativeCompactionEnabled = false
	t.Cleanup(func() { nativeCompactionEnabled = oldCompact })
	i32 := []wasm.ValType{wasm.I32}
	m := modFuncs(t,
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x10, 0x01, 0x0b}},
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}},
	)

	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{Workers: 1, Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	native := stats.NativeSize
	if native.ExecutableMappingBytes < native.TotalBytes || native.ExecutableMappingPages == 0 {
		t.Fatalf("executable mapping = %d bytes / %d pages for %d raw bytes", native.ExecutableMappingBytes, native.ExecutableMappingPages, native.TotalBytes)
	}
	if native.TotalBytes != len(cm.Code) || native.AccountedBytes() != native.TotalBytes {
		t.Fatalf("module native report = %#v, code bytes = %d", native, len(cm.Code))
	}
	functionBytes := 0
	for i, fn := range stats.Funcs {
		s := fn.NativeSize
		if s.TotalBytes != fn.CodeBytes {
			t.Fatalf("function %d native total = %d, code stats = %d", i, s.TotalBytes, fn.CodeBytes)
		}
		if got := s.HostAdapterBytes + s.AdapterToInternalPaddingBytes + s.InternalFunctionBytes; got != s.TotalBytes {
			t.Fatalf("function %d attributed bytes = %d, total = %d", i, got, s.TotalBytes)
		}
		functionBytes += s.TotalBytes
	}
	if native.FunctionBytes != functionBytes {
		t.Fatalf("module function bytes = %d, sum = %d", native.FunctionBytes, functionBytes)
	}
	if stats.Funcs[0].NativeSize.HostAdapterBytes == 0 || stats.Funcs[1].NativeSize.HostAdapterBytes != 0 {
		t.Fatalf("adapter attribution = %#v / %#v", stats.Funcs[0].NativeSize, stats.Funcs[1].NativeSize)
	}
	if stats.Funcs[0].NativeSize.FrameAdjustmentBytes == 0 || stats.Funcs[0].NativeSize.DeadFrameReservationBytes == 0 {
		t.Fatalf("frame attribution = %#v", stats.Funcs[0].NativeSize)
	}
	if report := stats.String(); !strings.Contains(report, "native: total=") || !strings.Contains(report, "dead-reserved=") {
		t.Fatalf("explain output lacks native ledger:\n%s", report)
	}
}

func TestSizeObjectiveSharesAdapterTailsAMD64(t *testing.T) {
	before := sharedAdaptersEnabled
	sharedAdaptersEnabled = false
	t.Cleanup(func() { sharedAdaptersEnabled = before })
	i32x2 := []wasm.ValType{wasm.I32, wasm.I32}
	m := modFuncs(t,
		funcDef{nil, i32x2, []byte{0x00, 0x41, 0x01, 0x41, 0x0b, 0x0b}},
		funcDef{nil, i32x2, []byte{0x00, 0x41, 0x02, 0x41, 0x0c, 0x0b}},
		funcDef{nil, i32x2, []byte{0x00, 0x41, 0x03, 0x41, 0x0d, 0x0b}},
	)
	m.Exports = append(m.Exports,
		wasm.Export{Name: "g", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 1}},
		wasm.Export{Name: "h", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 2}},
	)
	size := OptimizeSize
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{Objective: &size, Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	if stats.NativeSize.ModuleOtherBytes == 0 {
		t.Fatal("identical adapter tails were not moved to a shared island")
	}
	if stats.NativeSize.AccountedBytes() != len(cm.Code) {
		t.Fatalf("accounted bytes = %d, code = %d", stats.NativeSize.AccountedBytes(), len(cm.Code))
	}
	parallel, err := CompileModuleWith(m, CompileOptions{Objective: &size, Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(parallel.Code, cm.Code) {
		t.Fatal("serial and parallel shared-tail layouts differ")
	}
	for i, fn := range stats.Funcs {
		if fn.NativeSize.HostAdapterTailBytes != sharedAdapterTailJumpBytesAMD64 {
			t.Fatalf("function %d adapter tail = %d, want jump size %d", i, fn.NativeSize.HostAdapterTailBytes, sharedAdapterTailJumpBytesAMD64)
		}
	}
}

func TestSizeObjectiveSharesWholeAdaptersAMD64(t *testing.T) {
	before := sharedAdaptersEnabled
	t.Cleanup(func() { sharedAdaptersEnabled = before })
	i32x2 := []wasm.ValType{wasm.I32, wasm.I32}
	m := modFuncs(t,
		funcDef{nil, i32x2, []byte{0x00, 0x41, 0x01, 0x41, 0x0b, 0x0b}},
		funcDef{nil, i32x2, []byte{0x00, 0x41, 0x02, 0x41, 0x0c, 0x0b}},
		funcDef{nil, i32x2, []byte{0x00, 0x41, 0x03, 0x41, 0x0d, 0x0b}},
	)
	m.Exports = append(m.Exports, wasm.Export{Name: "g", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 1}})
	m.Exports = append(m.Exports, wasm.Export{Name: "h", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 2}})
	size := OptimizeSize

	sharedAdaptersEnabled = true
	var stats ModuleStats
	shared, err := CompileModuleWith(m, CompileOptions{Objective: &size, Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	for i, fn := range stats.Funcs {
		if got := fn.NativeSize.HostAdapterBytes; got != sharedAdapterThunkBytesAMD64 {
			t.Fatalf("function %d adapter thunk = %d bytes, want %d", i, got, sharedAdapterThunkBytesAMD64)
		}
	}
	if stats.NativeSize.AccountedBytes() != len(shared.Code) || stats.NativeSize.ModuleOtherBytes == 0 {
		t.Fatalf("shared adapter accounting = %#v, code=%d", stats.NativeSize, len(shared.Code))
	}
	parallel, err := CompileModuleWith(m, CompileOptions{Objective: &size, Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(parallel.Code, shared.Code) {
		t.Fatal("serial and parallel shared-adapter layouts differ")
	}
	cm, err := CompileModuleWith(m, CompileOptions{Objective: &size})
	if err != nil {
		t.Fatal(err)
	}
	if got := runCompiledAmd64u(t, cm); got != 1 {
		t.Fatalf("shared adapter execution = %d, want 1", got)
	}
}
