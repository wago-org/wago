//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"errors"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestProductionTailAddressFreeFinalizedPlans(t *testing.T) {
	for _, pair := range []bool{false, true} {
		types := []wasm.ValType{wasm.I32, wasm.F32}
		if pair {
			types = []wasm.ValType{wasm.I32, wasm.V128}
		}
		source := tailImportTestSource(types, types, []byte{0x20, 0, 0x20, 1, 0x12, 0, 0x0b})
		opts := CompileOptions{NativeTailCalls: true, InvocationContext: true, Interruptible: true, DeferCodeMapping: true, ImportBindings: []ImportBinding{{Dynamic: true, ImportIndex: 0}}}
		first, err := CompileModuleWith(localTailDecode(t, source), opts)
		if err != nil {
			t.Fatal(err)
		}
		second, err := CompileModuleWith(localTailDecode(t, source), opts)
		if err != nil {
			t.Fatal(err)
		}
		if first.CodeImage != nil || first.NativeTail == nil || len(first.NativeTail.Plans) != 1 || !bytes.Equal(first.Code, second.Code) || !slices.Equal(first.NativeTail.Plans, second.NativeTail.Plans) {
			t.Fatal("symbolic deterministic production tail image")
		}
		p := first.NativeTail.Plans[0]
		stride := 8
		if pair {
			stride = 16
		}
		if p.FunctionIndex != 1 || p.ImportIndex != 0 || p.SourcePC != 5 || p.FrameBytes < 48 || p.FrameBytes&15 != 0 || p.SourceStride != stride || p.SourceBase < 48 || p.SourceBase+stride+8 > p.FrameBytes {
			t.Fatal("finalized tail source frame", p)
		}
		pressured := false
		opts.InvocationContext = false
		opts.MemoryPressure = func() { pressured = true }
		if image, err := CompileModuleWith(localTailDecode(t, source), opts); image != nil || !errors.Is(err, ErrUnsupported) || pressured {
			t.Fatal("unqualified runtime reached publication", err)
		}
	}
}
