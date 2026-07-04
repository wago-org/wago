//go:build linux && amd64 && !tinygo

// This WASI-suite harness uses t.Skip/t.Fatal and os/filepath, none of which
// behave under TinyGo, so it is excluded there (like the spec-suite harness).

package wago

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// wasiManifest mirrors a WebAssembly/wasi-testsuite per-test `.json`. All fields
// are optional; a missing file means the defaults (exit_code 0, no stdout check).
type wasiManifest struct {
	Args     []string          `json:"args"`
	Env      map[string]string `json:"env"`
	Root     json.RawMessage   `json:"root"`       // presence ⇒ needs a preopened dir (filesystem)
	Ops      json.RawMessage   `json:"operations"` // presence ⇒ socket/interactive (wasip3)
	ExitCode int               `json:"exit_code"`
	Stdout   string            `json:"stdout"`
}

// wasiSkip lists wasm32-wasip1 tests wago cannot yet pass beyond the ones auto-
// skipped for needing a filesystem `root`: features not implemented (sockets,
// poll, real fd stat/seek/readdir, path ops) or behavior wago intentionally
// differs on. Keyed by test base name. Curated to keep TestWASISuite green; each
// entry is a concrete growth target.
var wasiSkip = map[string]bool{
	// Backend register allocator hits "no register available to spill" on these
	// (a pre-existing limitation surfaced by large Rust binaries — see the corpus
	// notes; unrelated to WASI). Growth target: fix the allocator, then unskip.
	"big_random_buf":       true,
	"clock_time_get":       true,
	"sched_yield":          true,
	"poll_oneoff_stdio":    true,
	"fopen-with-no-access": true,

	// Import ONLY the void proc_exit, so the module uses the async host-call path
	// where proc_exit is a no-op (it never returns to wasm to trap/exit). Programs
	// that also import a returning function (fd_write, …) run proc_exit
	// synchronously and work — see TestWASIHelloWorld. Growth target: always-sync
	// host calls.
	"proc_exit-success": true,
	"proc_exit-failure": true,

	// Sockets are not implemented (sock_* stubbed ENOTSUP).
	"sock_shutdown-invalid_fd": true,
	"sock_shutdown-not_sock":   true,
}

// TestWASISuite runs the WebAssembly/wasi-testsuite preview1 tests (the submodule
// at tests/wasi) through wago.WASI as a conformance oracle for the sync host-call
// path. Gated on WAGO_WASITEST_DIR (a checked-out wasi-testsuite). Tests that need
// a filesystem preopen, socket operations, or an unimplemented feature are
// skipped; the rest must match their manifest's exit code and stdout.
func TestWASISuite(t *testing.T) {
	dir := os.Getenv("WAGO_WASITEST_DIR")
	if dir == "" {
		t.Skip("set WAGO_WASITEST_DIR to a checked-out WebAssembly/wasi-testsuite to run")
	}
	var wasms []string
	for _, lang := range []string{"assemblyscript", "c", "rust"} {
		files, _ := filepath.Glob(filepath.Join(dir, "tests", lang, "testsuite", "wasm32-wasip1", "*.wasm"))
		wasms = append(wasms, files...)
	}
	if len(wasms) == 0 {
		t.Fatalf("no wasm32-wasip1 tests under %s (submodule checked out?)", dir)
	}
	sort.Strings(wasms)

	var pass, fail, skip int
	for _, wasmPath := range wasms {
		name := strings.TrimSuffix(filepath.Base(wasmPath), ".wasm")
		man := loadWASIManifest(strings.TrimSuffix(wasmPath, ".wasm") + ".json")
		if man.Root != nil || man.Ops != nil || wasiSkip[name] {
			skip++
			continue
		}
		if reason := runOneWASITest(wasmPath, man); reason != "" {
			fail++
			t.Errorf("%-36s FAIL: %s", name, reason)
		} else {
			pass++
			t.Logf("%-36s pass", name)
		}
	}
	t.Logf("TOTAL[wasip1]: passed=%d failed=%d skipped=%d (of %d)", pass, fail, skip, len(wasms))
	if pass == 0 {
		t.Fatal("no wasi tests passed — the suite did not actually run")
	}
}

func loadWASIManifest(path string) wasiManifest {
	var m wasiManifest
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

// runOneWASITest runs one module and returns "" on success or a failure reason.
func runOneWASITest(wasmPath string, man wasiManifest) string {
	src, err := os.ReadFile(wasmPath)
	if err != nil {
		return err.Error()
	}
	c, err := Compile(src)
	if err != nil {
		return "compile: " + err.Error()
	}
	env := make([]string, 0, len(man.Env))
	for k, v := range man.Env {
		env = append(env, k+"="+v)
	}
	sort.Strings(env)
	// Guest argv is [program name, manifest args...] — the reference adapters pass
	// the module path as argv[0] followed by the test's args.
	args := append([]string{filepath.Base(wasmPath)}, man.Args...)
	var stdout bytes.Buffer
	in, err := Instantiate(c, WASI(WASIConfig{Stdout: &stdout, Args: args, Env: env}))
	if err != nil {
		return "instantiate: " + err.Error()
	}
	defer in.Close()

	code := 0
	if _, err := in.Invoke("_start"); err != nil {
		var ex *ExitError
		if !errors.As(err, &ex) {
			return "trap: " + err.Error()
		}
		code = int(ex.Code)
	}
	if code != man.ExitCode {
		return fmt.Sprintf("exit code %d, want %d", code, man.ExitCode)
	}
	if man.Stdout != "" && stdout.String() != man.Stdout {
		return fmt.Sprintf("stdout %q, want %q", stdout.String(), man.Stdout)
	}
	return ""
}
