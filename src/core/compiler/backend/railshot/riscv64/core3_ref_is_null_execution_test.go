//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"crypto/sha256"
	"errors"
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
		})
	}
}
