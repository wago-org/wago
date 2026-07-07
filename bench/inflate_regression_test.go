package wagobench

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/wago"
	"github.com/wago-org/wasi"
)

// TestInflateMiscompileRegression guards a fixed codegen bug: inflate.wasm
// (miniz_oxide deflate→inflate round-trip) once produced corrupt output on wago
// while wasmtime ran it correctly (want "inflate:true:50000:e1d870dc").
//
// Root cause (railshot condenseBinary, the on-the-fly register allocator): a
// deferred RHS operand condensed into a register `rr` was placed into a detached
// elem but left UNPINNED, while the original on-stack node still owned rr. When
// computing the LHS then spilled that node (allocReg reclaiming rr — reached most
// readily via the load bounds check in explicit mode, which adds register
// pressure), rr was freed and reused, and the detached elem read a clobbered
// register. The same class of bug affected the in-place self-update branch
// (`x = c - x`, which corrupted guard-page mode). Fixed by pinning the copied-out
// / condensed RHS register across the LHS computation in both branches, mirroring
// the existing deferred-RHS relocation. See condenseBinary in
// src/core/compiler/backend/railshot/emit.go.
//
// The bug only surfaced under high register pressure in one very hot, deeply
// nested i64 bit-buffer OR/shift accumulation in func 13 (flush_block), so it
// required ≥2 pinned locals to reproduce and was mode-dependent per branch (the
// self-update path failed guard-page mode; the deferred-RHS path failed
// explicit-bounds mode). It is checked in both modes here (run this test with
// `-tags wago_guardpage` to cover the guard-page path).
func TestInflateMiscompileRegression(t *testing.T) {
	src, err := os.ReadFile("corpus/inflate.wasm")
	if err != nil {
		t.Skipf("inflate.wasm not present")
	}
	c, err := wago.Compile(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var stdout bytes.Buffer
	in, err := wago.Instantiate(c, wasi.Imports(wasi.Config{Stdout: &stdout, Args: []string{"inflate.wasm"}}))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	if _, err := in.Invoke("_start"); err != nil {
		var ex *wago.ExitError
		if !errors.As(err, &ex) {
			t.Fatalf("trap: %v", err)
		}
	}
	if got := strings.TrimRight(stdout.String(), "\r\n"); got != "inflate:true:50000:e1d870dc" {
		t.Fatalf("output %q, want inflate:true:50000:e1d870dc", got)
	}
}
