//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestBulkScratchHintsByteAndASTArm64(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind wasm.InstrKind
		body []byte
		bulk bool
	}{
		{"memory.init", wasm.InstrMemoryInit, []byte{0xfc, 8, 0, 0}, true},
		{"data.drop", wasm.InstrDataDrop, []byte{0xfc, 9, 0}, true},
		{"memory.copy", wasm.InstrMemoryCopy, []byte{0xfc, 10, 0, 0}, true},
		{"memory.fill", wasm.InstrMemoryFill, []byte{0xfc, 11, 0}, true},
		{"table.init", wasm.InstrTableInit, []byte{0xfc, 12, 0, 0}, true},
		{"elem.drop", wasm.InstrElemDrop, []byte{0xfc, 13, 0}, true},
		{"table.copy", wasm.InstrTableCopy, []byte{0xfc, 14, 0, 0}, true},
		{"table.fill", wasm.InstrTableFill, []byte{0xfc, 17, 0}, true},
		{"table.grow", wasm.InstrTableGrow, []byte{0xfc, 15, 0}, false},
		{"table.size", wasm.InstrTableSize, []byte{0xfc, 16, 0}, false},
		{"table.get", wasm.InstrTableGet, []byte{0x25, 0}, false},
		{"table.set", wasm.InstrTableSet, []byte{0x26, 0}, false},
		{"memory.grow", wasm.InstrMemoryGrow, []byte{0x40, 0}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bytes, err := scanBodyBytes(append(tc.body, 0x0b), 0, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast := scanBody(wasm.Expr{Instrs: []wasm.Instruction{{Kind: tc.kind}}}, 0, 0, 0)
			for _, h := range []funcHintView{bytes, ast} {
				if got := h.flags.has(hintUsesBulkMem); got != tc.bulk {
					t.Errorf("bulk scratch hint = %t, want %t (flags %#x)", got, tc.bulk, h.flags)
				}
			}
		})
	}
}

func scratchPinTestModule(t *testing.T, operation []byte, inline bool) *wasm.Module {
	body := []byte{1, 16, 0x7f}
	for i := byte(4); i < 20; i++ {
		body = append(body, 0x20, 3, 0x41, i, 0x6a, 0x21, i)
	}
	if inline {
		body = append(body, 0x20, 0, 0x20, 1, 0x20, 2, 0x10, 1)
	} else {
		body = append(body, operation...)
	}
	body = append(body, 0x41, 0)
	for i := byte(4); i < 20; i++ {
		body = append(body, 0x20, i, 0x6a)
	}
	body = append(body, 0x0b)
	fns := []funcDef{{[]wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body}}
	if inline {
		helper := append([]byte{0}, operation...)
		helper = append(helper, 0x0b)
		fns = append(fns, funcDef{[]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil, helper})
	}
	m := modFuncs(t, fns...)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	return m
}

// Inspect the actual assignments after compilation, without asserting an exact
// instruction sequence. The scalar control proves that pressure reaches all
// three extra leaf pins; the bulk and loop cases prove their exclusion.
func TestLeafScratchPinAssignmentsArm64(t *testing.T) {
	for _, tc := range []struct {
		name            string
		op              []byte
		inline          bool
		want, forbidden regMask
	}{
		{"scalar", nil, false, maskOf(X12, X13, X14), 0},
		{"data.drop", []byte{0xfc, 9, 0}, false, 0, maskOf(X9, X10, X11, X12, X13, X14)},
		{"elem.drop", []byte{0xfc, 13, 0}, false, 0, maskOf(X9, X10, X11, X12, X13, X14)},
		{"loop", []byte{0x03, 0x40, 0x0b}, false, maskOf(X14), maskOf(X12, X13)},
		{"nested-loop", []byte{0x03, 0x40, 0x03, 0x40, 0x0b, 0x0b}, false, maskOf(X14), maskOf(X12, X13)},
		{"fill", []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 11, 0}, false, 0, maskOf(X9, X10, X11, X12, X13, X14)},
		{"inline-fill", []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 11, 0}, true, 0, maskOf(X9, X10, X11, X12, X13, X14)},
		{"loop-fill", []byte{0x03, 0x40, 0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 11, 0, 0x0b}, false, 0, maskOf(X9, X10, X11, X12, X13, X14)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			op := tc.op
			if tc.name == "data.drop" {
				op = nil // The byte decoder requires a data-count section for data.drop.
			}
			m := scratchPinTestModule(t, op, tc.inline)
			if tc.name == "data.drop" {
				m.Code[0].BodyBytes = append(tc.op, m.Code[0].BodyBytes...)
			}
			if tc.name == "data.drop" || tc.name == "elem.drop" {
				m.Memories = nil // Passive segments do not require a memory/table.
			}
			policy := currentCodegenPolicy()
			hints, sidecar, _, err := computeModuleHintsWithWorkersPolicy(m, 0, 0, 1, policy)
			if err != nil {
				t.Fatal(err)
			}
			targets := buildInlineTargets(m, hints, policy)
			sc := newScratch()
			sc.classifier = wasm.NewModuleInstructionClassifier(m, true)
			h := sidecar.viewAt(hints[0], 0)
			var stats CodegenStats
			_, _, _, err = compileFunc(m, nil, 0, true, false, true, false, nil, &h,
				immutableTableHint{}, nil, false, 0, false, false, false, nil, nil,
				&stats, targets, hints, policy, sc)
			if err != nil {
				t.Fatal(err)
			}
			got := sc.fnState.pinnedLocalMask
			if got&tc.want != tc.want || got&tc.forbidden != 0 {
				t.Fatalf("actual local pins %#x, required %#x, forbidden %#x", got, tc.want, tc.forbidden)
			}
			if tc.inline && stats.Calls[callKindInline] != 1 {
				t.Fatalf("helper was not inlined: %v", stats.Calls)
			}
		})
	}
}

func TestBulkScratchHintsCore3Arm64(t *testing.T) {
	m := &wasm.Module{
		Memories: []wasm.MemType{{}, {Limits: wasm.Limits{Addr64: true}}},
		Tables: []wasm.Table{{Type: wasm.TableType{Ref: wasm.AbsRef(wasm.HeapFunc)}},
			{Type: wasm.TableType{Ref: wasm.AbsRef(wasm.HeapFunc), Limits: wasm.Limits{Addr64: true}}}},
	}
	for _, op := range [][]byte{
		{0xfc, 8, 0, 1}, {0xfc, 10, 1, 0}, {0xfc, 10, 0, 1}, {0xfc, 11, 1},
		{0xfc, 12, 0, 1}, {0xfc, 14, 1, 0}, {0xfc, 14, 0, 1}, {0xfc, 17, 1},
	} {
		h, err := scanFuncBody(wasm.Func{BodyBytes: append(op, 0x0b)}, 0, 0, 0, nil, m)
		if err != nil || !h.flags.has(hintUsesBulkMem) {
			t.Fatalf("Core 3 operation %x: bulk hint missing: flags %#x, err %v", op, h.flags, err)
		}
	}
}

func TestBulkHelpersCannotPromiseCallerPinPreservationArm64(t *testing.T) {
	ft := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}}
	if !preservesCallerPins(ft, 1, funcHints{}) {
		t.Fatal("scalar leaf lost its caller-pin preservation optimization")
	}
	if preservesCallerPins(ft, 1, funcHints{flags: hintUsesBulkMem}) {
		t.Fatal("bulk helper can overwrite caller pins even without locals or linear memory")
	}
}

func TestTableSetHelperCallHintsArm64(t *testing.T) {
	for _, tc := range []struct {
		ref  wasm.ValType
		call bool
	}{{wasm.FuncRef, false}, {wasm.ExternRef, false}, {wasm.AnyRef, true}} {
		m := &wasm.Module{Tables: []wasm.Table{{Type: wasm.TableType{Ref: tc.ref.Ref()}}}}
		h, err := scanFuncBody(wasm.Func{BodyBytes: []byte{0x03, 0x40, 0x26, 0, 0x0b, 0x0b}}, 0, 0, 0, nil, m)
		if err != nil {
			t.Fatal(err)
		}
		for _, flag := range []funcHintFlags{hintHasCall, hintHasNonDirectCall, hintHasLoopCall} {
			if got := h.flags.has(flag); got != tc.call {
				t.Errorf("%v flag %#x = %t, want %t", tc.ref, flag, got, tc.call)
			}
		}
	}
}

func TestInlineDirectCallKeepsRuntimeHelperCallClassificationArm64(t *testing.T) {
	m := scratchPinTestModule(t, nil, true)
	m.Tables = []wasm.Table{{Type: wasm.TableType{Ref: wasm.AbsRef(wasm.HeapAny), Limits: wasm.Limits{Min: 1}}}}
	// A runtime barrier call alongside an ordinary call that will be inlined.
	m.Code[0].BodyBytes = append([]byte{0x41, 0, 0xd0, 0x6e, 0x26, 0}, m.Code[0].BodyBytes...)
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, GCStructHelpers: true})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	s := stats.Funcs[0]
	if s.Calls[callKindInline] != 1 || s.Calls[callKindHostSync] != 1 {
		t.Fatalf("expected an inlined Wasm call and a remaining helper: %v", s.Calls)
	}
	if s.Peephole["all-calls-inlined"] != 0 {
		t.Fatal("inlining erased the runtime helper's call classification")
	}
}
