//go:build linux && riscv64 && !tinygo && !wago_guardpage

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Every target returns 42. A mutable exn cell can form an Env -> exception ->
// foreign Env edge; clearing it removes that edge without closing the module.
func rv64OwnedFunctionExceptionFixture(kind string) []byte {
	types := [][]byte{
		{0x60, 0, 1, 0x7f},                // 0: target
		{0x60, 1, 0x64, 0, 0},             // 1: tag/save(ref target)
		{0x60, 0, 0},                      // 2: void
		{0x60, 0, 1, 0x63, 0x69},          // 3: capture -> exn
		{0x60, 0, 1, 0x63, 0},             // 4: function -> nullable target
		{0x60, 1, 0x64, 0, 1, 0x63, 0x69}, // 5: capture(ref target) -> exn
	}
	read := []byte{0x02, 4, 0x1f, 0x40, 1, 0, 0, 0, 0x23, 0, 0x0a, 0x0b, 0, 0x0b, 0x14, 0, 0x0b}
	clearCell := []byte{0xd0, 0x69, 0x24, 0, 0x0b}
	capture := func(throw []byte) []byte {
		body := []byte{0x02, 0x69, 0x1f, 0x40, 1, 3, 0}
		body = append(body, throw...)
		return append(body, 0x0b, 0, 0x0b, 0x0b)
	}
	var functions, exports, bodies [][]byte
	var imports [][]byte
	if kind == "consumer" {
		throwImport := append(wasmtest.Name("env"), wasmtest.Name("throw")...)
		tagImport := append(wasmtest.Name("env"), wasmtest.Name("tag")...)
		imports = append(imports, append(throwImport, 0, 2), append(tagImport, 4, 0, 1))
		functions = [][]byte{{3}, {2}, {0}, {2}}
		bodies = [][]byte{capture([]byte{0x10, 0}), {0x10, 1, 0x24, 0, 0x0b}, read, clearCell}
		for i, name := range []string{"capture", "save", "run", "clear"} {
			exports = append(exports, wasmtest.ExportEntry(name, 0, uint32(i+1)))
		}
	} else if kind == "dynamic" {
		functions = [][]byte{{0}, {5}, {1}, {0}, {2}, {4}}
		bodies = [][]byte{{0x41, 42, 0x0b}, capture([]byte{0x20, 0, 0x08, 0}), {0x20, 0, 0x10, 1, 0x24, 0, 0x0b}, read, clearCell, {0xd2, 0, 0x0b}}
		for i, name := range []string{"target", "capture", "save", "run", "clear", "function"} {
			exports = append(exports, wasmtest.ExportEntry(name, 0, uint32(i)))
		}
	} else {
		functions = [][]byte{{0}, {2}, {3}, {2}, {0}, {2}, {4}}
		bodies = [][]byte{{0x41, 42, 0x0b}, {0xd2, 0, 0x08, 0, 0x0b}, capture([]byte{0x10, 1}), {0x10, 2, 0x24, 0, 0x0b}, read, clearCell, {0xd2, 0, 0x0b}}
		for i, name := range []string{"target", "throw", "capture", "save", "run", "clear", "function"} {
			exports = append(exports, wasmtest.ExportEntry(name, 0, uint32(i)))
		}
		if kind == "local" || kind == "operand" {
			// Extract a function and discard the owning exception before a new
			// capture collects. The function itself must retain its Env.
			body := []byte{0x02, 4, 0x1f, 0x40, 1, 0, 0, 0, 0x10, 2, 0x0a, 0x0b, 0, 0x0b}
			if kind == "local" {
				body = append(body, 0x21, 0, 0x10, 2, 0x1a, 0x20, 0)
			} else {
				body = append(body, 0x10, 2, 0x1a)
			}
			body = append(body, 0x14, 0, 0x0b)
			if kind == "local" {
				body = append([]byte{1, 1, 0x63, 0}, body...)
			} else {
				body = append([]byte{0}, body...)
			}
			functions = append(functions, []byte{0})
			bodies = append(bodies, body)
			exports = append(exports, wasmtest.ExportEntry("held", 0, 7))
		}
	}
	if kind == "import" {
		entry := append(wasmtest.Name("env"), wasmtest.Name("target")...)
		imports = append(imports, append(entry, 0, 0))
		functions, bodies = functions[1:], bodies[1:]
	}
	exports = append(exports, wasmtest.ExportEntry("saved", 3, 0), wasmtest.ExportEntry("tag", 4, 0))
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(types...))}
	if len(imports) != 0 {
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(imports...)))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec(functions...)))
	if kind != "consumer" {
		sections = append(sections, wasmtest.Section(13, wasmtest.Vec([]byte{0, 1})))
	}
	sections = append(sections, wasmtest.Section(6, wasmtest.Vec([]byte{0x69, 1, 0xd0, 0x69, 0x0b})), wasmtest.Section(7, wasmtest.Vec(exports...)))
	if kind != "consumer" {
		sections = append(sections, wasmtest.Section(9, wasmtest.Vec([]byte{3, 0, 1, 0})))
	}
	codes := make([][]byte, len(bodies))
	for i, body := range bodies {
		if i == 7 {
			codes[i] = append(wasmtest.ULEB(uint32(len(body))), body...)
		} else {
			codes[i] = wasmtest.Code(body)
		}
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(codes...)))
	return wasmtest.Module(sections...)
}

func TestRV64ExceptionFunctionOwnerFixtureTyping(t *testing.T) {
	for _, kind := range []string{"provider", "consumer", "dynamic", "local", "operand", "import"} {
		t.Run(kind, func(t *testing.T) {
			m, err := wasm.DecodeModule(rv64OwnedFunctionExceptionFixture(kind))
			if err != nil {
				t.Fatal(err)
			}
			if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{MultiMemory: true, ExtendedConstGlobals: true, GCConstExpr: true, CompactImports: true}, 1, wasm.ValidationLimits{}); err != nil {
				t.Fatal(err)
			}
			if kind == "operand" {
				plan, err := wasm.BuildCollectorOperandRoots(m, 7)
				if err != nil {
					t.Fatal(err)
				}
				r := wasm.ReaderFrom(m.Code[7].BodyBytes)
				calls, retained := 0, false
				for r.HasNext() {
					pc := m.Code[7].LocalDeclBytes + uint32(r.Offset())
					op, err := r.Byte()
					if err != nil {
						t.Fatal(err)
					}
					var immediate wasm.InstructionImmediate
					if err := wasm.ClassifyInstructionImmediateInto(&r, op, &immediate); err != nil {
						t.Fatal(err)
					}
					if immediate.Kind == wasm.InstrCall && immediate.Index == 2 {
						calls++
						if calls == 2 {
							roots, ok := plan.FunctionsAt(pc)
							collector, collectorOK := plan.At(pc)
							retained = ok && collectorOK && len(roots) == 1 && roots[0] == 0 && len(collector) == 0
						}
					}
				}
				if !retained {
					t.Fatal("extracted function is not an exact function root across the collecting capture")
				}
			}
		})
	}
}
