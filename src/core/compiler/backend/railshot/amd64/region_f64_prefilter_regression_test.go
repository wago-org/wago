//go:build linux && amd64 && wago_experimental_bounded_vector_polls

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"reflect"
	"testing"
	"unsafe"
)

func TestRegionF64ProofNestedRegression(t *testing.T) {
	store := []byte{0x41, 0, 0x44, 0, 0, 0, 0, 0, 0, 0, 0, 0x39, 3, 0}
	for _, tc := range []struct {
		name           string
		prefix, suffix []byte
	}{
		{"block", []byte{2, 0x40}, []byte{0x0b}},
		{"loop", []byte{3, 0x40}, []byte{0x0b}},
		{"if-then", []byte{0x41, 1, 4, 0x40}, []byte{5, 1, 0x0b}},
		{"if-else", []byte{0x41, 1, 4, 0x40, 1, 5}, []byte{0x0b}},
		{"try-table", []byte{0x1f, 0x40, 0}, []byte{0x0b}},
		{"block-if-try", []byte{2, 0x40, 0x41, 1, 4, 0x40, 0x1f, 0x40, 0}, []byte{0x0b, 0x0b, 0x0b}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, hasStore := range []bool{false, true} {
				body := append([]byte(nil), tc.prefix...)
				if hasStore {
					body = append(body, store...)
				} else {
					body = append(body, 1)
				}
				body = append(body, tc.suffix...)
				body = append(body, 0x0b)
				h, err := scanBodyBytes(body, 0, 0, 0)
				if err != nil {
					t.Fatal(err)
				}
				leaf := wasm.Instruction{Kind: wasm.InstrNop}
				if hasStore {
					leaf.Kind = wasm.InstrF64Store
				}
				expr := wasm.Expr{Instrs: []wasm.Instruction{leaf}}
				switch tc.name {
				case "block":
					expr = regionProofASTWrap(wasm.InstrBlock, expr, false)
				case "loop":
					expr = regionProofASTWrap(wasm.InstrLoop, expr, false)
				case "if-then":
					expr = regionProofASTWrap(wasm.InstrIf, expr, false)
				case "if-else":
					expr = regionProofASTWrap(wasm.InstrIf, expr, true)
				case "try-table":
					expr = regionProofASTWrap(wasm.InstrTryTable, expr, false)
				case "block-if-try":
					expr = regionProofASTWrap(wasm.InstrTryTable, expr, false)
					expr = regionProofASTWrap(wasm.InstrIf, expr, false)
					expr = regionProofASTWrap(wasm.InstrBlock, expr, false)
				}
				if len(expr.Instrs) == 0 {
					t.Fatal("AST fixture is empty")
				}
				ast := scanBody(expr, 0, 0, 0)
				if h.provenNoF64Store() != !hasStore || ast.provenNoF64Store() != !hasStore {
					t.Fatalf("store=%v byte=%v AST=%v", hasStore, h.provenNoF64Store(), ast.provenNoF64Store())
				}
			}
		})
	}
}

func TestRegionF64ProofReusedMalformedRegression(t *testing.T) {
	for _, body := range [][]byte{{0x39}, {2, 0x40}, {0x0b, 1}, {0xff, 0x0b}, {0x0b, 0x39, 3, 0}} {
		h := newFuncHints(0, 0)
		h.setNoF64StoreProof(true)
		h.noteFixedScratchLease()
		elig := newGlobalEligibilityTracker(0)
		var accum shared.GlobalHintAccumulator
		accum.Reset(0)
		got, err := scanBodyBytesIntoMemory64WithModuleCalls(body, 0, 0, 0, h, &elig, false, nil, nil, nil, false, nil, nil, 0, &accum)
		if err == nil || got.provenNoF64Store() {
			t.Fatalf("body=%x err=%v retained proof=%v", body, err, got.provenNoF64Store())
		}
		if !got.hasFixedScratchLease() {
			t.Fatal("clearing proof damaged independent flag")
		}
	}
}

func TestRegionF64ProofActualInlineStoreRegression(t *testing.T) {
	requireCompilerDiagnostics(t) // inline must be observed, never silently assumed
	caller := []byte{0x10, 1, 0x0b}
	callee := []byte{0x41, 0, 0x44, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f, 0x39, 3, 0, 0x0b}
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{0})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(caller), wasmtest.Code(callee))),
	))
	if err != nil {
		t.Fatal(err)
	}
	hints, _, _, err := computeModuleHints(m, 0, 0, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !hints[0].provenNoF64Store() || hints[1].provenNoF64Store() {
		t.Fatal("fixture must have a store-free caller and storing callee")
	}
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{Interruptible: true, Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if stats.Funcs[0].Calls["inline"] != 1 || stats.Funcs[0].Peephole["all-calls-inlined"] == 0 {
		t.Fatalf("callee did not inline: calls=%v decisions=%v", stats.Funcs[0].Calls, stats.Funcs[0].Peephole)
	}
}

// Instruction control payloads have no public constructors. This fixture-only
// reflection helper initializes the private payload by its actual reflected type,
// without assuming struct offsets or introducing a production testing API.
func regionProofASTWrap(kind wasm.InstrKind, body wasm.Expr, alternate bool) wasm.Expr {
	in := wasm.Instruction{Kind: kind}
	field := reflect.ValueOf(&in).Elem().FieldByName("ext")
	payload := reflect.New(field.Type().Elem())
	if kind == wasm.InstrIf {
		name := "Then"
		if alternate {
			name = "Else"
		}
		payload.Elem().FieldByName(name).Set(reflect.ValueOf(body.Instrs))
	} else {
		payload.Elem().FieldByName("Body").Set(reflect.ValueOf(body))
	}
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(payload)
	return wasm.Expr{Instrs: []wasm.Instruction{in}}
}
