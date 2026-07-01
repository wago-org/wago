package wago_test

import (
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/wago"
)

// execFiles are official WebAssembly testsuite .wast files whose modules are
// within the runtime's execution scope. Each contributes assert_return /
// assert_trap execution assertions that run compiled native code and compare
// results against the spec's expected values — the real correctness oracle for
// the code generator (bugs unit tests miss).
var execFiles = []string{
	"i32", "i64", "f32", "f64", "f32_cmp", "f64_cmp", "f32_bitwise", "f64_bitwise",
	"int_exprs", "int_literals", "float_literals", "float_exprs", "float_misc",
	"conversions", "forward", "fac", "block", "loop", "if", "br", "br_if",
	"br_table", "return", "call", "call_indirect", "select", "nop", "unreachable",
	"unwind", "func", "labels", "switch", "stack", "local_get", "local_set",
	"local_tee", "global", "load", "store", "address", "align", "endianness",
	"memory_redundancy", "memory_size", "memory_grow", "left-to-right", "func_ptrs",
}

type specValue struct {
	Type  string `json:"type"`
	Value string `json:"value"`
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
func specArgBits(t *testing.T, v specValue) (bits uint64, ok bool) {
	n, err := strconv.ParseUint(v.Value, 10, 64)
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
	switch want.Value {
	case "nan:canonical":
		return isNaNClass(got, want.Type, true)
	case "nan:arithmetic":
		return isNaNClass(got, want.Type, false)
	}
	wbits, err := strconv.ParseUint(want.Value, 10, 64)
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
// (wabt) on PATH; skipped otherwise. Runs the x64 backend by default; set
// WAGO_SPECTEST_BACKEND=amd64 or =both to include the legacy backend as a
// differential cross-check while it still exists.
func TestSpecSuiteExec(t *testing.T) {
	dir := os.Getenv("WAGO_SPECTEST_DIR")
	if dir == "" {
		t.Skip("set WAGO_SPECTEST_DIR to a checked-out WebAssembly/testsuite to run")
	}
	wast2json, err := exec.LookPath("wast2json")
	if err != nil {
		t.Skip("wast2json (wabt) not on PATH")
	}

	backends := []struct {
		name string
		x64  bool
	}{}
	switch os.Getenv("WAGO_SPECTEST_BACKEND") {
	case "amd64":
		backends = append(backends, struct {
			name string
			x64  bool
		}{"amd64", false})
	case "both":
		backends = append(backends, struct {
			name string
			x64  bool
		}{"x64", true}, struct {
			name string
			x64  bool
		}{"amd64", false})
	default:
		backends = append(backends, struct {
			name string
			x64  bool
		}{"x64", true})
	}

	for _, be := range backends {
		be := be
		t.Run(be.name, func(t *testing.T) {
			runSpecExec(t, wast2json, dir, be.x64)
		})
	}
}

func runSpecExec(t *testing.T, wast2json, dir string, x64 bool) {
	tmp := t.TempDir()
	var totPass, totSkipMod, totSkipAssert int
	for _, base := range execFiles {
		wast := filepath.Join(dir, base+".wast")
		if _, err := os.Stat(wast); err != nil {
			continue
		}
		jsonPath := filepath.Join(tmp, base+".json")
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

		pass, skipMod, skipAssert := runSpecExecFile(t, base, tmp, sf, x64)
		totPass += pass
		totSkipMod += skipMod
		totSkipAssert += skipAssert
		t.Logf("%-18s pass=%d  skip(mod=%d assert=%d)", base, pass, skipMod, skipAssert)
	}
	t.Logf("TOTAL[%s]: assertions passed=%d | skipped modules=%d skipped assertions=%d",
		map[bool]string{true: "x64", false: "amd64"}[x64], totPass, totSkipMod, totSkipAssert)
	if totPass == 0 {
		t.Errorf("no execution assertions ran — harness or corpus misconfigured")
	}
}

// runSpecExecFile replays one .wast's commands. The "current" instance is the
// most recently instantiated module; when a module is out of scope (nil inst),
// its assertions are skipped until the next module command.
func runSpecExecFile(t *testing.T, base, tmp string, sf specExecFile, x64 bool) (pass, skipMod, skipAssert int) {
	var cur specModule
	defer cur.close()
	cfg := wago.NewRuntimeConfig().WithX64(x64)

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
		bits, ok := specArgBits(t, a)
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
		parts[i] = a.Value
	}
	return strings.Join(parts, ",")
}
