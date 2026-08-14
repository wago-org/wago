//go:build amd64

package amd64

import (
	"bytes"
	"encoding/binary"
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
	var encodingSum uint64
	for _, fn := range stats.Funcs {
		encodingSum += fn.Encoding.MemoryDisp0 + fn.Encoding.MemoryDisp8 + fn.Encoding.MemoryDisp32
	}
	if got := stats.Encoding.MemoryDisp0 + stats.Encoding.MemoryDisp8 + stats.Encoding.MemoryDisp32; got == 0 || got != encodingSum {
		t.Fatalf("module encoding operands = %d, per-function sum = %d", got, encodingSum)
	}
	localOperands := stats.Encoding.LocalDisp0 + stats.Encoding.LocalDisp8 + stats.Encoding.LocalDisp32
	if localOperands == 0 {
		t.Fatal("module local-frame encoding ledger is empty")
	}
	if stats.Encoding.LocalDisp0 > stats.Encoding.FrameDisp0 || stats.Encoding.LocalDisp8 > stats.Encoding.FrameDisp8 || stats.Encoding.LocalDisp32 > stats.Encoding.FrameDisp32 {
		t.Fatalf("local-frame encoding is not a subset of frame encoding: local=%#v frame=%#v", stats.Encoding, stats.Encoding)
	}
	if report := stats.String(); !strings.Contains(report, "native: total=") || !strings.Contains(report, "dead-reserved=") || !strings.Contains(report, "amd64-encoding:") || !strings.Contains(report, "amd64-local-encoding:") {
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
	beforeStackDelta := stackDeltaAdapterThunkEnabled
	t.Cleanup(func() {
		sharedAdaptersEnabled = before
		stackDeltaAdapterThunkEnabled = beforeStackDelta
	})
	i32x2 := []wasm.ValType{wasm.I32, wasm.I32}
	m := modFuncs(t,
		funcDef{nil, i32x2, []byte{0x00, 0x41, 0x01, 0x41, 0x0b, 0x0b}},
		funcDef{nil, i32x2, []byte{0x00, 0x41, 0x02, 0x41, 0x0c, 0x0b}},
		funcDef{nil, i32x2, []byte{0x00, 0x41, 0x03, 0x41, 0x0d, 0x0b}},
		funcDef{nil, i32x2, []byte{0x00, 0x41, 0x04, 0x41, 0x0e, 0x0b}},
		funcDef{nil, i32x2, []byte{0x00, 0x41, 0x05, 0x41, 0x0f, 0x0b}},
		funcDef{nil, i32x2, []byte{0x00, 0x41, 0x06, 0x41, 0x10, 0x0b}},
	)
	m.Exports = append(m.Exports, wasm.Export{Name: "g", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 1}})
	m.Exports = append(m.Exports, wasm.Export{Name: "h", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 2}})
	m.Exports = append(m.Exports, wasm.Export{Name: "i", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 3}})
	m.Exports = append(m.Exports, wasm.Export{Name: "j", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 4}})
	m.Exports = append(m.Exports, wasm.Export{Name: "k", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 5}})
	size := OptimizeSize

	sharedAdaptersEnabled = true
	stackDeltaAdapterThunkEnabled = true
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
	thunk := shared.Entry[0]
	if shared.Code[thunk] != 0x68 || shared.Code[thunk+5] != 0xe9 {
		t.Fatalf("stack-delta thunk opcodes = %#x/%#x, want PUSH/JMP", shared.Code[thunk], shared.Code[thunk+5])
	}
	sharedAt := thunk + 10 + int(int32(binary.LittleEndian.Uint32(shared.Code[thunk+6:thunk+10])))
	resolvedInternal := sharedAt + 8 + int(int32(binary.LittleEndian.Uint32(shared.Code[thunk+1:thunk+5])))
	if resolvedInternal != shared.InternalEntry[0] {
		t.Fatalf("stack-delta internal entry = %d, want %d", resolvedInternal, shared.InternalEntry[0])
	}
	if want := []byte{0x5d, 0x48, 0x8d, 0x05, 0, 0, 0, 0, 0x48, 0x01, 0xc5}; !bytes.Equal(shared.Code[sharedAt:sharedAt+len(want)], want) {
		t.Fatalf("stack-delta prefix = % x, want % x", shared.Code[sharedAt:sharedAt+len(want)], want)
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

	stackDeltaAdapterThunkEnabled = false
	var legacyStats ModuleStats
	legacy, err := CompileModuleWith(m, CompileOptions{Objective: &size, Stats: &legacyStats})
	if err != nil {
		t.Fatal(err)
	}
	for i, fn := range legacyStats.Funcs {
		if got := fn.NativeSize.HostAdapterBytes; got != legacySharedAdapterThunkBytesAMD64 {
			t.Fatalf("legacy function %d adapter thunk = %d bytes, want %d", i, got, legacySharedAdapterThunkBytesAMD64)
		}
	}
	if len(legacy.Code) <= len(shared.Code) {
		t.Fatalf("legacy stack-delta rollback code = %d bytes, want more than %d", len(legacy.Code), len(shared.Code))
	}
	if got := runCompiledAmd64u(t, legacy); got != 1 {
		t.Fatalf("legacy shared adapter execution = %d, want 1", got)
	}
}
