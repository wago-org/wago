//go:build wago_regalloccheck

package codegen

import (
	"reflect"
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
