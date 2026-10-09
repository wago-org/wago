//go:build linux && amd64 && wago_sumunroll

package amd64

import (
	"bytes"
	"os"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	x86 "github.com/wago-org/wago/src/core/encoder/amd64"
)

// This test must fail until the requested follow-up emitter is implemented.
// Selection, bounded bytes, register cleanup, and atomic rejection are separate
// from native oracle tests; a silent baseline fallback cannot pass this test.
func TestSumUnrollFollowupEmission(t *testing.T) {
	v := os.Getenv("WAGO_SUM_VARIANT")
	if v != "H" && v != "T64" && v != "T128" && v != "T256" {
		t.Skip("follow-up candidate only")
	}
	selectSumUnroll(t)
	requireCompilerDiagnostics(t)
	makeFn := func() *fn {
		return &fn{a: &x86.Asm{}, stats: &CodegenStats{}, m: &wasm.Module{Memories: []wasm.MemType{{}}}, locals: []localDef{{reg: R12}, {reg: R13}, {reg: R14}}, pinnedLocalMask: regMask(0).add(R12).add(R13).add(R14), linearSumLoop: 1 | 3<<16}
	}
	f := makeFn()
	pins := f.pinned
	if !f.trySelectedLinearSumLatch(nil, 1) {
		t.Fatal("no selected latch")
	}
	marker := "experimental-linear-sum-hybrid"
	if v != "H" {
		marker = "experimental-linear-sum-threshold"
	}
	if f.stats.Peephole[marker] != 1 {
		t.Fatalf("candidate not emitted: %v", f.stats.Peephole)
	}
	if f.a.Len() > 576 {
		t.Fatalf("latch budget: %d", f.a.Len())
	}
	if f.pinned != pins {
		t.Fatal("temporary pins leaked")
	}
	for _, r := range gpAlloc {
		if f.regUser[r] != nil {
			t.Fatal("temporary owner leaked")
		}
	}
	f = makeFn()
	f.a.B = []byte{0x90}
	for _, r := range gpAlloc {
		f.reserved = f.reserved.add(r)
	}
	sumUnrollExperiment.budget = 0
	// Direct rejection of the configurable emitter, before baseline fallback.
	// The outer wrapper's native fallback is covered by TestSumUnrollNativeFallback.
	saved := *f
	if f.tryExperimentalLinearSumLatch(nil, 1, 16, 4, 0) {
		t.Fatal("zero budget accepted")
	}
	if !bytes.Equal(f.a.B, []byte{0x90}) || f.pinned != saved.pinned || f.reserved != saved.reserved || f.regUser != saved.regUser {
		t.Fatal("rejection mutated state")
	}
}
