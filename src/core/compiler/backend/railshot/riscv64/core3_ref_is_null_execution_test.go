//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func core3RefIsNullExecutionSource() []byte {
	return []byte(core3RefIsNullExecutionWire)
}

func core3RefIsNullExecutionDecode(t *testing.T) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModule(core3RefIsNullExecutionSource())
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCore3RefIsNullExecutionPinnedContract(t *testing.T) {
	source := core3RefIsNullExecutionSource()
	if len(source) != 272 {
		t.Fatal("pinned source length", len(source))
	}
	wantSHA := [32]byte{0x52, 0x07, 0x18, 0x1e, 0x7b, 0x37, 0xc2, 0xb1, 0x21, 0xa6, 0x17, 0xf3, 0xde, 0x14, 0x75, 0x81, 0x73, 0x82, 0xf5, 0x7c, 0xc1, 0x46, 0xed, 0x06, 0xc1, 0x06, 0x01, 0x0f, 0xa5, 0x28, 0x7f, 0xcf}
	if got := sha256.Sum256(source); got != wantSHA || !isCore3RefIsNullExecutionWire(source) {
		t.Fatalf("pinned source identity: %x", got)
	}
	m := core3RefIsNullExecutionDecode(t)
	if !isCore3RefIsNullExecution(m) {
		t.Fatal("pinned decoded contract rejected")
	}
	// Recognition alone grants no executable authority. This remains the exact
	// public gate until the following runtime/table checkpoints are complete.
	if err := AdmitProductionSource(source); !errors.Is(err, ErrUnsupported) {
		t.Fatal("unqualified executable source admitted", err)
	}
}

func TestCore3RefIsNullExecutionWireConfinement(t *testing.T) {
	base := core3RefIsNullExecutionSource()
	for i := range base {
		mutated := bytes.Clone(base)
		mutated[i] ^= 1
		if isCore3RefIsNullExecutionWire(mutated) {
			t.Fatal("one-bit source mutation admitted", i)
		}
	}
	if isCore3RefIsNullExecutionWire(core3RefIsNullDeclarationsSource()) {
		t.Fatal("declaration sibling matched executable source")
	}
}

func TestCore3RefIsNullExecutionDecodedConfinement(t *testing.T) {
	mutations := map[string]func(*wasm.Module){
		"type": func(m *wasm.Module) {
			m.Types[2].SubTypes[0].Comp.Params[0] = wasm.FuncRef
		},
		"binding": func(m *wasm.Module) { m.FuncTypes[9].Index = 5 },
		"body":    func(m *wasm.Module) { m.Code[9].BodyBytes[3] = 1 },
		"local": func(m *wasm.Module) {
			m.Code[0].Locals.Runs = []wasm.LocalRun{{Count: 1, Type: wasm.I32}}
		},
		"table-type":    func(m *wasm.Module) { m.Tables[1].Type.Ref = wasm.FuncRef.Ref() },
		"table-size":    func(m *wasm.Module) { m.Tables[2].Type.Limits.Min = 3 },
		"element-table": func(m *wasm.Module) { m.Elements[1].Mode.Table = 1 },
		"element-target": func(m *wasm.Module) {
			m.Elements[1].Kind.Exprs[0].BodyBytes[1] = 1
		},
		"export":      func(m *wasm.Module) { m.Exports[0].Index.Index = 0 },
		"import":      func(m *wasm.Module) { m.Imports = append(m.Imports, wasm.Import{}) },
		"start":       func(m *wasm.Module) { index := wasm.FuncIdx(0); m.Start = &index },
		"custom":      func(m *wasm.Module) { m.Customs = append(m.Customs, wasm.CustomSec{Name: "x"}) },
		"raw-name":    func(m *wasm.Module) { m.RawNameSecPayload = []byte{0} },
		"branch-hint": func(m *wasm.Module) { m.BranchHints = []wasm.FuncBranchHints{{FuncIndex: 0}} },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			m := core3RefIsNullExecutionDecode(t)
			mutate(m)
			if isCore3RefIsNullExecution(m) {
				t.Fatal("hostile decoded shape admitted")
			}
			if image, err := CompileModuleWith(m, closedReferenceProductionOptions(false, true)); image != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("hostile decoded shape reached backend checkpoint", err)
			}
		})
	}
}

func TestCore3RefIsNullExecutionBackendCheckpoint(t *testing.T) {
	m := core3RefIsNullExecutionDecode(t)
	opts := closedReferenceProductionOptions(false, true)
	image, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	if image == nil || !image.NativeReferenceFunctions || image.NativeReferenceCalls || !image.NativeReferenceControl || image.NativeTail != nil || image.CodeImage != nil || len(image.Entry) != 10 {
		if image == nil {
			t.Fatal("exact backend checkpoint produced nil image")
		}
		t.Fatalf("exact backend checkpoint witness: functions=%t calls=%t control=%t entries=%d", image.NativeReferenceFunctions, image.NativeReferenceCalls, image.NativeReferenceControl, len(image.Entry))
	}
	wantEntries := []int{0, 576, 1200, 1824, 2448, 3120, 4000, 6128, 6960, 7920}
	wantCodeSHA := [32]byte{0xb7, 0x0c, 0x68, 0x57, 0x59, 0xc1, 0xfe, 0x72, 0x23, 0xd8, 0x07, 0x16, 0x7d, 0x4b, 0xe8, 0xc8, 0x33, 0xf8, 0x8d, 0x2b, 0x1a, 0x5a, 0xb8, 0x29, 0xfc, 0xbf, 0xff, 0x35, 0x08, 0x09, 0x58, 0x01}
	if len(image.Code) != 8872 || sha256.Sum256(image.Code) != wantCodeSHA || !slices.Equal(image.Entry, wantEntries) || !slices.Equal(image.InternalEntry, wantEntries) {
		t.Fatal("exact backend code image changed")
	}
	// This is deliberately a compiler-only checkpoint. The source boundary must
	// still reject until retained metadata and runtime lifetime proofs exist.
	if err := AdmitProductionSource(core3RefIsNullExecutionSource()); !errors.Is(err, ErrUnsupported) {
		t.Fatal("backend checkpoint escaped public source gate", err)
	}
}
