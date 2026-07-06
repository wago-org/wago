//go:build wago_guardpage

package wagobench

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"github.com/wago-org/wago/src/wago"
)

// TestWASIAppsDifferential runs real Rust/WASI programs under BOTH explicit and
// guard-page (signals-based) bounds checks and asserts identical, golden stdout.
// It is the WASI-program analogue of TestCorpusDifferential: several of these
// bugs were bounds-mode-specific miscompiles (e.g. num-bigint's to_str_radix
// corrupted only under guard-page — the #144/sqlite register-pressure class,
// where a local pinned to an arg-staging register R9/R10/R11 was clobbered during
// a call's argument staging in the bounds-check-elided guard-page window). Only
// the guard-page build can exercise signals mode, so this test is tagged
// wago_guardpage.
func TestWASIAppsDifferential(t *testing.T) {
	cases := []struct{ file, want string }{
		{"bignum.wasm", "bignum:1135:12201368:00000000"}, // num-bigint to_str_radix (guard-page R15 over-subscription)
		{"crcsum.wasm", "crc32:0eaf0153"},                // crc
		{"jsonproc.wasm", "json:2000:99939000"},          // serde_json
		{"regexmatch.wasm", "regex:3000:99780"},          // regex (DFA br_table dispatch)
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.file, func(t *testing.T) {
			src, err := os.ReadFile("corpus/" + tc.file)
			if err != nil {
				t.Skipf("%s not present", tc.file)
			}
			for _, mode := range []wago.BoundsCheckMode{wago.BoundsChecksExplicit, wago.BoundsChecksSignalsBased} {
				got := runWASIApp(t, src, mode, tc.file)
				if got != tc.want {
					t.Fatalf("mode %v: output %q, want %q", mode, got, tc.want)
				}
			}
		})
	}
}

func runWASIApp(t *testing.T, src []byte, mode wago.BoundsCheckMode, file string) string {
	t.Helper()
	cfg := wago.NewRuntimeConfig().WithBoundsChecks(mode)
	c, err := wago.CompileWithConfig(cfg, src)
	if err != nil {
		t.Fatalf("%s (%v) compile: %v", file, mode, err)
	}
	var stdout bytes.Buffer
	in, err := wago.Instantiate(c, wago.WASI(wago.WASIConfig{Stdout: &stdout, Args: []string{file}}))
	if err != nil {
		t.Fatalf("%s (%v) instantiate: %v", file, mode, err)
	}
	defer in.Close()
	if _, err := in.Invoke("_start"); err != nil {
		var ex *wago.ExitError
		if !errors.As(err, &ex) {
			t.Fatalf("%s (%v) trap: %v", file, mode, err)
		}
		if ex.Code != 0 {
			t.Fatalf("%s (%v) exited %d (stdout %q)", file, mode, ex.Code, stdout.String())
		}
	}
	return trimTrailingNewline(stdout.String())
}

func trimTrailingNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
