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

// Compile valid originals through the actual serial/parallel fallback drivers.
// Neither these fixtures nor the mutated accounting models execute guest code.
func TestNativeSourcePlanRealCompiler(t *testing.T) {
	old := sharedScalarEnabled
	sharedScalarEnabled = false
	t.Cleanup(func() { sharedScalarEnabled = old })
	for _, workers := range []int{1, 2} {
		for _, typ := range []wasm.ValType{wasm.I32, wasm.I64} {
			for _, op := range []byte{0x6a, 0x71, 0x72, 0x73} {
				if typ == wasm.I64 {
					op += 0x12
				}
				t.Run(string(rune(op))+string(rune(workers)), func(t *testing.T) {
					body := []byte{0, 0x20, 0, 0x20, 1, op, 0x20, 0, 0x73, 0x0b}
					if typ == wasm.I64 {
						body[len(body)-2] = 0x85
					}
					nativeSourceCompileReport(t, body, []wasm.ValType{typ, typ}, []wasm.ValType{typ}, workers, true)
				})
			}
		}
		for _, body := range [][]byte{
			{1, 1, 0x7f, 0x20, 0, 0x20, 1, 0x6a, 0x22, 2, 0x20, 0, 0x73, 0x0b},
			{1, 1, 0x7f, 0x20, 0, 0x20, 1, 0x6a, 0x21, 2, 1, 0x20, 2, 0x20, 0, 0x73, 0x0b},
			{1, 1, 0x7f, 0x20, 2, 0x20, 0, 0x6a, 0x20, 1, 0x73, 0x0b},
			{0, 0x20, 0, 0x41, 3, 0x6a, 0x20, 1, 0x73, 0x0b},
		} {
			nativeSourceCompileReport(t, body, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, workers, true)
		}
		nativeSourceCompileReport(t, []byte{0, 1, 0x0b}, nil, nil, workers, true)
	}
}
func nativeSourceCompileReport(t *testing.T, body []byte, params, results []wasm.ValType, workers int, complete bool) {
	t.Helper()
	m := mod1(t, params, results, body)
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
	if len(reports) != 2 || reports[1].Result.Verdict != regalloccheck.Inconclusive {
		t.Fatalf("reports %+v", reports)
	}
	got := strings.Contains(reports[1].Result.Message, "source accounting complete;")
	if !complete && reports[1].Result.Reason != regalloccheck.UnsupportedOperation {
		t.Fatalf("unproved rewrite reported false source mismatch: %+v", reports)
	}
	if got != complete {
		t.Fatalf("body=%x workers=%d complete=%v reports=%+v", body, workers, complete, reports)
	}
}
func TestNativeSourcePlanUnprovedRewrites(t *testing.T) {
	old := sharedScalarEnabled
	sharedScalarEnabled = false
	t.Cleanup(func() { sharedScalarEnabled = old })
	for _, body := range [][]byte{
		{0, 0x41, 3, 0x41, 5, 0x6a, 0x0b},             // constant folding
		{0, 0x20, 0, 0x41, 0, 0x6a, 0x0b},             // identity
		{0, 0x20, 0, 0x20, 0, 0x73, 0x0b},             // same-operand simplification
		{1, 1, 0x7f, 0x20, 0, 0x21, 1, 0x20, 1, 0x0b}, // set/get fusion
		{0, 0x41, 3, 0x1a, 0x20, 0, 0x0b},             // dropped literal fusion
		{0, 0x20, 0, 0x20, 1, 0x6c, 0x0b},             // catalogue gap
	} {
		nativeSourceCompileReport(t, body, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, 1, false)
	}
}

// Model-only fixture selects original facts exactly as the driver does. Tests
// mutate actual DAG/local/constant choices between selection and observation.
func nativeSourceModel(t *testing.T, body []byte) *fn {
	t.Helper()
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, append([]byte{0}, body...))
	var a wasm.ValidatedModuleAnalysis
	if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, &a); err != nil {
		t.Fatal(err)
	}
	cg := codegen.SourceOptions(codegen.Options{}, m, &a, wasm.ValidationFeatures{})
	codegen.SetSourceReporter(cg, m, func(r codegen.SourceReport) {
		if r.Result.Verdict != regalloccheck.Inconclusive {
			t.Errorf("source-only model claimed machine verdict: %+v", r)
		}
	})
	sc := newCompileScratch(0)
	shared.SetSourceContext(&sc.scalar, codegen.SourceContextFor(cg, m))
	t.Cleanup(func() { sc.scalar.FinishWorker(); codegen.CloseSourceContext(cg, m) })
	f := &fn{a: sc.asm, s: newStack(), sc: sc, m: m, nParams: 2, nLocals: 2, localType: []machineType{mtI32, mtI32}}
	checkNativeSourceBegin(f, 0, true)
	if f.sourcePlan == nil {
		t.Fatal("native source plan missing")
	}
	t.Cleanup(func() { checkSourceClose(f) })
	return f
}
func nativeSourceModelGet(t *testing.T, f *fn, pc int, x uint32) *elem {
	t.Helper()
	checkNativeSourceBefore(f, pc, 0x20)
	checkNativeSourceGet(f, x)
	e := f.s.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: x})
	checkNativeSourceAfter(f, pc+2)
	if r := shared.SourcePlanStatus(f.sourcePlan.owner.Token); r.Reason != regalloccheck.NoFailure {
		t.Fatal(r)
	}
	return e
}
func TestNativeSourcePlanActualDAGControls(t *testing.T) {
	for _, control := range []string{"positive", "wrong operator", "wrong type", "swapped children", "missing child tag", "stale child generation", "prefix replaced", "missing prehook", "reader fusion"} {
		t.Run(control, func(t *testing.T) {
			f := nativeSourceModel(t, []byte{0x20, 0, 0x20, 1, 0x6a, 0x0b})
			left := nativeSourceModelGet(t, f, 0, 0)
			right := nativeSourceModelGet(t, f, 2, 1)
			if control != "missing prehook" {
				checkNativeSourceBefore(f, 4, 0x6a)
			}
			e := f.s.alloc()
			e.setElemKind(ekDeferred)
			e.setDeferredOp(opAdd)
			e.setValueType(mtI32)
			e.arg0, e.arg1 = left, right
			f.s.pushDeferred(e)
			st := f.sourcePlan
			switch control {
			case "wrong operator":
				e.setDeferredOp(opXor)
			case "wrong type":
				e.setValueType(mtI64)
			case "swapped children":
				e.arg0, e.arg1 = right, left
			case "missing child tag":
				delete(st.associations, left)
			case "stale child generation":
				a := st.associations[left]
				n, _ := shared.SourceSlotNode(a.ref)
				_, _ = shared.BindSourceSlot(st.owner.Token, a.slot, n)
			case "prefix replaced":
				e.arg0 = right
			}
			end := 5
			if control == "reader fusion" {
				end = 6
			}
			checkNativeSourceAfter(f, end)
			r := shared.SourcePlanStatus(st.owner.Token)
			if control == "positive" {
				if r.Reason != regalloccheck.NoFailure || r.Events != 3 {
					t.Fatal(r)
				}
			} else {
				want := regalloccheck.InvalidGraph
				if control == "missing prehook" || control == "reader fusion" {
					want = regalloccheck.UnsupportedOperation
				}
				if r.Reason != want {
					t.Fatalf("control=%s reason=%v want=%v", control, r.Reason, want)
				}
			}
		})
	}
}
func TestNativeSourcePlanActualSelections(t *testing.T) {
	for _, control := range []string{"wrong get index", "missing get hook", "wrong literal", "wrong set index", "wrong set input", "wrong tee mode", "wrong exit result"} {
		t.Run(control, func(t *testing.T) {
			body := []byte{0x20, 0, 0x0b}
			if control == "wrong literal" {
				body = []byte{0x41, 3, 0x0b}
			}
			if strings.HasPrefix(control, "wrong set") || control == "wrong tee mode" {
				body = []byte{0x20, 0, 0x22, 1, 0x0b}
			}
			if control == "wrong exit result" {
				body = []byte{0x20, 0, 0x20, 1, 0x1a, 0x0b}
			}
			f := nativeSourceModel(t, body)
			st := f.sourcePlan
			switch control {
			case "wrong get index", "missing get hook":
				checkNativeSourceBefore(f, 0, 0x20)
				x := uint32(0)
				if control == "wrong get index" {
					x = 1
					checkNativeSourceGet(f, x)
				}
				f.s.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: x})
				checkNativeSourceAfter(f, 2)
			case "wrong literal":
				checkNativeSourceBefore(f, 0, 0x41)
				f.s.pushIntegerConstant(mtI32, 4)
				checkNativeSourceAfter(f, 2)
			case "wrong set index", "wrong set input", "wrong tee mode":
				e := nativeSourceModelGet(t, f, 0, 0)
				checkNativeSourceBefore(f, 2, 0x22)
				x, tee := 1, true
				if control == "wrong set index" {
					x = 0
				}
				if control == "wrong tee mode" {
					tee = false
				}
				if control == "wrong set input" {
					e = &elem{st: storage{kind: stConst, typ: mtI32, cval: 3}}
				}
				checkNativeSourceSet(f, x, tee, e)
			case "wrong exit result":
				left := nativeSourceModelGet(t, f, 0, 0)
				right := nativeSourceModelGet(t, f, 2, 1)
				checkNativeSourceBefore(f, 4, 0x1a)
				f.s.erase(right)
				checkNativeSourceAfter(f, 5)
				a := st.associations[left]
				n := st.locals[1]
				a.ref, _ = shared.BindSourceSlot(st.owner.Token, a.slot, n)
				st.associations[left] = a
				checkNativeSourceBefore(f, 5, 0x0b)
			}
			want := regalloccheck.InvalidGraph
			if control == "missing get hook" || control == "wrong tee mode" || control == "unknown register" {
				want = regalloccheck.UnsupportedOperation
			}
			if r := shared.SourcePlanStatus(st.owner.Token); r.Reason != want {
				t.Fatalf("control=%s reason=%v want=%v", control, r.Reason, want)
			}
		})
	}
}

func TestNativeSourcePlanSharedRouteExcluded(t *testing.T) {
	old := sharedScalarEnabled
	sharedScalarEnabled = true
	t.Cleanup(func() { sharedScalarEnabled = old })
	for _, workers := range []int{1, 2} {
		nativeSourceCompileReport(t, []byte{0, 0x20, 0, 0x20, 1, 0x6a, 0x20, 0, 0x73, 0x0b}, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, workers, false)
	}
}
func TestNativeSourcePlanLocalRepresentationControls(t *testing.T) {
	for _, control := range []string{"unknown register", "wrong constant", "wrong returned index", "wrong shadow version", "missing tag"} {
		t.Run(control, func(t *testing.T) {
			f := nativeSourceModel(t, []byte{0x20, 0, 0x0b})
			st := f.sourcePlan
			if control == "wrong shadow version" {
				st.locals[0] = st.locals[1]
			}
			checkNativeSourceBefore(f, 0, 0x20)
			checkNativeSourceGet(f, 0)
			value := storage{kind: stLocalRef, typ: mtI32}
			switch control {
			case "unknown register":
				value.kind = stReg
				value.reg = RAX
			case "wrong constant":
				value.kind = stConst
				value.cval = 3
			case "wrong returned index":
				value.idx = 1
			}
			e := f.s.pushValue(value)
			checkNativeSourceAfter(f, 2)
			if control == "missing tag" {
				delete(st.associations, e)
				checkNativeSourceBefore(f, 2, 0x0b)
			}
			want := regalloccheck.InvalidGraph
			if control == "missing get hook" || control == "wrong tee mode" || control == "unknown register" {
				want = regalloccheck.UnsupportedOperation
			}
			if r := shared.SourcePlanStatus(st.owner.Token); r.Reason != want {
				t.Fatalf("control=%s reason=%v want=%v", control, r.Reason, want)
			}
		})
	}
}
func TestNativeSourcePlanTraversalBoundsAndUnwind(t *testing.T) {
	for _, control := range []string{"bad link", "depth mismatch", "cycle", "quota", "slot bound", "unfinished recipe", "panic"} {
		t.Run(control, func(t *testing.T) {
			f := nativeSourceModel(t, []byte{0x20, 0, 0x0b})
			st := f.sourcePlan
			owner := st.owner
			nativeSourceModelGet(t, f, 0, 0)
			switch control {
			case "bad link":
				f.s.head.next.prev = nil
			case "depth mismatch":
				f.s.logicalDepth++
			case "cycle":
				f.s.head.next.next = f.s.head.next
			case "quota":
				shared.ChargeSourcePlanAdapter(owner.Token, 1<<30, 0)
			case "slot bound":
				st.nextSlot = 4096
				st.bind(&elem{st: storage{kind: stLocalRef, typ: mtI32}}, st.locals[0])
			case "unfinished recipe":
				checkNativeSourceBefore(f, 2, 0x0b)
			case "panic":
				func() { defer func() { _ = recover() }(); defer f.checkEndLifetimes(); panic("model unwind") }()
				if f.sourcePlan != nil || owner.Token != (shared.SourcePlanToken{}) || st.associations != nil || st.locals != nil {
					t.Fatal("unwind retained source pools")
				}
				// Cleanup is idempotent; scratch reuse acquires a new exclusive owner.
				f.checkEndLifetimes()
				checkNativeSourceBegin(f, 0, true)
				if f.sourcePlan == nil {
					t.Fatal("unwind retained attempt")
				}
				return
			}
			if control != "unfinished recipe" {
				checkNativeSourceBefore(f, 2, 0x0b)
			}
			if control != "unfinished recipe" {
				want := regalloccheck.InvalidGraph
				if control == "quota" || control == "slot bound" {
					want = regalloccheck.ResourceLimit
				}
				if r := shared.SourcePlanStatus(owner.Token); r.Reason != want {
					t.Fatalf("control=%s reason=%v want=%v", control, r.Reason, want)
				}
			}
			checkSourceClose(f)
			if owner.Token != (shared.SourcePlanToken{}) || st.associations != nil || st.locals != nil {
				t.Fatal("cleanup retained source pools")
			}
		})
	}
}

func TestNativeSourcePlanUnprovedMultiResultExit(t *testing.T) {
	old := sharedScalarEnabled
	sharedScalarEnabled = false
	t.Cleanup(func() { sharedScalarEnabled = old })
	for _, workers := range []int{1, 2} {
		nativeSourceCompileReport(t, []byte{0, 0x20, 0, 0x20, 1, 0x0b}, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32, wasm.I32}, workers, false)
	}
}
func TestNativeSourcePlanDeferredDepthMaterialization(t *testing.T) {
	old := sharedScalarEnabled
	sharedScalarEnabled = false
	t.Cleanup(func() { sharedScalarEnabled = old })
	body := []byte{0, 0x20, 0, 0x20, 1, 0x6a}
	for i := 0; i < int(maxDeferDepth)+3; i++ {
		body = append(body, 0x20, 0, 0x73)
	}
	body = append(body, 0x0b)
	for _, workers := range []int{1, 2} {
		nativeSourceCompileReport(t, body, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, workers, true)
	}
}

func TestNativeSourcePlanPreservedPrefixControl(t *testing.T) {
	for _, replace := range []bool{false, true} {
		f := nativeSourceModel(t, []byte{0x20, 0, 0x20, 0, 0x20, 1, 0x6a, 0x1a, 0x0b})
		prefix := nativeSourceModelGet(t, f, 0, 0)
		left := nativeSourceModelGet(t, f, 2, 0)
		right := nativeSourceModelGet(t, f, 4, 1)
		checkNativeSourceBefore(f, 6, 0x6a)
		e := f.s.alloc()
		e.setElemKind(ekDeferred)
		e.setDeferredOp(opAdd)
		e.setValueType(mtI32)
		e.arg0, e.arg1 = left, right
		f.s.pushDeferred(e)
		st := f.sourcePlan
		if replace {
			fresh := f.s.alloc()
			fresh.st = prefix.st
			fresh.prev, fresh.next = prefix.prev, prefix.next
			fresh.prev.next, fresh.next.prev = fresh, fresh
			prefix.prev, prefix.next = nil, nil
			// Even matching desired provenance cannot legitimize an unobserved rewrite
			// of a lower live stack root during this source event.
			if !st.bind(fresh, st.nodes[0]) {
				t.Fatal("model prefix binding")
			}
		}
		checkNativeSourceAfter(f, 7)
		want := regalloccheck.NoFailure
		if replace {
			want = regalloccheck.InvalidGraph
		}
		if r := shared.SourcePlanStatus(st.owner.Token); r.Reason != want {
			t.Fatal(replace, r)
		}
	}
}
