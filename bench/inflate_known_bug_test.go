//go:build wago_wasi

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

// TestInflateKnownMiscompile is a checked-in reproduction of an OPEN codegen bug,
// kept SKIPPED until fixed. Remove the Skip when the fix lands to turn it into a
// regression guard.
//
// inflate.wasm (miniz_oxide deflate→inflate round-trip) produces corrupt output
// on wago while wasmtime runs it correctly (want "inflate:true:50000:e1d870dc").
// Unlike the num-bigint bug (#183, guard-page-only), this fails in BOTH bounds
// modes, so it is a mode-independent miscompile.
//
// Localized (via a per-function pin-disable bisection):
//   - Disabling local pinning for exactly function index 13 —
//     miniz_oxide::deflate::core::flush_block (25 KB code, spill_hi=5, a
//     register-heavy call-making + memory-touching function) — makes it correct.
//   - It is NOT register-class-specific: excluding any single class (R9/R10/R11,
//     RBP, or R12–R15) does not fix it; only reducing the pin COUNT does.
//   - Threshold: pinning ≤1 local in flush_block is correct; pinning ≥2 corrupts.
//     A general 2-pin bug would break the whole spec/corpus suite, so it is
//     specific to flush_block's control flow — i.e. the STACK_REG lazy
//     spill/reload state reconciliation (reconcileLocals/convergeEdgeTo /
//     spillLocalsForCall) desyncs two pinned locals across this function's
//     branches. That reconciliation backs every call-making function, so a fix
//     needs its own careful investigation.
func TestInflateKnownMiscompile(t *testing.T) {
	t.Skip("open bug: miniz_oxide flush_block (func 13) miscompiled when ≥2 locals are pinned; see comment")

	src, err := os.ReadFile("corpus/inflate.wasm")
	if err != nil {
		t.Skipf("inflate.wasm not present")
	}
	c, err := wago.Compile(nil, src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var stdout bytes.Buffer
	in, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: wasi.Imports(wasi.Config{Stdout: &stdout, Args: []string{"inflate.wasm"}})})
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
