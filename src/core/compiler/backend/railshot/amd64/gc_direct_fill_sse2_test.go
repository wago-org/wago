//go:build linux && amd64 && wago_gc_direct_fill_sse2

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"strings"
	"testing"
)

func TestDirectFillSSE2Instructions(t *testing.T) {
	m, e := wasm.DecodeModule(gcArrayFillReductionModuleBytes(true))
	if e != nil {
		t.Fatal(e)
	}
	if e = wasm.ValidateModule(m); e != nil {
		t.Fatal(e)
	}
	var stats *ModuleStats
	if diagnosticsEnabled {
		stats = &ModuleStats{}
	}
	cm, e := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, GCArrayHelpers: true, AMD64FeaturesSet: true, AMD64Features: 0, Stats: stats})
	if e != nil {
		t.Fatal(e)
	}
	defer cm.CodeImage.Close()
	if stats != nil && stats.Funcs[0].Peephole["gc-fill-sse2"] != 1 {
		t.Fatal("missing SSE2 fill selection", stats.Funcs[0].Peephole)
	}
	listing := disasm(t, cm.Code)
	if !strings.Contains(listing, "movdqu") || !strings.Contains(listing, "pshufd") {
		t.Fatal("missing SSE2 vector stores/broadcast", listing)
	}
	if strings.Contains(listing, "vmovdqu") || strings.Contains(listing, "vpshufd") {
		t.Fatal("unexpected AVX requirement", listing)
	}
	for _, line := range strings.Split(listing, "\n") {
		if strings.Contains(line, "movdqu") || strings.Contains(line, "pshufd") {
			t.Log(line)
		}
	}
}
