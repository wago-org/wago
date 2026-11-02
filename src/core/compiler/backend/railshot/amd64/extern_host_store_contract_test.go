package amd64

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// These contracts run on every build host. Native helper/frame execution is
// covered by TestR9H2HostImport on AMD64 and actual ARM64 QEMU; parsing the
// other target's source here is explicitly not native target execution.
func TestExternHostStoreContractRouting(t *testing.T) {
	for _, ref := range []byte{0x6f, 0x70} {
		table := append(append(wasmtest.Name("h"), wasmtest.Name("t")...), 1, ref, 1, 1, 4)
		global := append(append(wasmtest.Name("h"), wasmtest.Name("g")...), 3, ref, 1)
		m, e := wasm.DecodeModule(wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(table, global))))
		if e != nil {
			t.Fatal(e)
		}
		for _, kind := range []wasm.InstrKind{wasm.InstrTableSet, wasm.InstrGlobalSet, wasm.InstrTableFill, wasm.InstrTableCopy, wasm.InstrTableGrow, wasm.InstrTableInit} {
			if got := shared.ExternStoreMayCall(m, kind, 0); got != (ref == 0x6f) {
				t.Fatalf("ref=%x kind=%v helper=%v", ref, kind, got)
			}
		}
		for _, kind := range []wasm.InstrKind{wasm.InstrTableGet, wasm.InstrGlobalGet, wasm.InstrTableSize} {
			if shared.ExternStoreMayCall(m, kind, 0) {
				t.Fatalf("read %v marked as store helper", kind)
			}
		}
	}
	if shared.ExternStoreMayCall(nil, wasm.InstrTableSet, 0) {
		t.Fatal("nil module")
	}
}

func TestExternHostStoreContractHelperABI(t *testing.T) {
	want := map[int][2]int{53: {3, 0}, 54: {2, 0}, 55: {4, 0}, 56: {5, 0}, 57: {3, 1}, 58: {5, 0}}
	seen := map[int]int{}
	for _, file := range []string{"table.go", "globals.go"} {
		tree, e := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if e != nil {
			t.Fatal(e)
		}
		ast.Inspect(tree, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			fn, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || fn.Sel.Name != "callGCStructHelper" || len(call.Args) != 3 {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok {
				return true
			}
			id, e := strconv.Atoi(lit.Value)
			if e != nil {
				return true
			}
			counts, ok := want[id]
			if !ok {
				return true
			}
			seen[id]++
			for i, expected := range counts {
				switch arg := call.Args[i+1].(type) {
				case *ast.CompositeLit:
					if len(arg.Elts) != expected {
						t.Errorf("helper %d operand vector %d arity=%d want=%d", id, i, len(arg.Elts), expected)
					}
				case *ast.Ident:
					if arg.Name != "nil" || expected != 0 {
						t.Errorf("helper %d missing operand vector %d", id, i)
					}
				default:
					t.Errorf("helper %d unknown operand vector %d", id, i)
				}
			}
			return true
		})
	}
	for id := range want {
		if seen[id] != 1 {
			t.Errorf("helper %d emit sites=%d want=1", id, seen[id])
		}
	}
}

func TestExternHostStoreContractCallFrames(t *testing.T) {
	// Both byte/AST hint planning and loop planning must consult the same
	// collector-independent classification; otherwise older providers can elide
	// helper call frames (including ARM64's link-register record).
	for _, file := range []string{"hints.go", "control.go"} {
		tree, e := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if e != nil {
			t.Fatal(e)
		}
		count := 0
		ast.Inspect(tree, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			fn, ok := call.Fun.(*ast.SelectorExpr)
			if ok && fn.Sel.Name == "ExternStoreMayCall" {
				count++
			}
			return true
		})
		minimum := 1
		if file == "hints.go" {
			minimum = 3
		}
		if count < minimum {
			t.Fatalf("%s omitted collector-free helper planning: %d sites", file, count)
		}
	}
}
