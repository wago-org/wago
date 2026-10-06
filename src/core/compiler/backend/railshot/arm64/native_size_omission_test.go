//go:build arm64

package arm64

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestNativeSizeRejectsOmittedAlignment(t *testing.T) {
	requireCompilerDiagnostics(t)
	old := nativeCompactionEnabled
	nativeCompactionEnabled = false
	defer func() { nativeCompactionEnabled = old }()
	params := []wasm.ValType{wasm.I32, wasm.I32}
	results := []wasm.ValType{wasm.I32}
	nonzero := 0
	for steps := 1; steps <= 4; steps++ {
		t.Run(fmt.Sprint(steps), func(t *testing.T) {
			body := []byte{0, 0x20, 0}
			for i := 0; i < steps; i++ {
				body = append(body, 0x20, 1, 0x6a)
			}
			body = append(body, 0x0b)
			single := mod1(t, params, results, body)
			cm1, err := CompileModuleWith(single, CompileOptions{Workers: 1})
			if err != nil {
				t.Fatal(err)
			}
			defer cm1.CodeImage.Close()
			// Compile the same exported function twice. Independent one-function
			// images establish its physical boundaries without reading size categories.
			m := modFuncs(t, funcDef{params, results, body}, funcDef{params, results, body})
			m.Exports = append(m.Exports, wasm.Export{Name: "g", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 1}})
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{Workers: 1, Stats: &stats})
			if err != nil {
				t.Fatal(err)
			}
			defer cm.CodeImage.Close()
			size := len(cm1.Code)
			if cm.Entry[0] != 0 || cm.Entry[1] < size || len(cm.Code) != cm.Entry[1]+size {
				t.Fatal("fixture contains extra fragments")
			}
			if !bytes.Equal(cm.Code[:size], cm1.Code) || !bytes.Equal(cm.Code[cm.Entry[1]:], cm1.Code) {
				t.Fatal("standalone and embedded function bytes differ")
			}
			gap := cm.Code[size:cm.Entry[1]]
			if !bytes.Equal(gap, make([]byte, len(gap))) {
				t.Fatal("inter-function span is not zero padding")
			}
			physical := len(cm.Code)
			// Freeze all expected sizes before corrupting a copy. Never run the
			// finalizer again: its residual alignment calculation would hide omission.
			reconcile := func(r shared.NativeSizeReport) error {
				if got := r.AccountedBytes(); got != physical {
					return fmt.Errorf("unattributed bytes: %d", physical-got)
				}
				if r.TotalBytes != physical || r.FunctionBytes != 2*size || r.FunctionAlignmentBytes != len(gap) || r.ModuleOtherBytes != 0 {
					return fmt.Errorf("wrong category boundary")
				}
				return nil
			}
			if err := reconcile(stats.NativeSize); err != nil {
				t.Fatal(err)
			}
			if len(gap) == 0 {
				return
			}
			nonzero++
			bad := stats.NativeSize
			bad.FunctionAlignmentBytes = 0
			want := fmt.Sprintf("unattributed bytes: %d", len(gap))
			if err := reconcile(bad); err == nil || err.Error() != want {
				t.Fatalf("omitted padding: %v, want %s", err, want)
			}
			// A total-only observer would miss this reassignment. The boundary check
			// must reject it even though its grand total still reconciles.
			bad.ModuleOtherBytes = len(gap)
			if err := reconcile(bad); err == nil || err.Error() != "wrong category boundary" {
				t.Fatalf("misattributed padding: %v", err)
			}
		})
	}
	if nonzero == 0 {
		t.Fatal("no fixture contains alignment padding")
	}
}
