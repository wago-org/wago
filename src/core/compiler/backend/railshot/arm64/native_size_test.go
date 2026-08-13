//go:build arm64

package arm64

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestNativeSizeReportAccountsModuleAndFunctionBytesArm64(t *testing.T) {
	beforeFinalizer := nativeFinalizerEnabled
	beforeCompact := nativeCompactionEnabled
	nativeFinalizerEnabled = true
	nativeCompactionEnabled = true
	t.Cleanup(func() {
		nativeFinalizerEnabled = beforeFinalizer
		nativeCompactionEnabled = beforeCompact
	})

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
	if native.TotalBytes != len(cm.Code) {
		t.Fatalf("native total = %d, code = %d", native.TotalBytes, len(cm.Code))
	}
	if len(stats.Funcs) != 2 {
		t.Fatalf("function reports = %d, want 2", len(stats.Funcs))
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
		if s.DeadReservationBytes() > s.TotalBytes {
			t.Fatalf("function %d dead reservations = %d, total = %d", i, s.DeadReservationBytes(), s.TotalBytes)
		}
		functionBytes += s.TotalBytes
	}
	if native.FunctionBytes != functionBytes {
		t.Fatalf("module function bytes = %d, sum = %d", native.FunctionBytes, functionBytes)
	}
	if native.FunctionAlignmentBytes != native.TotalBytes-native.FunctionBytes {
		t.Fatalf("module alignment = %d, want %d", native.FunctionAlignmentBytes, native.TotalBytes-native.FunctionBytes)
	}
	if native.AccountedBytes() != native.TotalBytes {
		t.Fatalf("module accounted = %d, total = %d", native.AccountedBytes(), native.TotalBytes)
	}

	if stats.Funcs[0].NativeSize.HostAdapterBytes == 0 {
		t.Fatal("exported caller has no attributed host adapter")
	}
	if got := stats.Funcs[1].NativeSize.HostAdapterBytes; got != 0 {
		t.Fatalf("direct-only callee host adapter = %d, want 0", got)
	}
	if got := stats.Funcs[1].NativeSize.AdapterToInternalPaddingBytes; got != 0 {
		t.Fatalf("direct-only callee internal padding = %d, want 0", got)
	}
	if stats.Funcs[0].NativeSize.FrameAdjustmentBytes == 0 || stats.Funcs[0].NativeSize.DeadFrameReservationBytes != 0 {
		t.Fatalf("caller frame attribution = %#v, want compact physical bytes and no retained dead bytes", stats.Funcs[0].NativeSize)
	}

	report := stats.String()
	if !strings.Contains(report, "native: total=") || !strings.Contains(report, "dead-reserved=") {
		t.Fatalf("explain output lacks native size summary:\n%s", report)
	}
}
