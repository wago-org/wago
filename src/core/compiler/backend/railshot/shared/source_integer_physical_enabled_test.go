//go:build wago_regalloccheck

package shared

import (
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	x86 "github.com/wago-org/wago/src/core/encoder/amd64"
	"testing"
)

// All negative images are inspected only. They are never mapped or executed.
func sourceIntegerPhysicalModel(t *testing.T, wide bool, mode string) (*SourceIntegerPhysical, []byte) {
	t.Helper()
	typ, first, second := wasm.I32, byte(0x71), byte(0x73)
	if wide {
		typ, first, second = wasm.I64, 0x83, 0x85
	}
	p, attempt := sourcePlanFixture(t, []byte{0x20, 0, 0x20, 1, first, 0x20, 2, second, 0x0b}, []wasm.ValType{typ, typ, typ}, []wasm.ValType{typ}, nil, SourcePlanLimits{})
	j := BeginSourceMaterializationJournal(p, 8)
	physical := BeginSourceIntegerPhysical(j, true)
	if physical == nil || !physical.admitted {
		t.Fatal("physical admission")
	}
	get := func(event int) SourceNodeRef {
		r := sourcePlanStart(t, p, SourceRuleAlias, event)
		n := sourcePlanOutput(t, r)
		if !CommitSourceRecipe(r, 0, 0) {
			t.Fatal("get")
		}
		return n
	}
	x, y := get(0), get(1)
	b := sourcePlanStart(t, p, SourceRuleIntegerBinary, 2, x, y)
	sum := sourcePlanOutput(t, b)
	if !CommitSourceRecipe(b, 0, 0) {
		t.Fatal("first recipe")
	}
	z := get(3)
	b = sourcePlanStart(t, p, SourceRuleIntegerBinary, 4, sum, z)
	out := sourcePlanOutput(t, b)
	if !CommitSourceRecipe(b, 0, 0) {
		t.Fatal("second recipe")
	}
	kind1, kind2 := wasm.InstrI32And, wasm.InstrI32Xor
	if wide {
		kind1, kind2 = wasm.InstrI64And, wasm.InstrI64Xor
	}
	roles := []SourceNodeRef{x, y}
	dst, src := x86.RAX, x86.RCX
	if mode == "commuted" {
		roles = []SourceNodeRef{y, x}
		dst, src = x86.RCX, x86.RAX
	}
	record := mode != "missing receipts"
	var producer1, producer2 SourceMaterializationProducer
	var token SourceMaterializationToken
	var ok bool
	if record {
		producer1, ok = ExpectSourceMaterializationProducer(j, sum, roles, kind1)
		if !ok {
			t.Fatal("producer1")
		}
		producer2, ok = ExpectSourceMaterializationProducer(j, out, []SourceNodeRef{sum, z}, kind2)
		if !ok {
			t.Fatal("producer2")
		}
	}
	var a x86.Asm
	a.ObserveRegalloc(physical.ObserveEffect)
	a.ObserveGPWrites(physical.ObserveGPWrites)
	if mode == "missing observer" {
		a.ObserveGPWrites(nil)
	}
	frame := int32(0)
	if mode == "frame" {
		frame = 8
	}
	a.SubRsp(frame)
	move := func(d, s x86.Reg) {
		if wide {
			a.MovReg64(d, s)
		} else {
			a.MovRegReg32(d, s)
		}
	}
	if record {
		token, ok = BeginSourceMaterialization(j, producer1, sum, roles, kind1, a.Len())
		if !ok {
			t.Fatal("begin1")
		}
	}
	a.AluRR(0x21, dst, src, wide)
	if record && !CommitSourceMaterialization(j, token, a.Len()) {
		t.Fatal("commit1")
	}
	if mode == "commuted" {
		move(x86.RAX, x86.RCX)
	}
	if mode == "partial clobber" {
		a.MovRegReg32(x86.RAX, x86.RAX)
	}
	if record {
		token, ok = BeginSourceMaterialization(j, producer2, out, []SourceNodeRef{sum, z}, kind2, a.Len())
		if !ok {
			t.Fatal("begin2")
		}
	}
	op := byte(0x31)
	if mode == "wrong operator" {
		op = 9
	}
	right := x86.RDX
	if mode == "wrong input" {
		right = x86.RCX
	}
	w := wide
	if mode == "wrong width" {
		w = !wide
	}
	a.AluRR(op, x86.RAX, right, w)
	if record && !CommitSourceMaterialization(j, token, a.Len()) {
		t.Fatal("commit2")
	}
	if mode == "wrong return" {
		move(x86.RAX, x86.RCX)
	}
	if mode == "extra arithmetic" {
		a.AluRR(0x31, x86.RAX, x86.RCX, wide)
	}
	if mode == "raw gap" {
		a.B = append(a.B, 0x90)
	}
	a.AddRsp(frame)
	a.Ret()
	b = sourcePlanStart(t, p, SourceRuleExit, 5, out)
	if !CommitSourceRecipe(b, 0, a.Len()) {
		t.Fatal("exit")
	}
	_, r := SealSourcePlan(p)
	if r.Readiness != SourceMappingReady {
		t.Fatal(r)
	}
	if r := EndSourceMaterializationEmission(j, a.Len()); r.Readiness != SourceMaterializationRecordingClosed {
		t.Fatal(r)
	}
	physical.EndEmission(a.Len())
	if mode == "changed length" {
		a.B = append(a.B, 0x90)
	}
	if mode == "resource" {
		attempt.owner.sourceWork = 1
	}
	if mode == "retired" {
		EndSourceAttempt(attempt)
	}
	return physical, a.B
}

func TestSourceIntegerPhysicalWholeImageControls(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, tc := range []struct {
			name    string
			verdict regalloccheck.Verdict
			reason  regalloccheck.FailureReason
		}{
			{"positive", regalloccheck.Verified, regalloccheck.NoFailure}, {"commuted", regalloccheck.Verified, regalloccheck.NoFailure},
			{"wrong input", regalloccheck.Rejected, regalloccheck.ProvenanceMismatch}, {"wrong operator", regalloccheck.Rejected, regalloccheck.ProvenanceMismatch}, {"wrong width", regalloccheck.Rejected, regalloccheck.ProvenanceMismatch}, {"wrong return", regalloccheck.Rejected, regalloccheck.ProvenanceMismatch},
			{"extra arithmetic", regalloccheck.Inconclusive, regalloccheck.UnsupportedOperation}, {"missing observer", regalloccheck.Inconclusive, regalloccheck.UnsupportedOperation}, {"raw gap", regalloccheck.Inconclusive, regalloccheck.UnsupportedOperation}, {"frame", regalloccheck.Inconclusive, regalloccheck.UnsupportedOperation}, {"changed length", regalloccheck.Inconclusive, regalloccheck.UnsupportedOperation}, {"resource", regalloccheck.Inconclusive, regalloccheck.ResourceLimit}, {"retired", regalloccheck.Inconclusive, regalloccheck.InvalidGraph},
			{"missing receipts", regalloccheck.Inconclusive, regalloccheck.UnsupportedOperation},
		} {
			t.Run(tc.name+map[bool]string{false: "/i32", true: "/i64"}[wide], func(t *testing.T) {
				p, code := sourceIntegerPhysicalModel(t, wide, tc.name)
				r := p.VerifyAMD64(code)
				if r.Verdict != tc.verdict || r.Reason != tc.reason {
					t.Fatalf("%+v want %v/%v", r, tc.verdict, tc.reason)
				}
			})
		}
	}
	p, code := sourceIntegerPhysicalModel(t, true, "partial clobber")
	if r := p.VerifyAMD64(code); r.Verdict != regalloccheck.Rejected || r.Reason != regalloccheck.UnknownInput {
		t.Fatal("high-half clobber", r)
	}
}

func TestSourceIntegerPhysicalCannotReuseProofForChangedBytes(t *testing.T) {
	p, code := sourceIntegerPhysicalModel(t, false, "positive")
	if r := p.VerifyAMD64(code); r.Verdict != regalloccheck.Verified {
		t.Fatal(r)
	}
	code[8] ^= 1 // Change a live ALU operand in memory; never execute it.
	if r := p.VerifyAMD64(code); r.Verdict != regalloccheck.Inconclusive || r.Reason != regalloccheck.InvalidGraph {
		t.Fatal("reused prior proof", r)
	}
	if p.Result().Verdict == regalloccheck.Verified {
		t.Fatal("retained successful proof after reuse")
	}
}

func TestSourceIntegerPhysicalReusePreservesEarlierQuota(t *testing.T) {
	p, code := sourceIntegerPhysicalModel(t, false, "resource")
	if r := p.VerifyAMD64(code); r.Reason != regalloccheck.ResourceLimit {
		t.Fatal(r)
	}
	if r := p.VerifyAMD64(code); r.Reason != regalloccheck.InvalidGraph {
		t.Fatal(r)
	}
	if p.Result().Reason != regalloccheck.ResourceLimit {
		t.Fatal("lost historical quota refusal", p.Result())
	}
}

func TestSourceIntegerPhysicalAdmissionAndObserverReservation(t *testing.T) {
	for _, tc := range []struct {
		name            string
		body            []byte
		params, results []wasm.ValType
		locals          []wasm.LocalRun
	}{
		{"floating ABI", []byte{0x20, 0, 0x0b}, []wasm.ValType{wasm.F32}, []wasm.ValType{wasm.F32}, nil},
		{"literal", []byte{0x41, 0, 0x0b}, nil, []wasm.ValType{wasm.I32}, nil},
		{"declared zero", []byte{0x20, 0, 0x0b}, nil, []wasm.ValType{wasm.I32}, []wasm.LocalRun{{Count: 1, Type: wasm.I32}}},
		{"stack argument", []byte{0x20, 0, 0x0b}, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, attempt := sourcePlanFixture(t, tc.body, tc.params, tc.results, tc.locals, SourcePlanLimits{})
			j := BeginSourceMaterializationJournal(plan, 8)
			before := attempt.owner.sourceStorage
			p := BeginSourceIntegerPhysical(j, true)
			if p == nil || p.ObservationReady() || p.journal != nil || p.Result().Verdict != regalloccheck.Inconclusive || attempt.owner.sourceStorage != before-1 {
				t.Fatal("unadmitted observation reservation", p)
			}
			if other := BeginSourceIntegerPhysical(j, true); other != nil {
				t.Fatal("nested physical owner")
			}
		})
	}
	plan, attempt, j, _, _ := sourceMaterializationFixture(t)
	before := attempt.owner.sourceStorage
	p := BeginSourceIntegerPhysical(j, false)
	if p == nil || p.ObservationReady() || p.journal != nil || attempt.owner.sourceStorage != before-1 {
		t.Fatal("disabled observation")
	}
	EndSourceAttempt(attempt)
	if p.ObservationReady() || BeginSourceIntegerPhysical(j, true) != nil || ChargeSourcePlanAdapter(plan, 1, 0) {
		t.Fatal("retired observation authority")
	}
}
