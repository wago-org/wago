//go:build amd64

package amd64

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestNativeSizeReportAccountsModuleAndFunctionBytesAMD64(t *testing.T) {
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
