//go:build wago_regalloccheck

package shared

import (
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	x86 "github.com/wago-org/wago/src/core/encoder/amd64"
	"testing"
)

func sourceBranchFixture(t *testing.T) (*ScalarState, *wasm.Module) {
	t.Helper()
	m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32, wasm.I32}, Results: []wasm.ValType{wasm.I32}}}}}}, FuncTypes: []wasm.TypeIdx{{}}, Code: []wasm.Func{{BodyBytes: []byte{0x20, 0, 0x04, 0x7f, 0x20, 1, 0x05, 0x20, 0, 0x0b, 0x0b}}}}
	a := new(wasm.ValidatedModuleAnalysis)
	if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, a); err != nil {
		t.Fatal(err)
	}
	opts := codegen.SourceOptions(codegen.Options{}, m, a, wasm.ValidationFeatures{})
	s := new(ScalarState)
	SetSourceContext(s, codegen.SourceContextFor(opts, m))
	t.Cleanup(func() { s.FinishWorker(); codegen.CloseSourceContext(opts, m) })
	return s, m
}

// Negative encoder images are inspected only; no emitted bytes are executed.
func TestSourceBranchEncoderProofControls(t *testing.T) {
	for _, test := range []struct {
		name    string
		verdict regalloccheck.Verdict
	}{
		{"positive", regalloccheck.Verified},
		{"wide spill", regalloccheck.Verified},
		{"wrong condition", regalloccheck.Rejected},
		{"wide condition", regalloccheck.Inconclusive},
		{"different test operands", regalloccheck.Inconclusive},
		{"aliased test operands", regalloccheck.Inconclusive},
		{"reused condition pin", regalloccheck.Inconclusive},
		{"reused merge pin", regalloccheck.Inconclusive},
		{"large balanced frame", regalloccheck.Inconclusive},
		{"wrong then", regalloccheck.Rejected},
		{"wrong else", regalloccheck.Rejected},
		{"wrong return", regalloccheck.Rejected},
		{"duplicate pins", regalloccheck.Inconclusive},
		{"wrong predicate", regalloccheck.Inconclusive},
		{"wrong false target", regalloccheck.Inconclusive},
		{"wrong join target", regalloccheck.Inconclusive},
		{"reversed predicate with swapped arms", regalloccheck.Inconclusive},
		{"preloaded merge skips else", regalloccheck.Inconclusive},
		{"frame bounds", regalloccheck.Rejected},
		{"negative frame offset", regalloccheck.Rejected},
		{"SP balance", regalloccheck.Rejected},
		{"missing observer", regalloccheck.Inconclusive},
		{"raw gap", regalloccheck.Inconclusive},
		{"changed length", regalloccheck.Inconclusive},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, m := sourceBranchFixture(t)
			b := BeginSourceBranch(s, m, 0, true)
			if b == nil {
				t.Fatal("not admitted")
			}
			defer b.Close()
			var a x86.Asm
			a.ObserveRegalloc(b.ObserveEffect)
			a.ObserveGPWrites(b.ObserveGPWrites)
			if test.name == "missing observer" {
				a.ObserveGPWrites(nil)
			}
			frame := int32(24)
			if test.name == "large balanced frame" {
				frame = 256
			}
			a.SubRsp(frame)
			a.MovRegReg32(x86.R9, x86.RAX)
			pin := x86.R10
			if test.name == "duplicate pins" {
				pin = x86.R9
			}
			a.MovRegReg32(pin, x86.RCX)
			off := int32(8)
			if test.name == "frame bounds" {
				off = 24
			}
			if test.name == "negative frame offset" {
				off = -4
			}
			src := x86.R9
			if test.name == "duplicate pins" {
				src = x86.RAX
			}
			if test.name == "wrong condition" {
				src = x86.R10
			}
			if test.name == "wide spill" {
				a.Store64(x86.RSP, off, src)
			} else {
				a.Store32(x86.RSP, off, src)
			}
			conditionReg := x86.RDI
			if test.name == "reused condition pin" {
				conditionReg = x86.R10
			}
			if test.name == "preloaded merge skips else" {
				conditionReg = x86.RBP
			}
			a.Load32(conditionReg, x86.RSP, off)
			if test.name == "raw gap" {
				a.EmitBytes([]byte{0x90})
			}
			if test.name == "different test operands" {
				a.TestReg(x86.RDI, x86.R10, false)
			} else if test.name == "aliased test operands" {
				a.TestReg(x86.RDI, x86.R9, false)
			} else {
				a.TestSelf(conditionReg, test.name == "wide condition")
			}
			condition := x86.CondE
			if test.name == "wrong predicate" || test.name == "reversed predicate with swapped arms" {
				condition = x86.CondNE
			}
			branch := a.JccPlaceholder(condition)
			thenSrc := x86.R10
			if test.name == "duplicate pins" {
				thenSrc = x86.R9
			}
			if test.name == "reused condition pin" {
				thenSrc = x86.RCX
			}
			if test.name == "wrong then" {
				thenSrc = x86.R9
			}
			if test.name == "reversed predicate with swapped arms" {
				thenSrc = x86.R9
			}
			merge := x86.RBP
			if test.name == "reused merge pin" {
				merge = x86.R9
			}
			a.MovRegReg32(merge, thenSrc)
			jump := a.JmpPlaceholder()
			falseStart := a.Len()
			elseSrc := x86.R9
			if test.name == "duplicate pins" {
				elseSrc = x86.RAX
			}
			if test.name == "wrong else" {
				elseSrc = x86.R10
			}
			if test.name == "reversed predicate with swapped arms" {
				elseSrc = x86.R10
			}
			elseMerge := merge
			if test.name == "preloaded merge skips else" {
				elseMerge = x86.RDI
			}
			a.MovRegReg32(elseMerge, elseSrc)
			join := a.Len()
			returnSrc := merge
			if test.name == "wrong return" {
				returnSrc = x86.R10
			}
			a.MovRegReg32(x86.RAX, returnSrc)
			restore := frame
			if test.name == "SP balance" {
				restore = 32
			}
			a.AddRsp(restore)
			a.Ret()
			if test.name == "wrong false target" {
				falseStart++
			}
			if test.name == "preloaded merge skips else" {
				falseStart = join
			}
			if test.name == "wrong join target" {
				join++
			}
			a.PatchRel32(branch, falseStart)
			a.PatchRel32(jump, join)
			b.EndEmission(a.Len())
			code := a.B
			if test.name == "changed length" {
				code = code[:len(code)-1]
			}
			r := b.Verify(code)
			if r.Verdict != test.verdict {
				t.Fatalf("result=%+v code=%x", r, code)
			}
			if again := b.Verify(code); again != r {
				t.Fatal("duplicate verification changed result")
			}
			journal, ledger, token := b.journal, b.ledger, b.attempt
			work, storage := s.sourceWork, s.sourceStorage
			b.Close()
			b.Close()
			if b.owner != nil || journal.Finalize(0, 0, nil).State == regalloccheck.JournalReady || ledger.EventCount() != 0 || token.owner != nil || s.sourceAttempt != nil || s.sourceWork != work || s.sourceStorage != storage {
				t.Fatal("close retained proof or refunded history")
			}
		})
	}
}

func TestSourceBranchQuotaAndAbandonment(t *testing.T) {
	for _, test := range []string{"work", "storage", "abandon", "retry"} {
		t.Run(test, func(t *testing.T) {
			s, m := sourceBranchFixture(t)
			if test == "work" {
				s.sourceWork = 0
			}
			if test == "storage" {
				s.sourceStorage = 0
			}
			b := BeginSourceBranch(s, m, 0, true)
			if test == "work" || test == "storage" {
				if b != nil || s.sourceAttempt != nil {
					t.Fatal("exhaustion selected defaults")
				}
				return
			}
			if b == nil {
				t.Fatal("not admitted")
			}
			storage := s.sourceStorage
			b.Close()
			if s.sourceAttempt != nil || s.sourceStorage != storage {
				t.Fatal("abandon retained/refunded state")
			}
			if test == "retry" {
				next := BeginSourceBranch(s, m, 0, true)
				if next == nil || s.sourceStorage != storage-8192 {
					t.Fatal("retry refunded quota")
				}
				next.Close()
			}
		})
	}
}

func TestSourceBranchRetiredWorkerCannotReuseProof(t *testing.T) {
	s, m := sourceBranchFixture(t)
	b := BeginSourceBranch(s, m, 0, true)
	if b == nil {
		t.Fatal("not admitted")
	}
	s.FinishWorker()
	var a wasm.ValidatedModuleAnalysis
	if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, &a); err != nil {
		t.Fatal(err)
	}
	opts := codegen.SourceOptions(codegen.Options{}, m, &a, wasm.ValidationFeatures{})
	var reports []codegen.SourceReport
	codegen.SetSourceReporter(opts, m, func(r codegen.SourceReport) { reports = append(reports, r) })
	SetSourceContext(s, codegen.SourceContextFor(opts, m))
	codegen.RecordSourceResult(s.sourceContext, 0, regalloccheck.Result{Verdict: regalloccheck.Verified})
	b.result = regalloccheck.Result{Verdict: regalloccheck.Rejected}
	work, storage := s.sourceWork, s.sourceStorage
	if r := b.Verify(make([]byte, 64)); r.Verdict != regalloccheck.Inconclusive || r.Reason != regalloccheck.InvalidGraph {
		t.Fatalf("retired proof=%+v", r)
	}
	b.Close()
	if s.sourceWork != work || s.sourceStorage != storage || s.sourceAttempt != nil {
		t.Fatal("retired proof charged fresh worker")
	}
	s.FinishWorker()
	codegen.CloseSourceContext(opts, m)
	if len(reports) != 1 || reports[0].Result.Verdict != regalloccheck.Verified {
		t.Fatalf("old report overwrote fresh context: %+v", reports)
	}
}

func TestSourceBranchMetadataBoundBeforeSignature(t *testing.T) {
	_, small := sourceBranchFixture(t)
	m := &wasm.Module{Types: append([]wasm.RecType(nil), small.Types...), FuncTypes: small.FuncTypes, Code: small.Code}
	params := make([]wasm.ValType, 100)
	for i := range params {
		params[i] = wasm.I32
	}
	for i := 0; i < 8; i++ {
		m.Types = append(m.Types, wasm.RecType{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompFunc, Params: params}}}})
	}
	var a wasm.ValidatedModuleAnalysis
	if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, &a); err != nil {
		t.Fatal(err)
	}
	opts := codegen.SourceOptions(codegen.Options{}, m, &a, wasm.ValidationFeatures{})
	var reports []codegen.SourceReport
	codegen.SetSourceReporter(opts, m, func(r codegen.SourceReport) { reports = append(reports, r) })
	s := new(ScalarState)
	SetSourceContext(s, codegen.SourceContextFor(opts, m))
	work, storage := s.sourceWork, s.sourceStorage
	if b := BeginSourceBranch(s, m, 0, true); b != nil {
		b.Close()
		t.Fatal("metadata limit admitted")
	}
	if s.sourceAttempt != nil || work-s.sourceWork > 32+4096 || s.sourceStorage != storage-8192 {
		t.Fatal("metadata attempt escaped bounds or refunded history")
	}
	s.FinishWorker()
	codegen.CloseSourceContext(opts, m)
	if len(reports) != 1 || reports[0].Result.Verdict != regalloccheck.Inconclusive || reports[0].Result.Reason != regalloccheck.ResourceLimit {
		t.Fatalf("metadata report=%+v", reports)
	}
}
