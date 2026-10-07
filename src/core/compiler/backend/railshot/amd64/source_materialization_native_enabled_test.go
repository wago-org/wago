//go:build amd64 && wago_regalloccheck && !tinygo && !wago_profile

package amd64

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestNativeSourceMaterializationRealEmission(t *testing.T) {
	old := sharedScalarEnabled
	sharedScalarEnabled = false
	t.Cleanup(func() { sharedScalarEnabled = old })
	for _, workers := range []int{1, 2} {
		for _, typ := range []wasm.ValType{wasm.I32, wasm.I64} {
			for _, op := range []byte{0x6a, 0x71, 0x72, 0x73} {
				second := byte(0x73)
				if op == 0x73 {
					second = 0x6a
				}
				if typ == wasm.I64 {
					op += 0x12
					second += 0x12
				}
				body := []byte{0, 0x20, 0, 0x20, 1, op, 0x20, 2, second, 0x0b}
				m := mod1(t, []wasm.ValType{typ, typ, typ}, []wasm.ValType{typ}, body)
				reports := nativeSourceMaterializationCompile(t, m, workers)
				closed := op != 0x6a && op != 0x7c
				want := "materialization receipts recording closed (2)"
				if !closed {
					want = "materialization receipts incomplete"
				}
				if len(reports) != 2 || reports[1].Result.Verdict != regalloccheck.Inconclusive || !strings.Contains(reports[1].Result.Message, "source accounting complete;") || !strings.Contains(reports[1].Result.Message, want) {
					t.Fatalf("workers=%d type=%v op=%x reports=%+v", workers, typ, op, reports)
				}
			}
		}
	}
}
func nativeSourceMaterializationCompile(t *testing.T, m *wasm.Module, workers int) []codegen.SourceReport {
	t.Helper()
	m.FuncTypes = append(m.FuncTypes, m.FuncTypes[0])
	m.Code = append(m.Code, m.Code[0])
	var a wasm.ValidatedModuleAnalysis
	if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, &a); err != nil {
		t.Fatal(err)
	}
	cg := codegen.SourceOptions(codegen.Options{}, m, &a, wasm.ValidationFeatures{})
	var reports []codegen.SourceReport
	if !codegen.SetSourceReporter(cg, m, func(r codegen.SourceReport) { reports = append(reports, r) }) {
		t.Fatal("reporter")
	}
	cm, err := CompileModuleWith(m, CompileOptions{Codegen: cg, Workers: workers, DeferCodeMapping: workers != 1})
	if cm != nil && cm.CodeImage != nil {
		t.Cleanup(func() { cm.CodeImage.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	return reports
}
func TestNativeSourceMaterializationUnsupportedMachineFormSeparate(t *testing.T) {
	old := sharedScalarEnabled
	sharedScalarEnabled = false
	t.Cleanup(func() { sharedScalarEnabled = old })
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, []byte{0, 0x20, 0, 0x41, 3, 0x6a, 0x20, 1, 0x73, 0x0b})
	r := nativeSourceMaterializationCompile(t, m, 1)
	if len(r) != 2 || r[1].Result.Verdict != regalloccheck.Inconclusive || !strings.Contains(r[1].Result.Message, "source accounting complete;") || !strings.Contains(r[1].Result.Message, "materialization receipts incomplete") {
		t.Fatal(r)
	}
}
func TestNativeSourceMaterializationModelRRInstruction(t *testing.T) {
	for _, commute := range []bool{false, true} {
		f := nativeSourceModel(t, []byte{0x20, 0, 0x20, 1, 0x6a, 0x0b})
		left := nativeSourceModelGet(t, f, 0, 0)
		right := nativeSourceModelGet(t, f, 2, 1)
		checkNativeSourceBefore(f, 4, 0x6a)
		node := f.s.alloc()
		node.setElemKind(ekDeferred)
		node.setDeferredOp(opAdd)
		node.setValueType(mtI32)
		node.arg0, node.arg1 = left, right
		f.s.pushDeferred(node)
		checkNativeSourceAfter(f, 5)
		left.st.kind, left.st.reg = stLocalReg, RAX
		right.st.kind, right.st.reg = stLocalReg, RCX
		checkNativeSourceProducerBefore(f, node)
		if commute {
			checkNativeSourceALUBefore(f, node, right, left)
		} else {
			checkNativeSourceALUBefore(f, node, left, right)
		}
		start := f.a.Len()
		f.a.AluRR(0x01, RAX, RCX, false)
		checkNativeSourceALUAfter(f)
		end := f.a.Len()
		st := f.sourcePlan
		if r := shared.SourceMaterializationStatus(st.materialization); r.Reason != regalloccheck.NoFailure || r.Receipts != 1 {
			t.Fatal(r)
		}
		checkNativeSourceBefore(f, 5, 0x0b)
		f.s.erase(node)
		checkNativeSourceAfter(f, 6)
		checkSourceFinishEmission(f)
		receipt, ok := shared.SourceMaterializationReceiptAt(st.materialization, 0)
		if !ok || receipt.Start != start || receipt.End != end || receipt.Width != 4 || receipt.Kind != wasm.InstrI32Add || receipt.SourceEvent != 2 {
			t.Fatal(receipt, ok)
		}
		// No assertion of physical homes or Verified: final bytes have not been
		// checked against these roles and this model has no complete ABI envelope.
	}
}
func TestNativeSourceMaterializationSelectionControls(t *testing.T) {
	for _, mode := range []string{"wrong operator", "missing output tag", "stale input slot", "missing before hook", "missing after hook", "unadmitted RI"} {
		t.Run(mode, func(t *testing.T) {
			f := nativeSourceModel(t, []byte{0x20, 0, 0x20, 1, 0x6a, 0x0b})
			left := nativeSourceModelGet(t, f, 0, 0)
			right := nativeSourceModelGet(t, f, 2, 1)
			checkNativeSourceBefore(f, 4, 0x6a)
			node := f.s.alloc()
			node.setElemKind(ekDeferred)
			node.setDeferredOp(opAdd)
			node.setValueType(mtI32)
			node.arg0, node.arg1 = left, right
			f.s.pushDeferred(node)
			checkNativeSourceAfter(f, 5)
			left.st.kind, left.st.reg = stLocalReg, RAX
			right.st.kind, right.st.reg = stLocalReg, RCX
			st := f.sourcePlan
			want := regalloccheck.UnsupportedOperation
			switch mode {
			case "wrong operator":
				node.setDeferredOp(opXor)
			case "missing output tag":
				delete(st.associations, node)
			case "stale input slot":
				a := st.associations[left]
				n, _ := shared.SourceSlotNode(a.ref)
				shared.BindSourceSlot(st.owner.Token, a.slot, n)
				want = regalloccheck.InvalidGraph
			case "unadmitted RI":
				right.st.kind = stConst
				right.st.cval = 3
			}
			checkNativeSourceProducerBefore(f, node)
			if mode != "missing before hook" {
				checkNativeSourceALUBefore(f, node, left, right)
			}
			f.a.AluRR(0x01, RAX, RCX, false)
			if mode != "missing after hook" {
				checkNativeSourceALUAfter(f)
			}
			if mode == "missing after hook" {
				checkNativeSourceBefore(f, 5, 0x0b)
				f.s.erase(node)
				checkNativeSourceAfter(f, 6)
				checkSourceFinishEmission(f)
			}
			if r := shared.SourceMaterializationStatus(st.materialization); r.Reason != want || r.Readiness != shared.SourceMaterializationIncomplete {
				t.Fatal(mode, r, want)
			}
		})
	}
}
