//go:build linux && amd64 && !tinygo

package wasi_test

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/plugins/wasi"
)

// TestWASIApps runs the real Rust/WASI application corpus (bench/corpus/rust-wasi,
// built to wasm32-wasip1) end to end through wago.WASI and checks each program's
// deterministic output. Unlike the emscripten/Go real-large tier (which only
// decode/validate/compile), these actually execute — a guard that wago runs real
// third-party libraries correctly. Skips a binary that isn't checked in.
func TestWASIApps(t *testing.T) {
	cases := []struct {
		file, want string
	}{
		{"markdown.wasm", "markdown: 90762 bytes md -> 153162 bytes html"},                            // pulldown-cmark
		{"crcsum.wasm", "crc32:0eaf0153"},                                                             // crc
		{"blake3sum.wasm", "blake3:2c0df2a3958b9ae33905bcf5b5c3bbbd18e1803ca69e76da038f728def02886e"}, // blake3
		{"base64x.wasm", "base64:40000"},                                                              // base64
		{"jsonproc.wasm", "json:2000:99939000"},                                                       // serde_json
		{"script.wasm", "rhai:599960000"},                                                             // rhai scripting engine
		{"regexmatch.wasm", "regex:3000:99780"},                                                       // regex (DFA br_table dispatch)
		{"bignum.wasm", "bignum:1135:12201368:00000000"},                                              // num-bigint (to_str_radix)
	}
	for _, tc := range cases {
		t.Run(strings.TrimSuffix(tc.file, ".wasm"), func(t *testing.T) {
			src, err := os.ReadFile("../../bench/corpus/" + tc.file)
			if err != nil {
				t.Skipf("%s not present", tc.file)
			}
			c, err := wago.Compile(src)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			var stdout bytes.Buffer
			in, err := wago.Instantiate(c, wasi.Imports(wasi.Config{Stdout: &stdout, Args: []string{tc.file}}))
			if err != nil {
				t.Fatalf("instantiate: %v", err)
			}
			defer in.Close()
			if _, err := in.Invoke("_start"); err != nil {
				var ex *wago.ExitError
				if !errors.As(err, &ex) {
					t.Fatalf("trap: %v", err)
				}
				if ex.Code != 0 {
					t.Fatalf("exited %d (stdout %q)", ex.Code, stdout.String())
				}
			}
			if got := strings.TrimSpace(stdout.String()); got != tc.want {
				t.Fatalf("output %q, want %q", got, tc.want)
			}
		})
	}
}
