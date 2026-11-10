//go:build wago_regalloccheck

package shared

import (
	"bytes"
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	x86 "github.com/wago-org/wago/src/core/encoder/amd64"
	"testing"
)

func sourceIntegerFinalModel(t *testing.T) (*SourceIntegerPhysical, []byte, codegen.Options, *wasm.Module, *[]codegen.SourceReport) {
	t.Helper()
	m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, Results: []wasm.ValType{wasm.I32}}}}}}, FuncTypes: []wasm.TypeIdx{{}, {}}, Code: []wasm.Func{{BodyBytes: []byte{0x20, 2, 0x0b}}, {BodyBytes: []byte{0x20, 0, 0x20, 1, 0x71, 0x20, 2, 0x73, 0x0b}}}}
	a := new(wasm.ValidatedModuleAnalysis)
	if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, a); err != nil {
		t.Fatal(err)
	}
	opts := codegen.SourceOptions(codegen.Options{}, m, a, wasm.ValidationFeatures{})
	ctx := codegen.SourceContextFor(opts, m)
	var reports []codegen.SourceReport
	codegen.SetSourceReporter(opts, m, func(r codegen.SourceReport) { reports = append(reports, r) })
	s := new(ScalarState)
	SetSourceContext(s, ctx)
	PrepareSourceIntegerFinalAttempt(s, m, 1)
	attempt := BeginSourceAttempt(s, m, 1)
	l, res, err := wasm.BuildSourceLedger(m, a, 1, wasm.ValidationFeatures{}, wasm.SourceLedgerLimits{})
	if err != nil || res.Coverage != wasm.SourceContractsComplete || !AttachSourceLedger(attempt, l) {
		t.Fatal("original ledger", res, err)
	}
	p, r := BeginSourcePlan(attempt, SourcePlanLimits{})
	if p.p == nil {
		t.Fatal(r)
	}
	t.Cleanup(func() { s.FinishWorker(); codegen.CloseSourceContext(opts, m) })
	j := BeginSourceMaterializationJournal(p, 8)
	physical := BeginSourceIntegerPhysical(j, true)
	get := func(event int) SourceNodeRef {
		r := sourcePlanStart(t, p, SourceRuleAlias, event)
		n := sourcePlanOutput(t, r)
		if !CommitSourceRecipe(r, 0, 0) {
			t.Fatal("get")
		}
		return n
	}
	x, y := get(0), get(1)
	r1 := sourcePlanStart(t, p, SourceRuleIntegerBinary, 2, x, y)
	sum := sourcePlanOutput(t, r1)
	if !CommitSourceRecipe(r1, 0, 0) {
		t.Fatal("and")
	}
	z := get(3)
	r2 := sourcePlanStart(t, p, SourceRuleIntegerBinary, 4, sum, z)
	out := sourcePlanOutput(t, r2)
	if !CommitSourceRecipe(r2, 0, 0) {
		t.Fatal("xor")
	}
	var encoder x86.Asm
	encoder.ObserveRegalloc(physical.ObserveEffect)
	encoder.ObserveGPWrites(physical.ObserveGPWrites)
	encoder.SubRsp(0)
	for _, op := range []struct {
		node   SourceNodeRef
		inputs []SourceNodeRef
		kind   wasm.InstrKind
		opcode byte
		right  x86.Reg
	}{{sum, []SourceNodeRef{x, y}, wasm.InstrI32And, 0x21, x86.RCX}, {out, []SourceNodeRef{sum, z}, wasm.InstrI32Xor, 0x31, x86.RDX}} {
		producer, ok := ExpectSourceMaterializationProducer(j, op.node, op.inputs, op.kind)
		if !ok {
			t.Fatal("producer")
		}
		token, ok := BeginSourceMaterialization(j, producer, op.node, op.inputs, op.kind, encoder.Len())
		if !ok {
			t.Fatal("receipt")
		}
		encoder.AluRR(op.opcode, x86.RAX, op.right, false)
		if !CommitSourceMaterialization(j, token, encoder.Len()) {
			t.Fatal("commit")
		}
	}
	encoder.AddRsp(0)
	encoder.Ret()
	exit := sourcePlanStart(t, p, SourceRuleExit, 5, out)
	if !CommitSourceRecipe(exit, 0, encoder.Len()) {
		t.Fatal("exit")
	}
	if _, r := SealSourcePlan(p); r.Readiness != SourceMappingReady {
		t.Fatal(r)
	}
	if r := EndSourceMaterializationEmission(j, encoder.Len()); r.Readiness != SourceMaterializationRecordingClosed {
		t.Fatal(r)
	}
	physical.EndEmission(encoder.Len())
	return physical, encoder.B, opts, m, &reports
}

func TestSourceIntegerFinalSnapshotAndDirectoryControls(t *testing.T) {
	for _, tc := range []struct {
		name    string
		verdict regalloccheck.Verdict
		reason  regalloccheck.FailureReason
	}{
		{"positive", regalloccheck.Verified, regalloccheck.NoFailure},
		{"changed snapshot input", regalloccheck.Inconclusive, regalloccheck.UnsupportedOperation},
		{"changed final bytes", regalloccheck.Inconclusive, regalloccheck.UnsupportedOperation},
		{"duplicate image", regalloccheck.Inconclusive, regalloccheck.UnsupportedOperation},
		{"wrong entry", regalloccheck.Inconclusive, regalloccheck.UnsupportedOperation},
		{"middle internal entry", regalloccheck.Inconclusive, regalloccheck.UnsupportedOperation},
		{"alias entry", regalloccheck.Inconclusive, regalloccheck.UnsupportedOperation},
		{"negative directory", regalloccheck.Inconclusive, regalloccheck.UnsupportedOperation},
		{"foreign admission", regalloccheck.Inconclusive, regalloccheck.UnsupportedOperation},
		{"matcher work limit", regalloccheck.Inconclusive, regalloccheck.ResourceLimit},
		{"retired attempt", regalloccheck.Inconclusive, regalloccheck.UnsupportedOperation},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, code, opts, m, reports := sourceIntegerFinalModel(t)
			ctx := codegen.SourceContextFor(opts, m)
			if r := p.VerifyAMD64(code); r.Verdict != regalloccheck.Verified {
				t.Fatal(r)
			}
			prefix := bytes.Repeat([]byte{0xcc}, 17)
			image := append(prefix, code...)
			image = append(image, 0xcc, 0xcc)
			entry, internal := []int{0, 17}, []int{0, 17}
			admit := true
			switch tc.name {
			case "changed snapshot input":
				code[8] ^= 1
				image = append(bytes.Repeat([]byte{0xcc}, 17), code...)
			case "changed final bytes":
				image[25] ^= 1
			case "duplicate image":
				image = append(image, code...)
			case "wrong entry":
				entry[1] = 0
			case "middle internal entry":
				internal[1]++
			case "alias entry":
				internal[0] = entry[1]
			case "negative directory":
				entry[1] = -1
			case "foreign admission":
				admit = false
			case "matcher work limit":
				image = append(image, bytes.Repeat([]byte{0xcc}, 4096-len(image))...)
			case "retired attempt":
				PrepareSourceIntegerFinalAttempt(p.materialization.plan.attempt.owner, m, 1)
			}
			// Source pools can retire before final layout. Only the owned checked
			// context snapshot is allowed to survive; no guest native image is run.
			EndSourceAttempt(p.materialization.plan.attempt)
			VerifySourceIntegerFinalModule(ctx, m, image, entry, internal, admit)
			codegen.CloseSourceContext(opts, m)
			if len(*reports) != 2 || (*reports)[0].Result.Verdict != regalloccheck.Inconclusive || (*reports)[1].Result.Verdict != tc.verdict || (*reports)[1].Result.Reason != tc.reason {
				t.Fatalf("reports=%+v want %v/%v", *reports, tc.verdict, tc.reason)
			}
		})
	}
}

func TestSourceIntegerUniqueImageOverlappingAndAdversarialPatterns(t *testing.T) {
	for _, tc := range []struct {
		image, pattern string
		start, count   int
	}{{"abcXYZdef", "XYZ", 3, 1}, {"abababa", "aba", 2, 2}, {"aaaaaa", "aaaa", 1, 2}, {"abababababac", "ababac", 6, 1}, {"aaaaab", "aaaac", -1, 0}} {
		start, count := sourceIntegerUniqueImage([]byte(tc.image), []byte(tc.pattern), make([]int, len(tc.pattern)))
		if start != tc.start || count != tc.count {
			t.Fatal(tc, start, count)
		}
	}
}

type sourceIntegerForeignWitness struct{ closed bool }

func (w *sourceIntegerForeignWitness) Close() { w.closed = true }

func TestSourceIntegerFinalOpaqueTransportHasNoPromotionAuthority(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		p, code, opts, m, reports := sourceIntegerFinalModel(t)
		ctx := codegen.SourceContextFor(opts, m)
		var opaque sourceIntegerForeignWitness
		if foreign && !codegen.StoreSourceFinalWitness(p.materialization.plan.attempt.finalAttempt, m, &opaque) {
			t.Fatal("opaque store")
		}
		VerifySourceIntegerFinalModule(ctx, m, code, []int{0, 0}, []int{0, 0}, true)
		if foreign && !opaque.closed {
			t.Fatal("foreign opaque witness retained")
		}
		if !codegen.ChargeFinalSourceWitnessPass(ctx, m, 16384, 8192) {
			t.Fatal("no-candidate pass spent final credits")
		}
		recordSourceIntegerFinalResult(ctx, 1, nil, codegen.SourceFinalAttempt{}, regalloccheck.Result{Verdict: regalloccheck.Verified})
		codegen.CloseSourceContext(opts, m)
		if len(*reports) != 2 || (*reports)[1].Result.Verdict != regalloccheck.Inconclusive {
			t.Fatalf("opaque/no-proof promotion: %+v", *reports)
		}
	}
}

func TestSourceIntegerFinalRepeatPhysicalProofRetiresCertificate(t *testing.T) {
	p, code, opts, m, reports := sourceIntegerFinalModel(t)
	ctx := codegen.SourceContextFor(opts, m)
	if r := p.VerifyAMD64(code); r.Verdict != regalloccheck.Verified {
		t.Fatal(r)
	}
	if r := p.VerifyAMD64(code); r.Verdict == regalloccheck.Verified {
		t.Fatal("repeated physical proof retained success", r)
	}
	image := append(bytes.Repeat([]byte{0xcc}, 17), code...)
	VerifySourceIntegerFinalModule(ctx, m, image, []int{0, 17}, []int{0, 17}, true)
	codegen.CloseSourceContext(opts, m)
	if len(*reports) != 2 || (*reports)[1].Result.Verdict == regalloccheck.Verified {
		t.Fatalf("retired certificate promoted: %+v", *reports)
	}
}
