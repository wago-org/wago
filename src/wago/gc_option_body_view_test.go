//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func optionViewBinary(t *testing.T, source []byte, m *wasm.Module) []byte {
	t.Helper()
	code := gcOptionULEB(uint32(len(m.Code)))
	for _, f := range m.Code {
		body := gcOptionULEB(uint32(len(f.Locals.Runs)))
		for _, run := range f.Locals.Runs {
			body = append(body, gcOptionULEB(run.Count)...)
			v, ok := gcOptionEncodeLocal(run.Type)
			if !ok {
				t.Fatal("unsupported test local")
			}
			body = append(body, v...)
		}
		body = append(body, f.BodyBytes...)
		code = append(code, gcOptionULEB(uint32(len(body)))...)
		code = append(code, body...)
	}
	out := append([]byte(nil), source[:8]...)
	r := wasm.NewReader(source)
	for i := 0; i < 8; i++ {
		if _, e := r.Byte(); e != nil {
			t.Fatal(e)
		}
	}
	for r.HasNext() {
		start := r.Offset()
		id, e := r.Byte()
		if e != nil {
			t.Fatal(e)
		}
		n, e := r.U32()
		if e != nil {
			t.Fatal(e)
		}
		end := r.Offset() + int(n)
		if end > len(source) {
			t.Fatal("truncated source")
		}
		if id == 10 {
			out = append(out, 10)
			out = append(out, gcOptionULEB(uint32(len(code)))...)
			out = append(out, code...)
		} else {
			out = append(out, source[start:end]...)
		}
		for r.Offset() < end {
			if _, e := r.Byte(); e != nil {
				t.Fatal(e)
			}
		}
	}
	return out
}
func TestGCOptionViewFixture(t *testing.T) {
	for _, shift := range []uint32{0, 130} {
		t.Run(fmt.Sprint(shift), func(t *testing.T) {
			source, some := iteratorCarrierFixture(16, shift, 3)
			m, e := wasm.DecodeModule(source)
			if e != nil {
				t.Fatal(e)
			}
			features := wasm.ValidationFeatures{CompactImports: true, MultiMemory: true, ExtendedConstGlobals: true, GCConstExpr: true}
			var original wasm.ValidatedModuleAnalysis
			if e = wasm.ValidateModuleWithAnalysis(m, features, 1, wasm.ValidationLimits{}, &original); e != nil {
				t.Fatal(e)
			}
			hashes := make([][32]byte, len(m.Code))
			for i, c := range m.Code {
				hashes[i] = sha256.Sum256(c.BodyBytes)
			}
			view, e := buildGCOptionBodyView(m)
			if e != nil {
				t.Fatal(e)
			}
			if view == nil || view.Families != 1 || len(view.Constructors) != 4 {
				t.Fatalf("view%+v want1family/4constructors", view)
			}
			if view.Module == m || original.ValidFor(view.Module) {
				t.Fatal("private module reused original analysis identity")
			}
			for i, c := range m.Code {
				if sha256.Sum256(c.BodyBytes) != hashes[i] {
					t.Fatal("original body changed")
				}
			}
			var fresh wasm.ValidatedModuleAnalysis
			if e = wasm.ValidateModuleWithAnalysis(view.Module, features, 1, wasm.ValidationLimits{}, &fresh); e != nil {
				t.Fatal(e)
			}
			if !fresh.ValidFor(view.Module) {
				t.Fatal("private validation not owned by view")
			}
			for _, cert := range view.Constructors {
				f := view.Module.Code[int(cert.Function)-view.Module.ImportedFuncCount()]
				if sha256.Sum256(f.BodyBytes) != cert.BodySHA256 {
					t.Fatal("stale constructor certificate")
				}
			}
			binary := optionViewBinary(t, source, view.Module)
			if bytes.Equal(binary, source) {
				t.Fatal("body not rewritten")
			}
			rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompileWorkers(1)))
			defer rt.Close()
			module, e := rt.Compile(binary)
			if e != nil {
				t.Fatal(e)
			}
			defer module.Close()
			in, e := rt.Instantiate(context.Background(), module)
			if e != nil {
				t.Fatal(e)
			}
			defer in.Close()
			roots := in.c.GCNativeRootAdmission()
			if !roots.Required || !roots.Exact {
				t.Fatalf("private roots%+v", roots)
			}
			before := in.gc.Stats()
			got, e := in.Invoke("run")
			if e != nil || len(got) != 1 || got[0] != 120 {
				t.Fatalf("private sum%v err%v", got, e)
			}
			idx, e := in.Invoke("index")
			if e != nil || len(idx) != 1 || idx[0] != 16 {
				t.Fatalf("private index%v err%v", idx, e)
			}
			after := in.gc.Stats()
			if after.Allocations-before.Allocations != 17 {
				t.Fatalf("logical%d want17", after.Allocations-before.Allocations)
			}
			if count := iteratorPublishedType(t, in, some); count != 16 {
				t.Fatalf("constructor unexpectedly removed: Some%d want16", count)
			}
			t.Logf("families%d growth%d certs%d roots%+v", view.Families, view.Growth, len(view.Constructors), roots)
		})
	}
}
