//go:build amd64 && wago_regalloccheck && !tinygo && !wago_profile

package amd64

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
)

type scalarGraphTestTarget struct {
	*fn
	fault   string
	limits  regalloccheck.Limits
	result  regalloccheck.Result
	model   regalloccheck.Graph
	reports int
}

func (t *scalarGraphTestTarget) ScalarGraphLimits() regalloccheck.Limits { return t.limits }
func (t *scalarGraphTestTarget) ScalarGraphResult(g regalloccheck.Graph, r regalloccheck.Result) {
	t.model, t.result = g, r
	t.reports++
}
func (t *scalarGraphTestTarget) Load(r uint8, off int32, w bool) {
	if t.fault == "panic" {
		panic("injected scalar emission panic")
	}
	if t.fault == "wrong-load" {
		off += 8
	}
	t.fn.Load(r, off, w)
	if t.fault == "load-clobber" {
		t.fn.Constant(r, 99, w)
	}
}
func (t *scalarGraphTestTarget) Move(d, s uint8, w bool) {
	t.fn.Move(d, s, w)
	if t.fault == "copy-clobber" {
		t.fn.Constant(d, 99, w)
	}
}
func (t *scalarGraphTestTarget) Jump() int {
	if t.fault == "jump-clobber" {
		t.fn.Constant(uint8(gpAlloc[0]), 99, true)
	}
	return t.fn.Jump()
}
func (t *scalarGraphTestTarget) Patch(site, pos int) error {
	if t.fault == "wrong-branch" {
		pos += 4
	}
	if err := t.fn.Patch(site, pos); err != nil {
		return err
	}
	if t.fault == "branch-kind" {
		t.a.B[site-2], t.a.B[site-1] = 0x90, 0xe9
	}
	if t.fault == "unknown-branch" {
		t.a.B[site-1] = 0xff
	}
	return nil
}
func (t *scalarGraphTestTarget) Return(r uint8, w bool, slots int) {
	t.fn.Return(r, w, slots)
	if t.fault == "return-clobber" {
		t.fn.Constant(uint8(RAX), 99, w)
	}
}
func newScalarGraphTestTarget(regABI bool) *scalarGraphTestTarget {
	f := &fn{a: &encoder.Asm{}, nLocals: 4, nParams: 3, nLocalSlots: 4, singleRegResult: regABI, localSlot: []uint32{0, 8, 16, 24}}
	return &scalarGraphTestTarget{fn: f}
}
func scalarGraphCompile(t *testing.T, target *scalarGraphTestTarget, body []byte) {
	t.Helper()
	ft := &wasm.CompType{Params: []wasm.ValType{wasm.I32, wasm.I64, wasm.I64}, Results: []wasm.ValType{wasm.I64}}
	summary := shared.AdmitScalar(body, ft, append(append([]wasm.ValType{}, ft.Params...), wasm.I64))
	if !summary.Eligible {
		t.Fatal("fixture not admitted")
	}
	var state shared.ScalarState
	if _, err := state.CompileScalar(body, summary, []bool{false, true, true, true}, 3, target); err != nil {
		t.Fatal(err)
	}
	if target.reports != 1 {
		t.Fatalf("reports=%d", target.reports)
	}
}
func TestScalarGraphEmittedBranchesAndJoins(t *testing.T) {
	bodies := map[string][]byte{
		"identity":   {0x20, 1, 0x0b},
		"arithmetic": {0x20, 1, 0x20, 2, 0x7c, 0x0b},
		"if-result":  {0x20, 0, 0x04, 0x7e, 0x20, 1, 0x05, 0x20, 2, 0x0b, 0x0b},
		"compare":    {0x20, 1, 0x20, 2, 0x51, 0x04, 0x7e, 0x20, 1, 0x05, 0x20, 2, 0x0b, 0x0b},
		"scaled-add": {0x20, 1, 0x20, 2, 0x42, 2, 0x86, 0x7c, 0x0b},
		"shift":      {0x20, 1, 0x20, 2, 0x86, 0x0b},
		"nested":     {0x20, 0, 0x04, 0x7e, 0x20, 0, 0x04, 0x7e, 0x20, 1, 0x05, 0x20, 2, 0x0b, 0x05, 0x20, 2, 0x0b, 0x0b},
		"no-else":    {0x20, 0, 0x04, 0x40, 0x20, 1, 0x21, 2, 0x0b, 0x20, 2, 0x0b},
		"alias":      {0x20, 0, 0x04, 0x7e, 0x20, 1, 0x22, 2, 0x05, 0x20, 1, 0x22, 2, 0x0b, 0x20, 2, 0x7c, 0x0b},
	}
	for name, body := range bodies {
		for _, regABI := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/regABI=%v", name, regABI), func(t *testing.T) {
				target := newScalarGraphTestTarget(regABI)
				scalarGraphCompile(t, target, body)
				if target.result.Verdict != regalloccheck.Verified {
					t.Fatalf("result=%+v", target.result)
				}
				if len(target.a.B) == 0 || len(target.model.Inputs) != 3 {
					t.Fatal("not an emitted graph")
				}
				if name == "nested" && len(target.model.Blocks) < 7 {
					t.Fatalf("missing control graph: %d blocks", len(target.model.Blocks))
				}
			})
		}
	}
}
func TestScalarGraphRejectsWrongEmittedTransport(t *testing.T) {
	body := []byte{0x20, 0, 0x04, 0x7e, 0x20, 1, 0x05, 0x20, 2, 0x0b, 0x0b}
	for _, fault := range []string{"wrong-load", "load-clobber", "copy-clobber", "wrong-branch", "return-clobber", "jump-clobber", "branch-kind"} {
		t.Run(fault, func(t *testing.T) {
			target := newScalarGraphTestTarget(true)
			target.fault = fault
			defer func() {
				p := recover()
				if p == nil || !strings.Contains(fmt.Sprint(p), "shared scalar graph") {
					t.Fatalf("missing rejection: %v result=%+v", p, target.result)
				}
				if target.result.Verdict != regalloccheck.Rejected {
					t.Fatalf("result=%+v", target.result)
				}
				// Observers are restored before a graph failure is exposed.
				target.fn.Constant(uint8(RAX), 1, true)
				if target.reports != 1 {
					t.Fatal("retained graph observer")
				}
			}()
			scalarGraphCompile(t, target, body)
		})
	}
}
func TestScalarGraphInconclusiveLimitsAndUnknownEncoding(t *testing.T) {
	body := []byte{0x20, 0, 0x04, 0x7e, 0x20, 1, 0x05, 0x20, 2, 0x0b, 0x0b}
	for _, tc := range []struct {
		name   string
		limits regalloccheck.Limits
		fault  string
		reason regalloccheck.FailureReason
	}{
		{"construction", regalloccheck.Limits{Blocks: 1}, "", regalloccheck.ResourceLimit},
		{"analysis", regalloccheck.Limits{Work: 1}, "", regalloccheck.ResourceLimit},
		{"invalid-limits", regalloccheck.Limits{Work: -1}, "", regalloccheck.InvalidGraph},
		{"unknown-branch", regalloccheck.Limits{}, "unknown-branch", regalloccheck.UnsupportedOperation},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := newScalarGraphTestTarget(true)
			target.limits, target.fault = tc.limits, tc.fault
			scalarGraphCompile(t, target, body)
			if target.result.Verdict != regalloccheck.Inconclusive || target.result.Reason != tc.reason {
				t.Fatalf("result=%+v", target.result)
			}
		})
	}
}
func TestScalarGraphLoopRemainsFallback(t *testing.T) {
	body := []byte{0x03, 0x40, 0x20, 0, 0x0d, 0, 0x0b, 0x20, 1, 0x0b}
	ft := &wasm.CompType{Params: []wasm.ValType{wasm.I32, wasm.I64, wasm.I64}, Results: []wasm.ValType{wasm.I64}}
	if shared.AdmitScalar(body, ft, ft.Params).Eligible {
		t.Fatal("loop admitted without graph loop integration")
	}
	// Compile the real loop through the established backend, with PR800 checks.
	m := mod1(t, ft.Params, ft.Results, append([]byte{0}, body...))
	cm, err := CompileModuleWith(m, CompileOptions{Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
}

func TestScalarGraphIncompleteEmissionAndObserverCleanup(t *testing.T) {
	target := newScalarGraphTestTarget(true)
	gp, fx := 0, 0
	target.a.ObserveGPWrites(func(uint32) { gp++ })
	target.a.ObserveRegalloc(func(regalloccheck.Effect) { fx++ })
	ft := &wasm.CompType{Params: []wasm.ValType{wasm.I32, wasm.I64, wasm.I64}, Results: []wasm.ValType{wasm.I64}}
	valid := []byte{0x20, 1, 0x0f, 0x0b}
	summary := shared.AdmitScalar(valid, ft, append(append([]wasm.ValType{}, ft.Params...), wasm.I64))
	if !summary.Eligible {
		t.Fatal("fixture not admitted")
	}
	var state shared.ScalarState
	// A caller violating the immutable admitted-bytes contract must not get a
	// successful graph report after the return followed by an emission error.
	if _, err := state.CompileScalar(valid[:len(valid)-1], summary, []bool{false, true, true, true}, 3, target); err == nil {
		t.Fatal("missing decode error")
	}
	if target.result.Verdict != regalloccheck.Inconclusive {
		t.Fatalf("result=%+v", target.result)
	}
	before := gp
	target.fn.Constant(0, 5, true)
	if gp != before+1 || fx == 0 {
		t.Fatal("previous observers not restored/composed")
	}
	target.reports = 0
	if _, err := state.CompileScalar(valid, summary, []bool{false, true, true, true}, 3, target); err != nil {
		t.Fatal(err)
	}
	if target.reports != 1 || target.result.Verdict != regalloccheck.Verified {
		t.Fatalf("reuse=%+v", target.result)
	}
}

func TestScalarGraphRestoresObserversAfterEmissionPanic(t *testing.T) {
	target := newScalarGraphTestTarget(true)
	target.fault = "panic"
	writes := 0
	target.a.ObserveGPWrites(func(uint32) { writes++ })
	ft := &wasm.CompType{Params: []wasm.ValType{wasm.I32, wasm.I64, wasm.I64}, Results: []wasm.ValType{wasm.I64}}
	body := []byte{0x20, 1, 0x0b}
	summary := shared.AdmitScalar(body, ft, append(append([]wasm.ValType{}, ft.Params...), wasm.I64))
	var state shared.ScalarState
	func() {
		defer func() {
			if p := recover(); p != "injected scalar emission panic" {
				t.Fatalf("panic changed: %v", p)
			}
		}()
		state.CompileScalar(body, summary, []bool{false, true, true, true}, 3, target)
	}()
	before := writes
	target.fn.Constant(0, 5, true)
	if writes != before+1 || target.reports != 0 {
		t.Fatal("observer/graph retained after panic")
	}
	target.fault = ""
	if _, err := state.CompileScalar(body, summary, []bool{false, true, true, true}, 3, target); err != nil {
		t.Fatal(err)
	}
	if target.reports != 1 || target.result.Verdict != regalloccheck.Verified {
		t.Fatalf("reuse=%+v", target.result)
	}
}

// Change the emitted frame operand while retaining the semantic right operand.
// The obligation must bind to encoder Read, not to the allocator's offset.
func (t *scalarGraphTestTarget) Binary(op shared.IntOp, w bool, d, l uint8, r shared.ScalarOperand) {
	if t.fault == "wrong-folded" && r.Kind == shared.ScalarFrame {
		r.Offset += 8
	}
	t.fn.Binary(op, w, d, l, r)
}
func TestScalarGraphRejectsWrongObservedFoldedRead(t *testing.T) {
	target := newScalarGraphTestTarget(true)
	target.fault = "wrong-folded"
	defer func() {
		if p := recover(); p == nil || target.result.Verdict != regalloccheck.Rejected {
			t.Fatalf("folded result=%+v panic=%v", target.result, p)
		}
	}()
	scalarGraphCompile(t, target, []byte{0x20, 1, 0x20, 2, 0x7c, 0x0b})
}
