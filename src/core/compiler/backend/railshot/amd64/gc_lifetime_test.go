//go:build amd64

package amd64

import (
	"bytes"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestGCLifetimeProof(t *testing.T) {
	m := lifetimeFixture(t, "qualifying", 1)
	r := analyzeGCLifetime(m, 0, gcLifetimeMaxRecords)
	if !r.Candidate || !r.DenominatorKnown || r.Sites != 1 || r.Reads != 1 || r.Aliases != 2 || r.Identity == ([32]byte{}) {
		t.Fatalf("%+v", r)
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
	p := f.sealGCLifetime(m.Code[0].BodyBytes)
	if p == nil || !f.replayGCLifetime(p, m.Code[0].BodyBytes) {
		t.Fatal("missing sealed replay")
	}
	for _, mutate := range []func(){
		func() { p.body[1]++ }, func() { p.context.layout.objectSize++ }, func() { p.records[0].pc++ },
		func() { p.records[len(p.records)-1].end++ }, func() { p.policy.MaxMachineWindow++ }, func() { p.typ++ },
	} {
		saved := *p
		saved.body = append([]byte(nil), p.body...)
		saved.records = append([]gcLifetimeRecord(nil), p.records...)
		mutate()
		if f.replayGCLifetime(p, m.Code[0].BodyBytes) {
			t.Fatal("accepted mismatched certificate")
		}
		*p = saved
	}
	// Diagnostic hashes do not occur in certificates and cannot authorize a replay.
	m.Code[0].BodyBytes = append([]byte{1}, m.Code[0].BodyBytes...)
	if f.replayGCLifetime(p, m.Code[0].BodyBytes) {
		t.Fatal("accepted changed complete body")
	}
}

func TestGCLifetimeReplayContextFallback(t *testing.T) {
	if !gc.NativeDeadBatchSupported || profileEnabled {
		t.Skip("consumer profile fallback")
	}
	for _, what := range []string{"type", "locals", "signature", "options"} {
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
			}
			if f.replayGCLifetime(p, m.Code[0].BodyBytes) {
				t.Fatal("replay accepted changed context")
			}
		})
	}
}
