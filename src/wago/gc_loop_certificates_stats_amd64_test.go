//go:build linux && amd64 && !tinygo && wago_codegenstats

package wago

import (
	b "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestGCLoopCertificateAdmission(t *testing.T) {
	for _, mergeOnly := range []bool{false, true} {
		m, e := wasm.DecodeModule(gcLoopCertificatesModule(mergeOnly))
		if e != nil {
			t.Fatal(e)
		}
		if e = wasm.ValidateModule(m); e != nil {
			t.Fatal(e)
		}
		for _, interruptible := range []bool{false, true} {
			var s b.ModuleStats
			opts := gcLoopCompileOptions(t, m, &s)
			opts.Interruptible = interruptible
			c, e := b.CompileModuleWith(m, opts)
			if e != nil {
				t.Fatal(e)
			}
			c.CodeImage.Close()
			guards := s.Funcs[1].Peephole["gc-resolve-loop-guard"]
			if interruptible {
				if guards != 0 {
					t.Fatalf("interruptible loop retained %d guards", guards)
				}
			} else {
				want := 3
				if !mergeOnly {
					want = 6
				}
				if guards != want {
					t.Fatalf("mergeOnly=%v guards=%d want=%d", mergeOnly, guards, want)
				}
			}
		}
	}
}

func gcLoopCompileOptions(t *testing.T, m *wasm.Module, s *b.ModuleStats) b.CompileOptions {
	t.Helper()
	metadata, e := frontend.BuildGCTypeMetadata(m)
	if e != nil {
		t.Fatal(e)
	}
	return b.CompileOptions{Stats: s, GCStructHelpers: true, GCArrayHelpers: true, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}}}
}
