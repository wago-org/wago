//go:build wago_regalloccheck

package codegen

import (
	"github.com/wago-org/wago/internal/regalloccheck"
	"reflect"
	"sync"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func validatedContextModule(t *testing.T) (*wasm.Module, *wasm.ValidatedModuleAnalysis) {
	t.Helper()
	m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompFunc}}}}}, FuncTypes: []wasm.TypeIdx{{}}, Code: []wasm.Func{{BodyBytes: []byte{0x0b}}}}
	a := new(wasm.ValidatedModuleAnalysis)
	if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, a); err != nil {
		t.Fatal(err)
	}
	return m, a
}

func TestSourceReporterJoinedOrderingAndClearedFacts(t *testing.T) {
	m, a := validatedContextModule(t)
	for i := 1; i < 8; i++ {
		m.FuncTypes = append(m.FuncTypes, m.FuncTypes[0])
		m.Code = append(m.Code, m.Code[0])
	}
	if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, a); err != nil {
		t.Fatal(err)
	}
	opts := SourceOptions(Options{}, m, a, wasm.ValidationFeatures{})
	ctx := SourceContextFor(opts, m)
	var reports []SourceReport
	if !SetSourceReporter(opts, m, func(r SourceReport) {
		if ctx.module != nil || ctx.analysis != nil || ctx.reporter != nil || ctx.reports != nil {
			t.Fatal("report delivered before fact release")
		}
		reports = append(reports, r)
	}) {
		t.Fatal("collector unavailable")
	}
	var wg sync.WaitGroup
	for i := 0; i < 7; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			RecordSourceResult(ctx, index, regalloccheck.Result{Verdict: regalloccheck.Verified, Work: index})
		}(i)
	}
	wg.Wait()
	if len(reports) != 0 {
		t.Fatal("reports delivered before join/close")
	}
	CloseSourceContext(opts, m)
	CloseSourceContext(opts, m)
	if len(reports) != 8 || !a.ValidFor(m) {
		t.Fatal("missing reports or changed analysis")
	}
	for i, r := range reports {
		if r.LocalFunction != i {
			t.Fatal("report order changed")
		}
		if i < 7 && (r.Result.Verdict != regalloccheck.Verified || r.Result.Work != i) {
			t.Fatal("concurrent report lost")
		}
	}
	if reports[7].Result.Verdict != regalloccheck.Inconclusive || reports[7].Result.Reason != regalloccheck.UnsupportedOperation {
		t.Fatal("omitted function became verified")
	}
	RecordSourceResult(ctx, 0, regalloccheck.Result{Verdict: regalloccheck.Verified})
	if len(reports) != 8 {
		t.Fatal("retired collector reused")
	}
}

func TestSourceReporterRefusesUnboundedCollector(t *testing.T) {
	m, a := validatedContextModule(t)
	for i := 1; i < 4097; i++ {
		m.FuncTypes = append(m.FuncTypes, m.FuncTypes[0])
		m.Code = append(m.Code, m.Code[0])
	}
	if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, a); err != nil {
		t.Fatal(err)
	}
	opts := SourceOptions(Options{}, m, a, wasm.ValidationFeatures{})
	ctx := SourceContextFor(opts, m)
	if SetSourceReporter(opts, m, func(SourceReport) { t.Fatal("oversized report") }) || ctx.reporter != nil || ctx.reports != nil {
		t.Fatal("oversized collector allocated")
	}
	CloseSourceContext(opts, m)
}

func TestSourceContextExactProfileAndOptionsCopy(t *testing.T) {
	m, a := validatedContextModule(t)
	profile := wasm.ValidationFeatures{MultiMemory: true, CompactImports: true, ExtendedConstGlobals: true, GCConstExpr: true}
	if err := wasm.ValidateModuleWithAnalysis(m, profile, 1, wasm.ValidationLimits{}, a); err != nil {
		t.Fatal(err)
	}
	var opts Options
	if !AttachSourceContext(&opts, m, a, profile) {
		t.Fatal("context unavailable")
	}
	copy := opts
	profile = wasm.ValidationFeatures{} // Later execution-feature narrowing.
	ctx := SourceContextFor(copy, m)
	got, features, ok := ValidatedSourceContext(ctx, m)
	if !ok || got != a || features == profile || !features.GCConstExpr || !features.ExtendedConstGlobals || !features.MultiMemory || !features.CompactImports || ctx != SourceContextFor(opts, m) {
		t.Fatal("original validation context was not copied/shared immutably")
	}
	if SourceContextFor(copy, &wasm.Module{}) != nil || SourceContextFor(copy, nil) != nil {
		t.Fatal("foreign module admitted")
	}
	// Invalidating the borrowed analysis must invalidate every copied option.
	*a = wasm.ValidatedModuleAnalysis{}
	if SourceContextFor(copy, m) != nil || SourceContextFor(opts, m) != nil {
		t.Fatal("stale analysis admitted")
	}
}

func TestSourceContextMissingAndFailedAttachment(t *testing.T) {
	m, a := validatedContextModule(t)
	var opts Options
	if SourceContextFor(opts, m) != nil || AttachSourceContext(nil, m, a, wasm.ValidationFeatures{}) || AttachSourceContext(&opts, nil, a, wasm.ValidationFeatures{}) || AttachSourceContext(&opts, m, nil, wasm.ValidationFeatures{}) {
		t.Fatal("missing context admitted")
	}
	if !AttachSourceContext(&opts, m, a, wasm.ValidationFeatures{}) {
		t.Fatal("valid context unavailable")
	}
	if AttachSourceContext(&opts, &wasm.Module{}, a, wasm.ValidationFeatures{}) || SourceContextFor(opts, m) != nil {
		t.Fatal("failed attachment retained old context")
	}
}

func TestSourceContextCloseReleasesCopiesAndStaleFacts(t *testing.T) {
	CloseSourceContext(Options{}, nil)
	for _, stale := range []bool{false, true} {
		m, a := validatedContextModule(t)
		opts := SourceOptions(Options{}, m, a, wasm.ValidationFeatures{MultiMemory: true})
		copy := opts
		ctx := SourceContextFor(opts, m)
		CloseSourceContext(copy, &wasm.Module{})
		if SourceContextFor(opts, m) != ctx {
			t.Fatal("foreign close retired context")
		}
		if stale {
			*a = wasm.ValidatedModuleAnalysis{}
		}
		before := *a
		CloseSourceContext(copy, m)
		CloseSourceContext(opts, m)
		if ctx.module != nil || ctx.analysis != nil || ctx.features != (wasm.ValidationFeatures{}) || SourceContextFor(opts, m) != nil || SourceContextFor(copy, m) != nil {
			t.Fatal("close retained borrowed facts")
		}
		if !reflect.DeepEqual(*a, before) || len(m.Code) != 1 {
			t.Fatal("close changed borrowed storage")
		}
	}
}
