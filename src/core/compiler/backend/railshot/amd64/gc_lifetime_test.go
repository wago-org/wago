//go:build amd64

package amd64

import (
	"bytes"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func compileGCLifetimeCode(t *testing.T, m *wasm.Module, on bool) []byte {
	t.Helper()
	cm, err := CompileModuleWith(m, CompileOptions{Optimizations: lifetimeSelection(on), Workers: 1, GCStructHelpers: true, DeferCodeMapping: true})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		t.Cleanup(func() { _ = cm.CodeImage.Close() })
	}
	return append([]byte(nil), cm.Code...)
}

func TestGCLifetimePublicationFallback(t *testing.T) {
	if !gc.NativeDeadBatchSupported || profileEnabled {
		t.Skip("consumer profile fallback")
	}
	m := lifetimeFixture(t, "qualifying", 1)
	baseline := compileGCLifetimeCode(t, m, false)
	t.Cleanup(func() {
		gcLifetimePlanTestHook = nil
		gcLifetimeEmissionTestHook = nil
	})
	cases := []struct {
		name string
		plan func(*gcLifetimePlan)
		emit func(*fn, int) error
	}{
		{name: "invalid", plan: func(p *gcLifetimePlan) { p.records[0].Descriptor++ }},
		{name: "partial-failure", emit: func(_ *fn, emitted int) error {
			if emitted == 2 {
				return errors.New("injected candidate emission failure")
			}
			return nil
		}},
		{name: "oversized", emit: func(f *fn, emitted int) error {
			if emitted == 1 {
				f.a.B = append(f.a.B, make([]byte, 8192)...)
			}
			return nil
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gcLifetimePlanTestHook, gcLifetimeEmissionTestHook = tc.plan, tc.emit
			if got := compileGCLifetimeCode(t, m, true); !bytes.Equal(got, baseline) {
				t.Fatal("rejected candidate did not publish retained baseline")
			}
			gcLifetimePlanTestHook, gcLifetimeEmissionTestHook = nil, nil
			if got := compileGCLifetimeCode(t, m, true); len(got) == 0 {
				t.Fatal("clean retry after rejected candidate emitted no code")
			}
		})
	}
}

func TestGCLifetimePanicUnwindsWorkerPlan(t *testing.T) {
	if !gc.NativeDeadBatchSupported || profileEnabled {
		t.Skip("consumer profile fallback")
	}
	m := lifetimeFixture(t, "qualifying", 1)
	t.Cleanup(func() { gcLifetimePlanTestHook = nil })
	gcLifetimePlanTestHook = func(*gcLifetimePlan) { panic("injected plan verifier panic") }
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("injected verifier panic did not propagate")
			}
		}()
		compileGCLifetimeCode(t, m, true)
	}()
	gcLifetimePlanTestHook = nil
	if got := compileGCLifetimeCode(t, m, true); len(got) == 0 {
		t.Fatal("worker scratch was not reusable after panic unwind")
	}
}

func TestGCLifetimeProof(t *testing.T) {
	m := lifetimeFixture(t, "qualifying", 1)
	r := analyzeGCLifetime(m, 0, gcLifetimeMaxRecords)
	if !r.Candidate || !r.DenominatorKnown || r.Sites != 1 || r.Reads != 1 || r.Aliases != 2 || r.Identity == ([32]byte{}) {
		t.Fatalf("%+v", r)
	}
	semantic := analyzeGCLifetimeWithRecords(m, 0, gcLifetimeMaxRecords, nil, false)
	if !semantic.Candidate || semantic.Identity != ([32]byte{}) {
		t.Fatalf("semantic proof performed diagnostic identity work: %+v", semantic)
	}
	cases := []struct {
		name   string
		body   []byte
		reason string
	}{
		{"adjacent", []byte{0x41, 42, 0xfb, 0, 0, 0xfb, 2, 0, 0, 0x0b}, "closed-scalar-struct"},
		{"default", []byte{0xfb, 1, 0, 0x1a, 0x41, 1, 0x0b}, "closed-scalar-struct"},
		{"overwrite", []byte{0xfb, 1, 0, 0x21, 0, 0xfb, 1, 0, 0x21, 0, 0x41, 1, 0x0b}, "multiple-constructors"},
		{"alias-overwrite", []byte{0xfb, 1, 0, 0x22, 0, 0x21, 1, 0x20, 1, 0x21, 0, 0x20, 0, 0xfb, 2, 0, 0, 0x0b}, "closed-scalar-struct"},
		{"call", []byte{0xfb, 1, 0, 0x10, 0, 0x0b}, "unsupported-opcode-0x10"},
		{"store", []byte{0xfb, 1, 0, 0x24, 0, 0x0b}, "unsupported-opcode-0x24"},
		{"return", []byte{0xfb, 1, 0, 0x0f, 0x0b}, "unsupported-opcode-0x0f"},
		{"branch", []byte{0xfb, 1, 0, 0x02, 0x40, 0x0b, 0x0b}, "unsupported-opcode-0x02"},
		{"identity", []byte{0xfb, 1, 0, 0xd1, 0x0b}, "unsupported-opcode-0xd1"},
		{"unknown", []byte{0xfb, 1, 0, 0xff, 0x0b}, "unsupported-opcode-0xff"},
		{"array", []byte{0xfb, 6, 0, 0x0b}, "unsupported-gc"},
		{"truncated", []byte{0xfb, 0}, "malformed"},
		{"missing-end", []byte{0xfb, 1, 0, 0x1a}, "malformed"},
		{"trailing", []byte{0xfb, 1, 0, 0x1a, 0x41, 1, 0x0b, 0x01}, "malformed"},
		{"stack-underflow", []byte{0xfb, 0, 0, 0x0b}, "malformed"},
		{"bad-local", []byte{0xfb, 1, 0, 0x21, 9, 0x0b}, "malformed"},
		{"no-gc", []byte{0x41, 42, 0x0b}, "no-gc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			copyM := *m
			copyM.Code = append([]wasm.Func(nil), m.Code...)
			copyM.Code[0].BodyBytes = tc.body
			got := analyzeGCLifetime(&copyM, 0, 256)
			if got.Reason != tc.reason {
				t.Fatalf("%+v", got)
			}
			if !got.Candidate && got.Reason != "no-gc" && got.DenominatorKnown {
				t.Fatal("partial denominator reported complete")
			}
		})
	}
}

func TestGCLifetimeBoundsAndTypes(t *testing.T) {
	fresh := func() *wasm.Module { return lifetimeFixture(t, "qualifying", 1) }
	m := fresh()
	if r := analyzeGCLifetime(m, 0, 1); r.Reason != "record-budget" {
		t.Fatal(r)
	}
	m = fresh()
	m.Code[0].BodyBytes = bytes.Repeat([]byte{1}, 4097)
	if r := analyzeGCLifetime(m, 0, 256); r.Reason != "body-budget" {
		t.Fatal(r)
	}
	m = fresh()
	m.Code[0].Locals.Runs[0].Count = 65
	if r := analyzeGCLifetime(m, 0, 256); r.Reason != "local-budget" {
		t.Fatal(r)
	}
	m = fresh()
	m.Code[0].Locals.Runs[0].Count = 64
	if r := analyzeGCLifetime(m, 0, 256); !r.Candidate {
		t.Fatal(r)
	}
	m = fresh()
	m.Types[0].SubTypes[0].Comp.Fields[0] = wasm.NewFieldType(wasm.StorageVal(wasm.AnyRef), wasm.Const)
	if r := analyzeGCLifetime(m, 0, 256); r.Reason != "layout" {
		t.Fatal(r)
	}
	m = fresh()
	m.Types[1].SubTypes[0].Comp.Results = []wasm.ValType{wasm.AnyRef}
	if r := analyzeGCLifetime(m, 0, 256); r.Reason != "object-result" {
		t.Fatal(r)
	}
	m = fresh()
	m.Types[1].SubTypes[0].Comp.Params = []wasm.ValType{wasm.I32}
	m.Code[0].BodyBytes = []byte{0x20, 0, 0xfb, 0, 0, 0x1a, 0x41, 7, 0x0b}
	if r := analyzeGCLifetime(m, 0, 256); !r.Candidate {
		t.Fatal(r)
	}
	m.Code[0].BodyBytes = []byte{0x41, 1, 0x21, 0, 0x20, 0, 0xfb, 0, 0, 0x1a, 0x41, 7, 0x0b}
	if r := analyzeGCLifetime(m, 0, 256); r.Reason != "parameter-write" {
		t.Fatal(r)
	}
	m = fresh()
	m.Code = make([]wasm.Func, 4097)
	r := analyzeGCLifetimes(m)
	if len(r.Functions) != 4096 || r.Omitted != 1 {
		t.Fatal("module budget")
	}
}

func lifetimeSelection(on bool) map[string]bool {
	s := map[string]bool{}
	for _, k := range OptKnobs() {
		s[k.Name] = k.On
	}
	s["gc-lifetime-analyze"] = on
	return s
}

func TestGCLifetimeCompileIdentity(t *testing.T) {
	old := runtime.GOMAXPROCS(8)
	defer runtime.GOMAXPROCS(old)
	for _, kind := range []string{"qualifying", "nonqualifying", "no-gc"} {
		t.Run(kind, func(t *testing.T) {
			m := lifetimeFixture(t, kind, 8)
			var oracleCode, oracleOn []byte
			var oracleRoots *shared.GCModuleFrameRootPlan
			var oracleReport *GCLifetimeReport
			for _, workers := range []int{1, 4, 8} {
				for _, on := range []bool{false, true} {
					plans := make([]*shared.GCFrameRootPlan, len(m.Code))
					for i := range plans {
						plans[i] = &shared.GCFrameRootPlan{Candidate: true, Exact: true, Locals: []shared.GCFrameLocal{{Index: 0, Offset: 16}, {Index: 1, Offset: 24}}}
					}
					roots := testGCModuleRootPlansAMD64(t, plans...)
					report := &GCLifetimeReport{}
					var stats ModuleStats
					opts := CompileOptions{Optimizations: lifetimeSelection(on), Workers: workers, GCStructHelpers: true, GCFrameRoots: roots, DeferCodeMapping: true, GCLifetimeReport: &report}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					cm, err := CompileModuleWith(m, opts)
					if err != nil {
						t.Fatal(err)
					}
					if cm.CodeImage != nil {
						defer cm.CodeImage.Close()
					}
					if oracleCode == nil {
						oracleCode = append([]byte(nil), cm.Code...)
						oracleRoots = roots
					} else if (bytes.Equal(oracleCode, cm.Code) == (on && kind == "qualifying" && gc.NativeDeadBatchSupported && !profileEnabled)) || !reflect.DeepEqual(oracleRoots, roots) {
						t.Fatalf("code/root mismatch workers=%d on=%t", workers, on)
					}
					if on {
						if oracleOn == nil {
							oracleOn = append([]byte(nil), cm.Code...)
						} else if !bytes.Equal(oracleOn, cm.Code) {
							t.Fatal("nondeterministic On code")
						}
					}
					if !on {
						if report != nil || stats.GCLifetime != nil {
							t.Fatal("Off did not bypass")
						}
					} else {
						if report == nil || len(report.Functions) != 8 {
							t.Fatal("missing report")
						}
						if oracleReport == nil {
							oracleReport = report
						} else if !reflect.DeepEqual(oracleReport, report) {
							t.Fatal("nondeterministic report")
						}
						if diagnosticsEnabled && !strings.Contains(stats.String(), "checked scalar replacement") {
							t.Fatal("missing report evidence")
						}
					}
				}
			}
		})
	}
}

func TestGCLifetimeExactBudgets(t *testing.T) {
	m := lifetimeFixture(t, "qualifying", 1)
	// nop padding counts as work, including end. Exactly 256 records admits.
	body := append(bytes.Repeat([]byte{1}, 252), 0xfb, 1, 0, 0x1a, 0x41, 1, 0x0b)
	m.Code[0].BodyBytes = body
	if r := analyzeGCLifetime(m, 0, 256); !r.Candidate || r.Records != 256 {
		t.Fatal(r)
	}
	m.Code[0].BodyBytes = append([]byte{1}, body...)
	if r := analyzeGCLifetime(m, 0, 256); r.Reason != "record-budget" {
		t.Fatal(r)
	}
	m.Code[0].BodyBytes = append(bytes.Repeat([]byte{0x41, 1}, 64), 0xfb, 1, 0, 0x0b)
	if r := analyzeGCLifetime(m, 0, 256); r.Reason != "stack-budget" {
		t.Fatal(r)
	}
	m.Code[0].Locals.Runs = make([]wasm.LocalRun, 65)
	if r := analyzeGCLifetime(m, 0, 256); r.Reason != "local-budget" {
		t.Fatal(r)
	}
	m = lifetimeFixture(t, "qualifying", 1)
	m.Code[0].BodyBytes = []byte{0x20, 0, 0xfb, 2, 0, 0, 0x0b}
	if r := analyzeGCLifetime(m, 0, 256); r.Reason != "unknown-object" {
		t.Fatal(r)
	}
	m.Types[0].SubTypes[0].Comp.Fields = make([]wasm.FieldType, 33)
	if r := analyzeGCLifetime(m, 0, 256); r.Reason != "layout" {
		t.Fatal(r)
	}
}

func TestGCLifetimeNumericTypes(t *testing.T) {
	for _, tc := range []struct {
		typ  wasm.ValType
		init []byte
	}{
		{wasm.I32, []byte{0x41, 1}}, {wasm.I64, []byte{0x42, 1}},
		{wasm.F32, []byte{0x43, 0, 0, 0, 0}}, {wasm.F64, []byte{0x44, 0, 0, 0, 0, 0, 0, 0, 0}},
	} {
		t.Run(tc.typ.String(), func(t *testing.T) {
			m := lifetimeFixture(t, "qualifying", 1)
			m.Types[0].SubTypes[0].Comp.Fields[0] = wasm.NewFieldType(wasm.StorageVal(tc.typ), wasm.Const)
			m.Types[1].SubTypes[0].Comp.Results = []wasm.ValType{tc.typ}
			m.Code[0].BodyBytes = append(append([]byte(nil), tc.init...), 0xfb, 0, 0, 0xfb, 2, 0, 0, 0x0b)
			if r := analyzeGCLifetime(m, 0, 256); !r.Candidate {
				t.Fatal(r)
			}
		})
	}
}

func TestGCLifetimeIdentityAndInitializer(t *testing.T) {
	m := lifetimeFixture(t, "qualifying", 1)
	first := analyzeGCLifetime(m, 0, 256)
	m.Code[0].BodyBytes = append([]byte{1}, m.Code[0].BodyBytes...)
	second := analyzeGCLifetime(m, 0, 256)
	if !second.Candidate || first.Identity == second.Identity || second.ConstructorOffset != first.ConstructorOffset+1 {
		t.Fatal("identity/offset did not track complete body")
	}
	m.Code[0].Locals.Runs = []wasm.LocalRun{{Count: 1, Type: wasm.I32}}
	m.Code[0].BodyBytes = []byte{0x20, 0, 0xfb, 0, 0, 0x1a, 0x41, 1, 0x0b}
	if r := analyzeGCLifetime(m, 0, 256); r.Reason != "initializer" {
		t.Fatal(r)
	}
	// B1's byte gate is allowed false positives inside immediates; interpretation
	// must not mistake that byte for a constructor.
	m.Code[0].BodyBytes = []byte{0x41, 0xfb, 0, 0x0b}
	if r := analyzeGCLifetime(m, 0, 256); r.Reason != "no-constructor" || !r.DenominatorKnown {
		t.Fatal(r)
	}
}

func TestGCLifetimeReplay(t *testing.T) {
	if !gc.NativeDeadBatchSupported || profileEnabled {
		t.Skip("consumer profile fallback")
	}
	m := lifetimeFixture(t, "qualifying", 1)
	selection, err := optimizationBindings.ResolveSnapshot(lifetimeSelection(true), OptimizationSnapshot{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ft, _ := m.LocalFuncType(0)
	f := &fn{m: m, ft: ft, gcStructHelpers: true, policy: shared.DefaultCodegenPolicy(selection)}
	mutations := []func(*gcLifetimePlan){
		func(p *gcLifetimePlan) { p.context.bodyHash++ }, func(p *gcLifetimePlan) { p.context.fact.layout.objectSize++ },
		func(p *gcLifetimePlan) { p.records[0].Offset++ }, func(p *gcLifetimePlan) { p.records[len(p.records)-1].Descriptor++ },
		func(p *gcLifetimePlan) { p.policy.MaxMachineWindow++ }, func(p *gcLifetimePlan) { p.typ++ },
	}
	for _, mutate := range mutations {
		p := f.sealGCLifetime(m.Code[0].BodyBytes)
		if p == nil || !f.replayGCLifetime(p, m.Code[0].BodyBytes) {
			t.Fatal("missing sealed replay")
		}
		var originalFact, savedFact *gcLifetimeTypeFact
		if p.context.fact != nil {
			originalFact = p.context.fact
			copyFact := *p.context.fact
			savedFact = &copyFact
		}
		mutate(p)
		if f.replayGCLifetime(p, m.Code[0].BodyBytes) {
			t.Fatal("accepted mismatched certificate")
		}
		if consumed, err := f.consumeGCLifetime(p, m.Code[0].BodyBytes); consumed || err != nil || f.a != nil {
			t.Fatal("mismatch did not cleanly fall back before emission")
		}
		if savedFact != nil {
			*originalFact = *savedFact
		}
	}
	// Diagnostic hashes do not occur in certificates and cannot authorize a replay.
	p := f.sealGCLifetime(m.Code[0].BodyBytes)
	if p == nil {
		t.Fatal("seal before body mutation")
	}
	m.Code[0].BodyBytes = append([]byte{1}, m.Code[0].BodyBytes...)
	if f.replayGCLifetime(p, m.Code[0].BodyBytes) {
		t.Fatal("accepted changed complete body")
	}
	f.sc.releaseGCLifetimePlan(p)
}

func TestGCLifetimeReplayContextFallback(t *testing.T) {
	if !gc.NativeDeadBatchSupported || profileEnabled {
		t.Skip("consumer profile fallback")
	}
	for _, what := range []string{"type", "locals", "signature", "options", "helpers", "eh", "inline", "custom"} {
		t.Run(what, func(t *testing.T) {
			m := lifetimeFixture(t, "qualifying", 1)
			ft, _ := m.LocalFuncType(0)
			selection, _ := optimizationBindings.ResolveSnapshot(lifetimeSelection(true), OptimizationSnapshot{}, nil)
			f := &fn{m: m, ft: ft, gcStructHelpers: true, policy: shared.DefaultCodegenPolicy(selection)}
			p := f.sealGCLifetime(m.Code[0].BodyBytes)
			if p == nil {
				t.Fatal("seal")
			}
			switch what {
			case "type":
				m.Types[0].SubTypes[0].Comp.Fields[0] = wasm.NewFieldType(wasm.StorageVal(wasm.I64), wasm.Const)
			case "locals":
				m.Code[0].Locals.Runs[0].Count++
			case "signature":
				ft.Params = append(ft.Params, wasm.I32)
			case "options":
				f.policy.MaxMachineWindow++
			case "helpers":
				f.gcStructHelpers = false
			case "eh":
				f.moduleEH = true
			case "inline":
				f.inlineDepth = 1
			case "custom":
				f.customInstructions = map[uint32]CustomInstruction{0: {}}
			}
			if f.replayGCLifetime(p, m.Code[0].BodyBytes) {
				t.Fatal("replay accepted changed context")
			}
		})
	}
}

func TestGCLifetimeCompactRecordsAndScratchLifecycle(t *testing.T) {
	if !gc.NativeDeadBatchSupported || profileEnabled {
		t.Skip("consumer profile fallback")
	}
	if got := unsafe.Sizeof(gcLifetimeRewrite{}); got != 8 {
		t.Fatalf("rewrite record size = %d, want 8", got)
	}
	m := lifetimeFixture(t, "qualifying", 1)
	ft, _ := m.LocalFuncType(0)
	selection, _ := optimizationBindings.ResolveSnapshot(lifetimeSelection(true), OptimizationSnapshot{}, nil)
	sc := &scratch{}
	f := &fn{m: m, ft: ft, gcStructHelpers: true, policy: shared.DefaultCodegenPolicy(selection), sc: sc}
	if sc.gcLifetimeFacts != nil || sc.gcLifetimePlan != nil || sc.gcLifetimePlanLive {
		t.Fatal("unused serial scratch eagerly materialized lifetime storage")
	}
	p := f.sealGCLifetime(m.Code[0].BodyBytes)
	proof := analyzeGCLifetimeWithRecords(m, 0, gcLifetimeMaxRecords, nil, false)
	if p == nil || sc.gcLifetimeFacts == nil || len(p.records) != proof.Rewrites || !sc.gcLifetimeRecordsLive {
		t.Fatalf("plan=%v records=%d rewrites=%d live=%t", p != nil, len(p.records), proof.Rewrites, sc.gcLifetimeRecordsLive)
	}
	for i := 1; i < len(p.records); i++ {
		if p.records[i-1].Offset >= p.records[i].Offset {
			t.Fatal("records not in strict source order")
		}
	}
	if nested := f.sealGCLifetime(m.Code[0].BodyBytes); nested != nil || !f.replayGCLifetime(p, m.Code[0].BodyBytes) {
		t.Fatal("nested admission reused or damaged the live worker slot")
	}
	retainedPlan := p
	retained := cap(sc.gcLifetimeRecords)
	sc.releaseGCLifetimePlan(p)
	if sc.gcLifetimePlanLive || sc.gcLifetimeRecordsLive || len(sc.gcLifetimeRecords) != 0 ||
		p.body != nil || p.context.fact != nil || p.records != nil || p.typ != 0 {
		t.Fatal("release did not reset worker scratch")
	}
	p = f.sealGCLifetime(m.Code[0].BodyBytes)
	if p == nil || p != retainedPlan || cap(sc.gcLifetimeRecords) != retained {
		t.Fatal("retry did not reuse retained rewrite storage")
	}
	// A failed attempt releases its lease and a following retry may reuse it.
	p.records[0].Descriptor++
	if consumed, err := f.consumeGCLifetime(p, m.Code[0].BodyBytes); consumed || err != nil || sc.gcLifetimeRecordsLive {
		t.Fatalf("corrupt retry consumed=%t err=%v live=%t", consumed, err, sc.gcLifetimeRecordsLive)
	}
	if p = f.sealGCLifetime(m.Code[0].BodyBytes); p == nil || !f.replayGCLifetime(p, m.Code[0].BodyBytes) {
		t.Fatal("clean retry after corruption failed")
	}
	sc.releaseGCLifetimePlan(p)
	// The same worker slot admits bounded record growth, then shrinks without
	// changing plan identity or retaining pointer-bearing state from either use.
	smallBody := append([]byte(nil), m.Code[0].BodyBytes...)
	largeBody := []byte{0x41, 42, 0xfb, 0, 0, 0x21, 0}
	for range 60 {
		largeBody = append(largeBody, 0x20, 0, 0x1a)
	}
	m.Code[0].BodyBytes = append(largeBody, 0x20, 0, 0xfb, 2, 0, 0, 0x0b)
	p = f.sealGCLifetime(m.Code[0].BodyBytes)
	if p == nil || p != retainedPlan || len(p.records) <= retained || cap(sc.gcLifetimeRecords) > gcLifetimeMaxRecords {
		t.Fatal("bounded record growth did not reuse the worker plan")
	}
	sc.releaseGCLifetimePlan(p)
	m.Code[0].BodyBytes = smallBody
	p = f.sealGCLifetime(m.Code[0].BodyBytes)
	if p == nil || p != retainedPlan || len(p.records) >= cap(sc.gcLifetimeRecords) {
		t.Fatal("small retry did not reuse grown worker storage")
	}
	sc.releaseGCLifetimePlan(p)
	// Rejection/partial candidate failure leaves no live record bank.
	m.Code[0].BodyBytes = []byte{0xfb, 1, 0, 0xfb, 1, 0, 0x0b}
	if p = f.sealGCLifetime(m.Code[0].BodyBytes); p != nil || sc.gcLifetimeRecordsLive {
		t.Fatal("rejected candidate retained worker records")
	}
}

func TestGCLifetimeFactsLazySerialLifecycle(t *testing.T) {
	if !gc.NativeDeadBatchSupported || profileEnabled {
		t.Skip("consumer profile fallback")
	}
	selection, err := optimizationBindings.ResolveSnapshot(lifetimeSelection(true), OptimizationSnapshot{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	policy := shared.DefaultCodegenPolicy(selection)
	newFn := func(m *wasm.Module, function int, sc *scratch) *fn {
		ft, _ := m.LocalFuncType(function)
		return &fn{m: m, ft: ft, globalIdx: function, gcStructHelpers: true, policy: policy, sc: sc}
	}

	for _, kind := range []string{"no-gc", "nonqualifying"} {
		t.Run(kind, func(t *testing.T) {
			m := lifetimeFixture(t, kind, 1)
			sc := &scratch{}
			if p := newFn(m, 0, sc).sealGCLifetime(m.Code[0].BodyBytes); p != nil || sc.gcLifetimeFacts != nil {
				t.Fatalf("rejected serial function created facts: plan=%v facts=%p", p != nil, sc.gcLifetimeFacts)
			}
		})
	}

	m := lifetimeFixture(t, "qualifying", 2)
	sc := &scratch{}
	first := newFn(m, 0, sc).sealGCLifetime(m.Code[0].BodyBytes)
	if first == nil || sc.gcLifetimeFacts == nil {
		t.Fatal("accepted serial function did not lazily create facts")
	}
	facts := sc.gcLifetimeFacts
	sc.releaseGCLifetimePlan(first)
	second := newFn(m, 1, sc).sealGCLifetime(m.Code[1].BodyBytes)
	if second == nil || sc.gcLifetimeFacts != facts || second.context.fact != facts.types[0] {
		t.Fatal("accepted serial functions did not share one module cache")
	}
	sc.releaseGCLifetimePlan(second)

	other := lifetimeFixture(t, "qualifying", 1)
	otherScratch := &scratch{}
	otherPlan := newFn(other, 0, otherScratch).sealGCLifetime(other.Code[0].BodyBytes)
	if otherPlan == nil || otherScratch.gcLifetimeFacts == nil || otherScratch.gcLifetimeFacts == facts {
		t.Fatal("repeated compile scratch reused facts across module identity")
	}
	otherScratch.releaseGCLifetimePlan(otherPlan)
}

func TestGCLifetimeFactsReuseFallbackLayouts(t *testing.T) {
	m := gcLayoutTestModule(2, 3)
	selection, err := optimizationBindings.ResolveSnapshot(lifetimeSelection(true), OptimizationSnapshot{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	facts := newGCLifetimeModuleFacts()
	newFn := func() *fn {
		return &fn{m: m, policy: shared.DefaultCodegenPolicy(selection), sc: &scratch{gcLifetimeFacts: facts}}
	}

	// Simulate independent workers admitting two referenced types. Publication
	// must converge on one immutable fact and one field slice per type.
	const workers = 8
	type result struct {
		typeIndex uint32
		fact      *gcLifetimeTypeFact
	}
	got := make(chan result, workers*2)
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		for typeIndex := uint32(0); typeIndex < 2; typeIndex++ {
			wg.Add(1)
			go func(typeIndex uint32) {
				defer wg.Done()
				fact, ok := facts.typeFact(newFn(), typeIndex)
				if !ok {
					t.Errorf("type %d was not admitted", typeIndex)
					return
				}
				got <- result{typeIndex: typeIndex, fact: fact}
			}(typeIndex)
		}
	}
	wg.Wait()
	close(got)
	var published [2]*gcLifetimeTypeFact
	for result := range got {
		if published[result.typeIndex] == nil {
			published[result.typeIndex] = result.fact
		} else if published[result.typeIndex] != result.fact {
			t.Fatalf("type %d published multiple facts", result.typeIndex)
		}
	}
	if len(facts.types) != 2 || published[0] == nil || published[1] == nil || published[0] == published[1] {
		t.Fatalf("published facts = %#v, cache size = %d", published, len(facts.types))
	}

	// Baseline/candidate/helper retries reuse the published field storage.
	f := newFn()
	for typeIndex := uint32(0); typeIndex < 2; typeIndex++ {
		for retry := 0; retry < 4; retry++ {
			layout, ok := f.nativeGCStructAllocLayout(typeIndex)
			if !ok || len(layout.fields) == 0 || &layout.fields[0] != &facts.types[typeIndex].layout.fields[0] {
				t.Fatalf("type %d retry %d did not reuse published layout", typeIndex, retry)
			}
		}
	}

	// Unsupported indices and a changed module never acquire authority from a
	// cached plan. Restoring the immutable source makes the original fact usable.
	if _, ok := facts.typeFact(f, 2); ok || len(facts.types) != 2 {
		t.Fatal("unsupported type was cached")
	}
	otherModule := gcLayoutTestModule(2, 3)
	other := &fn{m: otherModule, policy: f.policy, sc: &scratch{gcLifetimeFacts: facts}}
	if _, ok := facts.typeFact(other, 0); ok {
		t.Fatal("fact crossed module identity")
	}
	disabledSelection := lifetimeSelection(true)
	disabledSelection["gc-native-alloc"] = false
	disabledSnapshot, err := optimizationBindings.ResolveSnapshot(disabledSelection, OptimizationSnapshot{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	disabled := &fn{m: m, policy: shared.DefaultCodegenPolicy(disabledSnapshot), sc: &scratch{gcLifetimeFacts: newGCLifetimeModuleFacts()}}
	if _, ok := disabled.sc.gcLifetimeFacts.typeFact(disabled, 0); ok {
		t.Fatal("disabled native allocation populated a fact")
	}
	saved := m.Types[0].SubTypes[0].Comp.Fields[0]
	m.Types[0].SubTypes[0].Comp.Fields[0] = wasm.NewFieldType(wasm.StorageVal(wasm.I32), wasm.Const)
	if _, ok := facts.typeFact(f, 0); ok {
		t.Fatal("changed type reused cached layout")
	}
	m.Types[0].SubTypes[0].Comp.Fields[0] = saved
	if fact, ok := facts.typeFact(f, 0); !ok || fact != published[0] {
		t.Fatal("restored type did not reuse original fact")
	}
}

func TestGCLifetimeCorruptAndReorderedRecords(t *testing.T) {
	if !gc.NativeDeadBatchSupported || profileEnabled {
		t.Skip("consumer profile fallback")
	}
	m := lifetimeFixture(t, "qualifying", 1)
	ft, _ := m.LocalFuncType(0)
	selection, _ := optimizationBindings.ResolveSnapshot(lifetimeSelection(true), OptimizationSnapshot{}, nil)
	newPlan := func() (*fn, *gcLifetimePlan) {
		f := &fn{m: m, ft: ft, gcStructHelpers: true, policy: shared.DefaultCodegenPolicy(selection), sc: &scratch{gcLifetimeFacts: newGCLifetimeModuleFacts()}}
		return f, f.sealGCLifetime(m.Code[0].BodyBytes)
	}
	for _, mutate := range []func([]gcLifetimeRewrite){
		func(records []gcLifetimeRewrite) { records[0].Offset++ },
		func(records []gcLifetimeRewrite) { records[0].Descriptor++ },
		func(records []gcLifetimeRewrite) {
			records[0], records[len(records)-1] = records[len(records)-1], records[0]
		},
	} {
		f, p := newPlan()
		if p == nil || len(p.records) < 2 {
			t.Fatal("missing multi-record plan")
		}
		mutate(p.records)
		if f.replayGCLifetime(p, m.Code[0].BodyBytes) {
			t.Fatal("accepted corrupted/reordered compact records")
		}
		f.sc.releaseGCLifetimePlan(p)
	}
}
