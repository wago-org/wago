//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

// This spec-suite harness uses t.Skip/t.Fatal and shells out to wast2json, none
// of which work under TinyGo, so it is excluded there (see docs/tinygo.md).

package wago_test

import (
	"encoding/binary"
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
	"memory", "float_memory", "memory_trap", "traps", "const",
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
// WAGO_SPEC_VERSION=simd is a focused shortcut for tests/spec/proposals/simd/*.wast.
//
// Each proposal directory is a full testsuite snapshot (the 1.0 core plus the
// proposal's new tests, and it also inherits earlier proposals' files), so a file
// is only counted once — for the earliest version that introduced it — by
// excluding every basename already claimed by the suite root or a lower version.
func specFilesForVersion(version, dir string) []string {
	if version == "1.0" {
		return coreFiles1_0
	}
	if version == "simd" {
		var out []string
		for _, name := range wastNames(filepath.Join(dir, "proposals", "simd")) {
			out = append(out, filepath.Join("proposals", "simd", strings.TrimSuffix(name, ".wast")))
		}
		sort.Strings(out)
		return out
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
	Type     string          `json:"type"`
	LaneType string          `json:"lane_type"` // wast2json's v128 lane type, e.g. i8/i16/i32/i64/f32/f64.
	Value    json.RawMessage `json:"value"`
}

// str returns the value as a plain JSON string and whether it was one. Numeric
// scalar values use this shape; v128 values use a lane array instead.
func (v specValue) str() (string, bool) {
	var s string
	if err := json.Unmarshal(v.Value, &s); err != nil {
		return "", false
	}
	return s, true
}

// laneStrings returns wast2json's structured v128 lane array as strings. Recent
// WABT emits strings, but accepting raw JSON numbers too keeps helper unit tests
// and older dumps easy to read.
func (v specValue) laneStrings() ([]string, bool) {
	var raw []json.RawMessage
	if err := json.Unmarshal(v.Value, &raw); err != nil {
		return nil, false
	}
	out := make([]string, len(raw))
	for i, r := range raw {
		var s string
		if err := json.Unmarshal(r, &s); err == nil {
			out[i] = s
			continue
		}
		out[i] = string(r)
	}
	return out, true
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

// specArgSlots decodes one spec value literal into the raw uint64 slot encoding
// Invoke expects: 32-bit types occupy the low word, 64-bit types the full word;
// a v128 occupies two adjacent little-endian uint64 slots. Float bit patterns
// are carried verbatim (wast2json emits decimal bit patterns for floats).
func specArgSlots(v specValue) (slots []uint64, ok bool) {
	if v.Type == "v128" {
		vec, ok := specV128(v)
		if !ok {
			return nil, false
		}
		return []uint64{binary.LittleEndian.Uint64(vec[0:8]), binary.LittleEndian.Uint64(vec[8:16])}, true
	}
	s, ok := v.str()
	if !ok {
		return nil, false // structured non-v128 value — out of scope
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return nil, false // non-numeric (e.g. ref.null / externref) — out of scope
	}
	return []uint64{n}, true
}

// valueWidth64 reports whether a spec value type occupies a full 64-bit slot.
func valueWidth64(typ string) bool { return typ == "i64" || typ == "f64" }

// resultSlotCount reports how many public Invoke result slots a spec value uses.
func resultSlotCount(v specValue) int {
	if v.Type == "v128" {
		return 2
	}
	return 1
}

func expectedResultSlots(vals []specValue) int {
	n := 0
	for _, v := range vals {
		n += resultSlotCount(v)
	}
	return n
}

// matchResult reports whether raw Invoke result slots match the spec's expected
// value, including the two NaN result classes. It consumes one slot for scalar
// values and two slots for v128.
func matchResult(got []uint64, want specValue) bool {
	if want.Type == "v128" {
		return matchV128Result(got, want)
	}
	if len(got) == 0 {
		return false
	}
	s, ok := want.str()
	if !ok {
		return false
	}
	switch s {
	case "nan:canonical":
		return isNaNClass(got[0], want.Type, true)
	case "nan:arithmetic":
		return isNaNClass(got[0], want.Type, false)
	}
	wbits, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return false
	}
	if valueWidth64(want.Type) {
		return got[0] == wbits
	}
	return uint32(got[0]) == uint32(wbits)
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

func specV128(v specValue) (wago.V128, bool) {
	var out wago.V128
	lanes, ok := v.laneStrings()
	if !ok {
		return out, false
	}
	putInt := func(i, bits int, s string) bool {
		u, ok := parseLaneBits(s, bits)
		if !ok {
			return false
		}
		switch bits {
		case 8:
			out[i] = byte(u)
		case 16:
			binary.LittleEndian.PutUint16(out[i*2:], uint16(u))
		case 32:
			binary.LittleEndian.PutUint32(out[i*4:], uint32(u))
		case 64:
			binary.LittleEndian.PutUint64(out[i*8:], u)
		}
		return true
	}
	switch v.LaneType {
	case "i8", "i8x16":
		if len(lanes) != 16 {
			return out, false
		}
		for i, s := range lanes {
			if !putInt(i, 8, s) {
				return out, false
			}
		}
	case "i16", "i16x8":
		if len(lanes) != 8 {
			return out, false
		}
		for i, s := range lanes {
			if !putInt(i, 16, s) {
				return out, false
			}
		}
	case "i32", "i32x4", "f32", "f32x4":
		if len(lanes) != 4 {
			return out, false
		}
		for i, s := range lanes {
			if !putInt(i, 32, s) {
				return out, false
			}
		}
	case "i64", "i64x2", "f64", "f64x2":
		if len(lanes) != 2 {
			return out, false
		}
		for i, s := range lanes {
			if !putInt(i, 64, s) {
				return out, false
			}
		}
	default:
		return out, false
	}
	return out, true
}

func parseLaneBits(s string, bits int) (uint64, bool) {
	if s == "nan:canonical" || s == "nan:arithmetic" {
		return 0, false
	}
	if u, err := strconv.ParseUint(s, 10, bits); err == nil {
		return u, true
	}
	i, err := strconv.ParseInt(s, 10, bits)
	if err != nil {
		return 0, false
	}
	return uint64(i), true
}

func matchV128Result(got []uint64, want specValue) bool {
	if len(got) < 2 {
		return false
	}
	if want.LaneType == "f32" || want.LaneType == "f32x4" || want.LaneType == "f64" || want.LaneType == "f64x2" {
		return matchFloatV128Result(got, want)
	}
	w, ok := specV128(want)
	if !ok {
		return false
	}
	return binary.LittleEndian.Uint64(w[:8]) == got[0] && binary.LittleEndian.Uint64(w[8:]) == got[1]
}

func matchFloatV128Result(got []uint64, want specValue) bool {
	lanes, ok := want.laneStrings()
	if !ok {
		return false
	}
	var gb [16]byte
	binary.LittleEndian.PutUint64(gb[:8], got[0])
	binary.LittleEndian.PutUint64(gb[8:], got[1])
	switch want.LaneType {
	case "f32", "f32x4":
		if len(lanes) != 4 {
			return false
		}
		for i, s := range lanes {
			bits := uint64(binary.LittleEndian.Uint32(gb[i*4:]))
			switch s {
			case "nan:canonical":
				if !isNaNClass(bits, "f32", true) {
					return false
				}
			case "nan:arithmetic":
				if !isNaNClass(bits, "f32", false) {
					return false
				}
			default:
				wantBits, ok := parseLaneBits(s, 32)
				if !ok || uint32(bits) != uint32(wantBits) {
					return false
				}
			}
		}
		return true
	case "f64", "f64x2":
		if len(lanes) != 2 {
			return false
		}
		for i, s := range lanes {
			bits := binary.LittleEndian.Uint64(gb[i*8:])
			switch s {
			case "nan:canonical":
				if !isNaNClass(bits, "f64", true) {
					return false
				}
			case "nan:arithmetic":
				if !isNaNClass(bits, "f64", false) {
					return false
				}
			default:
				wantBits, ok := parseLaneBits(s, 64)
				if !ok || bits != wantBits {
					return false
				}
			}
		}
		return true
	default:
		return false
	}
}

func TestSpecValueV128StructuredJSON(t *testing.T) {
	raw := []byte(`{
		"type":"assert_return",
		"action":{"type":"invoke","field":"id","args":[
			{"type":"v128","lane_type":"i8","value":["0","1","2","3","4","5","6","7","8","9","10","11","12","13","14","15"]}
		]},
		"expected":[
			{"type":"v128","lane_type":"i16","value":["-1","1","32767","-32768","4660","22136","39612","57072"]},
			{"type":"v128","lane_type":"f32","value":["0","2143289344","nan:arithmetic","2147483648"]}
		]
	}`)
	var cmd specExecCmd
	if err := json.Unmarshal(raw, &cmd); err != nil {
		t.Fatal(err)
	}

	arg, ok := specArgSlots(cmd.Action.Args[0])
	if !ok {
		t.Fatalf("specArgSlots rejected v128 arg")
	}
	if len(arg) != 2 || arg[0] != 0x0706050403020100 || arg[1] != 0x0f0e0d0c0b0a0908 {
		t.Fatalf("arg slots = %#x, want little-endian bytes 00..0f", arg)
	}

	wantInt, ok := specV128(cmd.Expected[0])
	if !ok {
		t.Fatalf("specV128 rejected integer lanes")
	}
	wantIntSlots := []uint64{binary.LittleEndian.Uint64(wantInt[:8]), binary.LittleEndian.Uint64(wantInt[8:])}
	if !matchResult(wantIntSlots, cmd.Expected[0]) {
		t.Fatalf("integer v128 expected value did not match its own slot encoding")
	}

	var gotFloat wago.V128
	binary.LittleEndian.PutUint32(gotFloat[0:], 0)
	binary.LittleEndian.PutUint32(gotFloat[4:], 0x7fc00000)  // canonical NaN.
	binary.LittleEndian.PutUint32(gotFloat[8:], 0x7fc00001)  // arithmetic NaN.
	binary.LittleEndian.PutUint32(gotFloat[12:], 0x80000000) // -0.
	gotFloatSlots := []uint64{binary.LittleEndian.Uint64(gotFloat[:8]), binary.LittleEndian.Uint64(gotFloat[8:])}
	if !matchResult(gotFloatSlots, cmd.Expected[1]) {
		t.Fatalf("float v128 expected value did not match lane NaN classes")
	}
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
// for the native code generators.
func TestSpecSuiteExec(t *testing.T) {
	dir := os.Getenv("WAGO_SPECTEST_DIR")
	if dir == "" {
		t.Skip("set WAGO_SPECTEST_DIR to a checked-out WebAssembly/testsuite to run")
	}
	dir = resolveSpecDir(t, dir)
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

func resolveSpecDir(t *testing.T, dir string) string {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dir, "i32.wast")); err == nil {
		return dir
	}
	if filepath.IsAbs(dir) {
		t.Fatalf("WAGO_SPECTEST_DIR %q does not look like a testsuite checkout", dir)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		cand := filepath.Join(wd, dir)
		if _, err := os.Stat(filepath.Join(cand, "i32.wast")); err == nil {
			return cand
		}
		next := filepath.Dir(wd)
		if next == wd {
			break
		}
		wd = next
	}
	t.Fatalf("WAGO_SPECTEST_DIR %q does not look like a testsuite checkout", dir)
	return ""
}

func runSpecExec(t *testing.T, wast2json, dir, version string) {
	tmp := t.TempDir()
	files := specFilesForVersion(version, dir)
	if len(files) == 0 {
		t.Fatalf("no spec files found for WAGO_SPEC_VERSION=%q under %s", version, dir)
	}
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

// spectestImports supplies the WebAssembly testsuite's standard "spectest" host
// module. wago is a 1.0 engine without the full linking harness, so this provides
// the pieces the 1.0 corpus actually depends on: the four well-known immutable
// globals (each == 666 in the reference interpreter). Extra entries are ignored by
// modules that don't import them, so this is safe to pass to every instantiate.
func spectestImports() wago.Imports {
	return wago.Imports{
		"spectest.global_i32": wago.GlobalImport{Type: wago.ValI32, Bits: wago.I32(666)},
		"spectest.global_i64": wago.GlobalImport{Type: wago.ValI64, Bits: wago.I64(666)},
		"spectest.global_f32": wago.GlobalImport{Type: wago.ValF32, Bits: wago.F32(666)},
		"spectest.global_f64": wago.GlobalImport{Type: wago.ValF64, Bits: wago.F64(666)},
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
			compiled, err := wago.Compile(cfg, data)
			if err != nil {
				skipMod++
				continue // unsupported module (feature out of scope) — not a miscompile
			}
			in, err := wago.Instantiate(compiled, wago.InstantiateOptions{Imports: spectestImports()})
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
	switch c.Action.Type {
	case "invoke":
		if _, _, sigErr := m.compiled.Signature(c.Action.Field); sigErr != nil {
			return nil, false, nil // export absent (module compiled a subset) — skip
		}
		var args []uint64
		for _, a := range c.Action.Args {
			slots, ok := specArgSlots(a)
			if !ok {
				return nil, false, nil // ref/unsupported arg — out of scope
			}
			args = append(args, slots...)
		}
		res, err = m.inst.Invoke(c.Action.Field, args...)
		return res, true, err
	case "get":
		g, gerr := m.inst.ExportedGlobalObject(c.Action.Field)
		if gerr != nil {
			return nil, false, nil // absent export — skip
		}
		if g.Type == wago.ValV128 {
			v, gerr := m.inst.GlobalV128(c.Action.Field)
			if gerr != nil {
				return nil, true, gerr
			}
			return []uint64{binary.LittleEndian.Uint64(v[:8]), binary.LittleEndian.Uint64(v[8:])}, true, nil
		}
		bits, gerr := m.inst.Global(c.Action.Field)
		if gerr != nil {
			return nil, true, gerr
		}
		return []uint64{bits}, true, nil
	default:
		return nil, false, nil
	}
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
	wantSlots := expectedResultSlots(c.Expected)
	if len(res) != wantSlots {
		t.Errorf("%s.wast:%d %s: result slot count got=%d want=%d", base, c.Line, c.Action.Field, len(res), wantSlots)
		return 0, true
	}
	for i, off := 0, 0; i < len(c.Expected); i++ {
		want := c.Expected[i]
		n := resultSlotCount(want)
		if !matchResult(res[off:off+n], want) {
			t.Errorf("%s.wast:%d %s(%v) result[%d]: got=%#x want=%s/%s:%s", base, c.Line, c.Action.Field, argValues(c.Action.Args), i, res[off:off+n], want.Type, want.LaneType, want.Value)
			return 0, true
		}
		off += n
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
