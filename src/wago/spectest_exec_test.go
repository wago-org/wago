//go:build linux && amd64 && !tinygo

// This spec-suite harness uses t.Skip/t.Fatal and shells out to wast2json, none
// of which work under TinyGo, so it is excluded there (see docs/tinygo.md).

package wago_test

import (
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/wago"
)

// coreFiles1_0 are the WebAssembly 1.0 (MVP) core testsuite .wast files whose
// modules are within the runtime's execution scope. Each contributes
// assert_return / assert_trap execution assertions that run compiled native code
// and compare results against the spec's expected values — the real correctness
// oracle for the code generator (bugs unit tests miss).
var coreFiles1_0 = []string{
	"i32", "i64", "f32", "f64", "f32_cmp", "f64_cmp", "f32_bitwise", "f64_bitwise",
	"int_exprs", "int_literals", "float_literals", "float_exprs", "float_misc",
	"conversions", "forward", "fac", "block", "loop", "if", "br", "br_if",
	"br_table", "return", "call", "call_indirect", "select", "nop", "unreachable",
	"unwind", "func", "labels", "switch", "stack", "local_get", "local_set",
	"local_tee", "global", "load", "store", "address", "align", "endianness",
	"memory_redundancy", "memory_size", "memory_grow", "left-to-right", "func_ptrs",
}

// proposalDirs maps a post-1.0 WebAssembly version to the testsuite proposal
// directories that version introduced. wago is a 1.0 engine, so most of these are
// skipped (its frontend rejects the features); the ones it does implement
// (bulk-memory, sign-extension, non-trapping conversions) contribute real
// assertions. The mapping lets `make spec-2.0` / `spec-3.0` and the CI card track
// coverage as features are added.
var proposalDirs = map[string][]string{
	"2.0": {"bulk-memory-operations", "reference-types", "simd"},
	"3.0": {"tail-call", "exception-handling", "function-references", "memory64"},
}

// versionOrder lists the post-1.0 versions from oldest to newest, so each new
// test file is attributed to the earliest version that introduced it.
var versionOrder = []string{"2.0", "3.0"}

// specFilesForVersion returns the testsuite paths (relative to the suite root,
// without the .wast extension) contributing to the given spec version. 1.0 is the
// curated MVP core list; 2.0/3.0 are the proposal files that version *adds*.
//
// Each proposal directory is a full testsuite snapshot (the 1.0 core plus the
// proposal's new tests, and it also inherits earlier proposals' files), so a file
// is only counted once — for the earliest version that introduced it — by
// excluding every basename already claimed by the suite root or a lower version.
func specFilesForVersion(version, dir string) []string {
	if version == "1.0" {
		return coreFiles1_0
	}
	claimed := map[string]bool{} // .wast basenames already attributed
	for _, name := range wastNames(dir) {
		claimed[name] = true // the full 1.0 core at the suite root
	}
	var out []string
	for _, v := range versionOrder {
		for _, p := range proposalDirs[v] {
			for _, name := range wastNames(filepath.Join(dir, "proposals", p)) {
				if claimed[name] {
					continue
				}
				claimed[name] = true
				if v == version {
					out = append(out, filepath.Join("proposals", p, strings.TrimSuffix(name, ".wast")))
				}
			}
		}
		if v == version {
			break
		}
	}
	sort.Strings(out)
	return out
}

// wastNames returns the .wast file names directly in dir (empty if dir is absent).
func wastNames(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".wast") {
			out = append(out, e.Name())
		}
	}
	return out
}

type specValue struct {
	Type string `json:"type"`
	// Value is a JSON scalar string for numeric types (wast2json emits the bit
	// pattern as a decimal string), but SIMD/reference proposals emit structured
	// values (e.g. v128 lane arrays). Keep it raw so those files still parse; str()
	// reports whether it is a plain string (numeric/NaN) this harness can handle.
	Value json.RawMessage `json:"value"`
}

// str returns the value as a plain JSON string and whether it was one (false for
// structured values like v128 lane arrays, which are out of this harness's scope).
func (v specValue) str() (string, bool) {
	var s string
	if err := json.Unmarshal(v.Value, &s); err != nil {
		return "", false
	}
	return s, true
}

type specAction struct {
	Type  string      `json:"type"` // "invoke" or "get"
	Field string      `json:"field"`
	Args  []specValue `json:"args"`
}

type specExecCmd struct {
	Type     string      `json:"type"`
	Line     int         `json:"line"`
	Filename string      `json:"filename"`
	Action   specAction  `json:"action"`
	Expected []specValue `json:"expected"`
	Text     string      `json:"text"`
}

type specExecFile struct {
	Commands []specExecCmd `json:"commands"`
}

// specArgBits decodes one spec value literal into the raw uint64 slot encoding
// Invoke expects: 32-bit types occupy the low word, 64-bit types the full word;
// float bit patterns are carried verbatim (wast2json emits the bit pattern as an
// unsigned decimal for every type, so int/float differ only by width).
func specArgBits(v specValue) (bits uint64, ok bool) {
	s, ok := v.str()
	if !ok {
		return 0, false // structured value (v128 lanes) — out of scope
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, false // non-numeric (e.g. ref.null / externref) — out of scope
	}
	return n, true
}

// valueWidth64 reports whether a spec value type occupies a full 64-bit slot.
func valueWidth64(typ string) bool { return typ == "i64" || typ == "f64" }

// matchResult reports whether a raw Invoke result word matches the spec's
// expected value, including the two NaN result classes.
func matchResult(got uint64, want specValue) bool {
	s, ok := want.str()
	if !ok {
		return false // structured expected value (v128) — unsupported here
	}
	switch s {
	case "nan:canonical":
		return isNaNClass(got, want.Type, true)
	case "nan:arithmetic":
		return isNaNClass(got, want.Type, false)
	}
	wbits, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return false
	}
	if valueWidth64(want.Type) {
		return got == wbits
	}
	return uint32(got) == uint32(wbits)
}

// isNaNClass reports whether got holds a NaN of the requested class for the
// float type. Canonical requires the exact canonical payload (mantissa MSB set,
// all other payload bits clear); arithmetic requires only a quiet NaN (mantissa
// MSB set) with any payload. Sign is unconstrained for both.
func isNaNClass(got uint64, typ string, canonical bool) bool {
	if typ == "f32" {
		b := uint32(got)
		if !math.IsNaN(float64(math.Float32frombits(b))) {
			return false
		}
		payload := b & 0x7fffff
		if canonical {
			return payload == 0x400000
		}
		return payload&0x400000 != 0
	}
	// f64
	if !math.IsNaN(math.Float64frombits(got)) {
		return false
	}
	payload := got & 0xfffffffffffff
	if canonical {
		return payload == 0x8000000000000
	}
	return payload&0x8000000000000 != 0
}

// TestSpecSuiteExec runs the official WebAssembly testsuite as a native
// execution oracle: it compiles each module with the selected backend,
// instantiates it, and replays every assert_return / assert_trap, comparing the
// compiled code's results against the spec's expected values. Modules that need
// features the runtime does not support (imports, unsupported opcodes, etc.) are
// skipped rather than failed, so a failure always means a real miscompile.
//
// Gated on WAGO_SPECTEST_DIR (a checked-out WebAssembly/testsuite) and wast2json
// (wabt) on PATH; skipped otherwise. This is the authoritative correctness oracle
// for the x64 code generator (the only backend).
func TestSpecSuiteExec(t *testing.T) {
	dir := os.Getenv("WAGO_SPECTEST_DIR")
	if dir == "" {
		t.Skip("set WAGO_SPECTEST_DIR to a checked-out WebAssembly/testsuite to run")
	}
	wast2json, err := exec.LookPath("wast2json")
	if err != nil {
		t.Skip("wast2json (wabt) not on PATH")
	}
	version := os.Getenv("WAGO_SPEC_VERSION")
	if version == "" {
		version = "1.0"
	}
	runSpecExec(t, wast2json, dir, version)
}

func runSpecExec(t *testing.T, wast2json, dir, version string) {
	tmp := t.TempDir()
	files := specFilesForVersion(version, dir)
	var totPass, totSkipMod, totSkipAssert int
	for _, base := range files {
		wast := filepath.Join(dir, base+".wast")
		if _, err := os.Stat(wast); err != nil {
			continue
		}
		// base may contain path separators (proposal files); flatten it for the
		// wast2json output name so all .json/.wasm land in tmp's root.
		name := strings.ReplaceAll(base, string(filepath.Separator), "_")
		jsonPath := filepath.Join(tmp, name+".json")
		if out, err := exec.Command(wast2json, "--enable-all", wast, "-o", jsonPath).CombinedOutput(); err != nil {
			t.Logf("%s: wast2json failed (%v): %s", base, err, out)
			continue
		}
		raw, err := os.ReadFile(jsonPath)
		if err != nil {
			t.Fatal(err)
		}
		var sf specExecFile
		if err := json.Unmarshal(raw, &sf); err != nil {
			t.Fatal(err)
		}

		pass, skipMod, skipAssert := runSpecExecFile(t, base, tmp, sf)
		totPass += pass
		totSkipMod += skipMod
		totSkipAssert += skipAssert
		t.Logf("%-40s pass=%d  skip(mod=%d assert=%d)", base, pass, skipMod, skipAssert)
	}
	t.Logf("TOTAL[%s]: assertions passed=%d | skipped modules=%d skipped assertions=%d",
		version, totPass, totSkipMod, totSkipAssert)
	// 1.0 must exercise real assertions; 2.0/3.0 legitimately skip everything wago
	// does not implement yet, so zero passes there is expected, not a misconfig.
	if version == "1.0" && totPass == 0 {
		t.Errorf("no execution assertions ran — harness or corpus misconfigured")
	}
}

// runSpecExecFile replays one .wast's commands. The "current" instance is the
// most recently instantiated module; when a module is out of scope (nil inst),
// its assertions are skipped until the next module command.
func runSpecExecFile(t *testing.T, base, tmp string, sf specExecFile) (pass, skipMod, skipAssert int) {
	var cur specModule
	defer cur.close()
	cfg := wago.NewRuntimeConfig()

	for _, c := range sf.Commands {
		switch c.Type {
		case "module":
			cur.close()
			cur = specModule{}
			data, err := os.ReadFile(filepath.Join(tmp, c.Filename))
			if err != nil {
				continue
			}
			compiled, err := wago.CompileWithConfig(cfg, data)
			if err != nil {
				skipMod++
				continue // unsupported module (feature out of scope) — not a miscompile
			}
			in, err := wago.Instantiate(compiled, nil)
			if err != nil {
				skipMod++
				continue // needs imports / unsupported instantiate — out of scope
			}
			cur = specModule{inst: in, compiled: compiled}
		case "assert_return", "action":
			if cur.inst == nil {
				skipAssert++
				continue
			}
			if p, ok := runReturnAssert(t, base, c, cur); ok {
				pass += p
			} else {
				skipAssert++
			}
		case "assert_trap", "assert_exhaustion":
			if cur.inst == nil {
				skipAssert++
				continue
			}
			if runTrapAssert(t, base, c, cur) {
				pass++
			} else {
				skipAssert++
			}
		}
	}
	return pass, skipMod, skipAssert
}

// specModule is the current module under test: the native instance plus the
// compiled metadata (used to confirm an export exists before invoking, so an
// absent-export skip is never confused with a trap).
type specModule struct {
	inst     *wago.Instance
	compiled *wago.Compiled
}

func (m *specModule) close() {
	if m.inst != nil {
		m.inst.Close()
		m.inst = nil
	}
}

// invokeAction performs an assertion's action against inst. It returns the raw
// result words, whether the action was in scope (a supported invoke of an
// existing export with numeric args), and any runtime error (a trap).
func invokeAction(c specExecCmd, m specModule, t *testing.T) (res []uint64, inScope bool, err error) {
	if c.Action.Type != "invoke" {
		return nil, false, nil // "get" reads a global — validated elsewhere, out of exec scope
	}
	if _, _, sigErr := m.compiled.Signature(c.Action.Field); sigErr != nil {
		return nil, false, nil // export absent (module compiled a subset) — skip
	}
	args := make([]uint64, len(c.Action.Args))
	for i, a := range c.Action.Args {
		bits, ok := specArgBits(a)
		if !ok {
			return nil, false, nil // non-numeric arg — out of scope
		}
		args[i] = bits
	}
	res, err = m.inst.Invoke(c.Action.Field, args...)
	return res, true, err
}

func runReturnAssert(t *testing.T, base string, c specExecCmd, m specModule) (int, bool) {
	res, inScope, err := invokeAction(c, m, t)
	if !inScope {
		return 0, false
	}
	if err != nil {
		t.Errorf("%s.wast:%d %s(%v): expected return, got trap: %v", base, c.Line, c.Action.Field, argValues(c.Action.Args), err)
		return 0, true
	}
	if len(res) != len(c.Expected) {
		t.Errorf("%s.wast:%d %s: result count got=%d want=%d", base, c.Line, c.Action.Field, len(res), len(c.Expected))
		return 0, true
	}
	for i, want := range c.Expected {
		if !matchResult(res[i], want) {
			t.Errorf("%s.wast:%d %s(%v) result[%d]: got=%#x want=%s:%s", base, c.Line, c.Action.Field, argValues(c.Action.Args), i, res[i], want.Type, want.Value)
			return 0, true
		}
	}
	return 1, true
}

func runTrapAssert(t *testing.T, base string, c specExecCmd, m specModule) bool {
	_, inScope, err := invokeAction(c, m, t)
	if !inScope {
		return false
	}
	if err == nil {
		t.Errorf("%s.wast:%d %s(%v): expected trap %q, returned normally", base, c.Line, c.Action.Field, argValues(c.Action.Args), c.Text)
		return false
	}
	return true
}

func argValues(args []specValue) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = string(a.Value)
	}
	return strings.Join(parts, ",")
}
