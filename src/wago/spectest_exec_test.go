//go:build linux && amd64 && !tinygo

// This spec-suite harness uses t.Skip/t.Fatal and shells out to wast2json, none
// of which work under TinyGo, so it is excluded there (see docs/tinygo.md).

package wago_test

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/spectest"
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

// proposalDirs records proposal lineage in the preserved legacy testsuite. The
// independently pinned Release 2.0 corpus bypasses this map; Release 3.0 still
// uses it to exclude files already introduced by 2.0 proposals.
var proposalDirs = map[string][]string{
	"2.0": {"bulk-memory-operations", "reference-types", "simd"},
	"3.0": {"tail-call", "exception-handling", "function-references", "memory64"},
}

// versionOrder lists the post-1.0 versions from oldest to newest, so each new
// test file is attributed to the earliest version that introduced it.
var versionOrder = []string{"2.0", "3.0"}

// specFilesForVersion returns paths in the preserved legacy testsuite (relative
// to its root and without the .wast extension). 1.0 is the curated MVP core list,
// 3.0 is the proposal delta, and WAGO_SPEC_VERSION=simd is a focused shortcut.
// Release 2.0 uses spectest.DiscoverRelease2 instead.
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
	Type   string      `json:"type"` // "invoke" or "get"
	Module string      `json:"module"`
	Field  string      `json:"field"`
	Args   []specValue `json:"args"`
}

type specExecCmd struct {
	Type     string      `json:"type"`
	Line     int         `json:"line"`
	Filename string      `json:"filename"`
	Name     string      `json:"name"`
	As       string      `json:"as"`
	Action   specAction  `json:"action"`
	Expected []specValue `json:"expected"`
	Text     string      `json:"text"`
}

type specExecFile struct {
	Commands []specExecCmd `json:"commands"`
}

type specExecGapReason uint8

const (
	specGapNone specExecGapReason = iota
	specGapCompileRejected
	specGapInstantiateRejected
	specGapModuleUnavailable
	specGapAbsentExport
	specGapReferenceArgument
	specGapReferenceResult
	specGapReferenceGlobal
	specExecGapReasonCount
)

var specExecGapNames = [...]string{
	specGapCompileRejected:     "compile-rejected",
	specGapInstantiateRejected: "instantiate-rejected",
	specGapModuleUnavailable:   "module-unavailable",
	specGapAbsentExport:        "absent-export",
	specGapReferenceArgument:   "reference-argument",
	specGapReferenceResult:     "reference-result",
	specGapReferenceGlobal:     "reference-global",
}

func (r specExecGapReason) String() string {
	if r > specGapNone && r < specExecGapReasonCount {
		return specExecGapNames[r]
	}
	return "none"
}

const maxRecordedAbsentExports = 64

type specGapSite struct {
	line   int
	module string
	field  string
}

type specExecStats struct {
	modulesPassed         int
	modulesSkipped        int
	modulesFailed         int
	assertionsPassed      int
	assertionsSkipped     int
	assertionsFailed      int
	gaps                  [specExecGapReasonCount]int
	absentExports         [maxRecordedAbsentExports]specGapSite
	absentExportSiteCount int
}

func (s *specExecStats) add(other specExecStats) {
	s.modulesPassed += other.modulesPassed
	s.modulesSkipped += other.modulesSkipped
	s.modulesFailed += other.modulesFailed
	s.assertionsPassed += other.assertionsPassed
	s.assertionsSkipped += other.assertionsSkipped
	s.assertionsFailed += other.assertionsFailed
	for reason := specGapNone + 1; reason < specExecGapReasonCount; reason++ {
		s.gaps[reason] += other.gaps[reason]
	}
	for i := 0; i < other.absentExportSiteCount; i++ {
		s.recordActionGap(specGapAbsentExport, specExecCmd{Line: other.absentExports[i].line, Action: specAction{Module: other.absentExports[i].module, Field: other.absentExports[i].field}})
	}
}

func (s *specExecStats) skipModule(reason specExecGapReason) {
	s.modulesSkipped++
	s.gaps[reason]++
}

func (s *specExecStats) skipAssertion(reason specExecGapReason) {
	s.assertionsSkipped++
	s.gaps[reason]++
}

func (s *specExecStats) recordActionGap(reason specExecGapReason, c specExecCmd) {
	if reason != specGapAbsentExport || s.absentExportSiteCount >= len(s.absentExports) {
		return
	}
	s.absentExports[s.absentExportSiteCount] = specGapSite{line: c.Line, module: c.Action.Module, field: c.Action.Field}
	s.absentExportSiteCount++
}

func (s specExecStats) gapCount(reason specExecGapReason) int {
	if reason <= specGapNone || reason >= specExecGapReasonCount {
		return 0
	}
	return s.gaps[reason]
}

func (s specExecStats) gapSummary() string {
	var parts []string
	for reason := specGapNone + 1; reason < specExecGapReasonCount; reason++ {
		parts = append(parts, fmt.Sprintf("%s=%d", reason, s.gaps[reason]))
	}
	return strings.Join(parts, " ")
}

func isReferenceSpecValue(v specValue) bool {
	return v.Type == "funcref" || v.Type == "externref"
}

func isNullFuncrefSpecValue(v specValue) bool {
	if v.Type != "funcref" {
		return false
	}
	s, ok := v.str()
	return ok && s == "null"
}

func classifyAssertionGap(c specExecCmd) specExecGapReason {
	for _, arg := range c.Action.Args {
		if isReferenceSpecValue(arg) && !isNullFuncrefSpecValue(arg) {
			return specGapReferenceArgument
		}
	}
	if c.Action.Type == "get" {
		for _, expected := range c.Expected {
			if isReferenceSpecValue(expected) {
				return specGapReferenceGlobal
			}
		}
	}
	for _, expected := range c.Expected {
		if isReferenceSpecValue(expected) && !isNullFuncrefSpecValue(expected) {
			return specGapReferenceResult
		}
	}
	return specGapNone
}

func TestSpecExecStatsAccounting(t *testing.T) {
	var total specExecStats
	total.add(specExecStats{modulesPassed: 2, modulesSkipped: 1, assertionsPassed: 5, assertionsFailed: 1})
	total.add(specExecStats{modulesFailed: 1, assertionsPassed: 3, assertionsSkipped: 2})
	want := specExecStats{
		modulesPassed:     2,
		modulesSkipped:    1,
		modulesFailed:     1,
		assertionsPassed:  8,
		assertionsSkipped: 2,
		assertionsFailed:  1,
	}
	if total != want {
		t.Fatalf("stats = %+v, want %+v", total, want)
	}
}

func runRelease2FocusedModule(t *testing.T, base string, moduleLine int) specExecStats {
	t.Helper()
	wast := filepath.Clean("../../tests/spec-v2/test/core/" + base + ".wast")
	if _, err := os.Stat(wast); err != nil {
		t.Skipf("Release 2 %s fixture unavailable: %v", base, err)
	}
	wast2json, err := exec.LookPath("wast2json")
	if err != nil {
		t.Skip("wast2json (wabt) not on PATH")
	}
	tmp := t.TempDir()
	jsonPath := filepath.Join(tmp, base+".json")
	if out, err := exec.Command(wast2json, "--enable-all", wast, "-o", jsonPath).CombinedOutput(); err != nil {
		t.Fatalf("%s.wast wast2json failed (%v): %s", base, err, out)
	}
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	var sf specExecFile
	if err := json.Unmarshal(raw, &sf); err != nil {
		t.Fatal(err)
	}

	focused := specExecFile{}
	for _, c := range sf.Commands {
		if c.Type == "module" {
			focused.Commands = append(focused.Commands, c)
			break
		}
	}
	for _, c := range sf.Commands {
		if c.Type == "register" {
			focused.Commands = append(focused.Commands, c)
			break
		}
	}
	start := -1
	for i, c := range sf.Commands {
		if c.Type == "module" && c.Line == moduleLine {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("%s.wast has no module at line %d", base, moduleLine)
	}
	end := len(sf.Commands)
	for i := start + 1; i < len(sf.Commands); i++ {
		if sf.Commands[i].Type == "module" {
			end = i
			break
		}
	}
	focused.Commands = append(focused.Commands, sf.Commands[start:end]...)
	return runSpecExecFile(t, base, tmp, focused)
}

func TestRelease2MultipleTableCopyExecution(t *testing.T) {
	stats := runRelease2FocusedModule(t, "table_copy", 751)
	want := specExecStats{modulesPassed: 2, assertionsPassed: 61}
	if stats != want {
		t.Fatalf("table_copy line 751 execution stats = %+v, want %+v", stats, want)
	}
}

func TestRelease2NonzeroTableInitExecution(t *testing.T) {
	stats := runRelease2FocusedModule(t, "table_init", 197)
	want := specExecStats{modulesPassed: 2, assertionsPassed: 31}
	if stats != want {
		t.Fatalf("table_init line 197 execution stats = %+v, want %+v", stats, want)
	}
}

func TestRelease2NonzeroTableExportImportExecution(t *testing.T) {
	stats := runRelease2FocusedModule(t, "imports", 386)
	want := specExecStats{modulesPassed: 2}
	if stats != want {
		t.Fatalf("imports line 386 execution stats = %+v, want %+v", stats, want)
	}
}

func TestRelease2ImportedThenLocalTableExecution(t *testing.T) {
	stats := runRelease2FocusedModule(t, "table", 12)
	want := specExecStats{modulesPassed: 2}
	if stats != want {
		t.Fatalf("table line 12 execution stats = %+v, want %+v", stats, want)
	}
}

func TestRelease2ImportedThenLocalTableSourceGuard(t *testing.T) {
	raw, err := os.ReadFile(filepath.Clean("../../tests/spec-v2/test/core/imports.wast"))
	if err != nil {
		t.Skipf("Release 2 imports fixture unavailable: %v", err)
	}
	want := `(module
  (import "spectest" "table" (table 0 funcref))
  (import "spectest" "table" (table 0 funcref))
  (table 10 funcref)
  (table 10 funcref)
)`
	if !strings.Contains(string(raw), want) {
		t.Fatal("imports.wast no longer contains the Release 2 imported-then-local table family at lines 376-381")
	}
}

func TestRelease2RefFuncGlobalExecution(t *testing.T) {
	wast := filepath.Clean("../../tests/spec-v2/test/core/ref_func.wast")
	if _, err := os.Stat(wast); err != nil {
		t.Skipf("Release 2 ref_func fixture unavailable: %v", err)
	}
	wast2json, err := exec.LookPath("wast2json")
	if err != nil {
		t.Skip("wast2json (wabt) not on PATH")
	}
	tmp := t.TempDir()
	jsonPath := filepath.Join(tmp, "ref_func.json")
	if out, err := exec.Command(wast2json, "--enable-all", wast, "-o", jsonPath).CombinedOutput(); err != nil {
		t.Fatalf("ref_func.wast wast2json failed (%v): %s", err, out)
	}
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	var sf specExecFile
	if err := json.Unmarshal(raw, &sf); err != nil {
		t.Fatal(err)
	}

	stats := runSpecExecFile(t, "ref_func", tmp, sf)
	want := specExecStats{modulesPassed: 3, assertionsPassed: 10}
	if stats != want {
		t.Fatalf("ref_func execution stats = %+v, want %+v", stats, want)
	}
}

func TestRelease2LinkingHasNoImportedFunctionReexportGaps(t *testing.T) {
	wast := filepath.Clean("../../tests/spec-v2/test/core/linking.wast")
	if _, err := os.Stat(wast); err != nil {
		t.Skipf("Release 2 linking fixture unavailable: %v", err)
	}
	wast2json, err := exec.LookPath("wast2json")
	if err != nil {
		t.Skip("wast2json (wabt) not on PATH")
	}
	tmp := t.TempDir()
	jsonPath := filepath.Join(tmp, "linking.json")
	if out, err := exec.Command(wast2json, "--enable-all", wast, "-o", jsonPath).CombinedOutput(); err != nil {
		t.Fatalf("linking.wast wast2json failed (%v): %s", err, out)
	}
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	var sf specExecFile
	if err := json.Unmarshal(raw, &sf); err != nil {
		t.Fatal(err)
	}

	stats := runSpecExecFile(t, "linking", tmp, sf)
	if stats.absentExportSiteCount == 0 {
		return
	}
	got := stats.absentExports[:stats.absentExportSiteCount]
	want := []specGapSite{
		{line: 18, module: "$Nf", field: "Mf.call"},
		{line: 71, module: "$Ng", field: "Mg.get"},
		{line: 77, module: "$Ng", field: "Mg.get_mut"},
		{line: 83, module: "$Ng", field: "Mg.get_mut"},
		{line: 169, module: "$Nt", field: "Mt.call"},
		{line: 174, module: "$Nt", field: "Mt.call"},
		{line: 179, module: "$Nt", field: "Mt.call"},
		{line: 184, module: "$Nt", field: "Mt.call"},
		{line: 205, module: "$Nt", field: "Mt.call"},
		{line: 210, module: "$Nt", field: "Mt.call"},
		{line: 216, module: "$Nt", field: "Mt.call"},
		{line: 222, module: "$Nt", field: "Mt.call"},
		{line: 337, module: "$Nm", field: "Mm.load"},
		{line: 350, module: "$Nm", field: "Mm.load"},
	}
	if !sameSpecGapSites(got, want) {
		t.Fatalf("linking imported-function absent-export sites = %+v, want exact known set %+v", got, want)
	}
	t.Fatalf("linking has %d imported-function absent-export gaps at exact lines %+v; want zero", len(got), got)
}

func sameSpecGapSites(a, b []specGapSite) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// specArgSlots decodes one spec value literal into the raw uint64 slot encoding
// Invoke expects: 32-bit types occupy the low word, 64-bit types the full word;
// a v128 occupies two adjacent little-endian uint64 slots. Float bit patterns
// are carried verbatim (wast2json emits decimal bit patterns for floats).
func specArgSlots(v specValue) (slots []uint64, ok bool) {
	if isNullFuncrefSpecValue(v) {
		return []uint64{0}, true
	}
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
	if isNullFuncrefSpecValue(want) {
		return len(got) > 0 && got[0] == 0
	}
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
// compiled code's results against the spec's expected values. Known incomplete
// Release 2 behavior remains skipped while support is under construction, but
// every skip is assigned a fixed reason so no feature gap is hidden in a generic
// module or assertion bucket. A failure therefore means a real execution or
// harness error.
//
// Gated on WAGO_SPECTEST_DIR (a checked-out WebAssembly/testsuite) and wast2json
// (wabt) on PATH; skipped otherwise. This is the authoritative correctness oracle
// for the amd64 code generator (the only backend).
func TestSpecSuiteExec(t *testing.T) {
	dir := os.Getenv("WAGO_SPECTEST_DIR")
	if dir == "" {
		t.Skip("set WAGO_SPECTEST_DIR to a checked-out WebAssembly/testsuite to run")
	}
	version := os.Getenv("WAGO_SPEC_VERSION")
	if version == "" {
		version = "1.0"
	}
	dir, files := resolveSpecPlan(t, dir, version)
	wast2json, err := exec.LookPath("wast2json")
	if err != nil {
		t.Skip("wast2json (wabt) not on PATH")
	}
	runSpecExec(t, wast2json, dir, version, files)
}

func resolveSpecPlan(t *testing.T, checkout, version string) (dir string, files []string) {
	t.Helper()
	if version == "2.0" {
		suite, err := spectest.DiscoverRelease2(checkout)
		if err != nil {
			t.Fatal(err)
		}
		return suite.CoreDir, suite.Files
	}
	dir = resolveSpecDir(t, checkout)
	return dir, specFilesForVersion(version, dir)
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

func runSpecExec(t *testing.T, wast2json, dir, version string, files []string) {
	tmp := t.TempDir()
	if len(files) == 0 {
		t.Fatalf("no spec files found for WAGO_SPEC_VERSION=%q under %s", version, dir)
	}
	var total specExecStats
	for _, base := range files {
		wast := filepath.Join(dir, base+".wast")
		if _, err := os.Stat(wast); err != nil {
			t.Errorf("%s: discovered corpus file is unavailable: %v", base, err)
			continue
		}
		// base may contain path separators (proposal files); flatten it for the
		// wast2json output name so all .json/.wasm land in tmp's root.
		name := strings.ReplaceAll(base, string(filepath.Separator), "_")
		jsonPath := filepath.Join(tmp, name+".json")
		if out, err := exec.Command(wast2json, "--enable-all", wast, "-o", jsonPath).CombinedOutput(); err != nil {
			t.Errorf("%s: wast2json failed (%v): %s", base, err, out)
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

		stats := runSpecExecFile(t, base, tmp, sf)
		total.add(stats)
		t.Logf("%-40s modules(pass=%d fail=%d skip=%d) assertions(pass=%d fail=%d skip=%d) gaps(%s)",
			base, stats.modulesPassed, stats.modulesFailed, stats.modulesSkipped,
			stats.assertionsPassed, stats.assertionsFailed, stats.assertionsSkipped, stats.gapSummary())
	}
	t.Logf("TOTAL[%s]: modules passed=%d failed=%d skipped=%d | assertions passed=%d failed=%d skipped=%d | gaps %s",
		version, total.modulesPassed, total.modulesFailed, total.modulesSkipped,
		total.assertionsPassed, total.assertionsFailed, total.assertionsSkipped, total.gapSummary())
	if total.modulesPassed+total.modulesSkipped+total.modulesFailed == 0 {
		t.Errorf("no modules were accounted — harness or corpus misconfigured")
	}
	if total.assertionsPassed+total.assertionsSkipped+total.assertionsFailed == 0 {
		t.Errorf("no execution assertions were accounted — harness or corpus misconfigured")
	}
}

// spectestImports supplies the WebAssembly testsuite's standard "spectest" host
// module: the four immutable globals (each == 666 in the reference interpreter)
// and the shared 10/20 funcref table. Extra entries are ignored by modules that
// do not import them, so the same map is safe for every instantiate in one file.
func spectestImports(table *wago.Table) wago.Imports {
	return wago.Imports{
		"spectest.global_i32": wago.GlobalImport{Type: wago.ValI32, Bits: wago.I32(666)},
		"spectest.global_i64": wago.GlobalImport{Type: wago.ValI64, Bits: wago.I64(666)},
		"spectest.global_f32": wago.GlobalImport{Type: wago.ValF32, Bits: wago.F32(666)},
		"spectest.global_f64": wago.GlobalImport{Type: wago.ValF64, Bits: wago.F64(666)},
		"spectest.table":      table,
	}
}

// runSpecExecFile replays one .wast's commands. The "current" instance is the
// most recently instantiated module; when a module is out of scope (nil inst),
// its assertions are skipped until the next module command.
func runSpecExecFile(t *testing.T, base, tmp string, sf specExecFile) (stats specExecStats) {
	var cur specModule
	var live []specModule
	standardTable, err := wago.NewTable(10, 20)
	if err != nil {
		t.Fatalf("create spectest.table: %v", err)
	}
	defer func() {
		for i := range live {
			live[i].close()
		}
		if err := standardTable.Close(); err != nil {
			t.Errorf("close spectest.table: %v", err)
		}
	}()
	named := map[string]specModule{}
	registered := map[string]specModule{}
	cfg := wago.NewRuntimeConfig()
	standardImports := spectestImports(standardTable)

	for _, c := range sf.Commands {
		switch c.Type {
		case "module":
			cur = specModule{}
			data, err := os.ReadFile(filepath.Join(tmp, c.Filename))
			if err != nil {
				stats.modulesFailed++
				t.Errorf("%s.wast:%d module output %q is unavailable: %v", base, c.Line, c.Filename, err)
				continue
			}
			compiled, err := wago.Compile(cfg, data)
			if err != nil {
				stats.skipModule(specGapCompileRejected)
				continue
			}
			imports, err := specImportsFor(compiled, registered, standardImports)
			if err != nil {
				stats.skipModule(specGapInstantiateRejected)
				continue
			}
			in, err := wago.Instantiate(compiled, wago.InstantiateOptions{Imports: imports})
			if err != nil {
				stats.skipModule(specGapInstantiateRejected)
				continue
			}
			stats.modulesPassed++
			cur = specModule{inst: in, compiled: compiled}
			live = append(live, cur)
			if c.Name != "" {
				named[c.Name] = cur
			}
		case "register":
			m := cur
			if c.Name != "" {
				m = named[c.Name]
			}
			if m.inst != nil && c.As != "" {
				registered[c.As] = m
			}
		case "assert_uninstantiable":
			data, err := os.ReadFile(filepath.Join(tmp, c.Filename))
			if err != nil {
				stats.assertionsFailed++
				t.Errorf("%s.wast:%d uninstantiable module output %q is unavailable: %v", base, c.Line, c.Filename, err)
				continue
			}
			compiled, err := wago.Compile(cfg, data)
			if err != nil {
				stats.skipAssertion(specGapCompileRejected)
				continue
			}
			imports, err := specImportsFor(compiled, registered, standardImports)
			if err != nil {
				stats.skipAssertion(specGapInstantiateRejected)
				continue
			}
			in, err := wago.Instantiate(compiled, wago.InstantiateOptions{Imports: imports})
			if err == nil {
				live = append(live, specModule{inst: in, compiled: compiled})
				stats.assertionsFailed++
				t.Errorf("%s.wast:%d expected module instantiation to trap: %s", base, c.Line, c.Text)
				continue
			}
			stats.assertionsPassed++
		case "assert_return", "action":
			if gap := classifyAssertionGap(c); gap != specGapNone {
				stats.skipAssertion(gap)
				continue
			}
			m := cur
			if c.Action.Module != "" {
				m = named[c.Action.Module]
			}
			if m.inst == nil {
				stats.skipAssertion(specGapModuleUnavailable)
				continue
			}
			gap, passed := runReturnAssert(t, base, c, m)
			switch {
			case gap != specGapNone:
				stats.skipAssertion(gap)
				stats.recordActionGap(gap, c)
			case passed:
				stats.assertionsPassed++
			default:
				stats.assertionsFailed++
			}
		case "assert_trap", "assert_exhaustion":
			if gap := classifyAssertionGap(c); gap != specGapNone {
				stats.skipAssertion(gap)
				continue
			}
			m := cur
			if c.Action.Module != "" {
				m = named[c.Action.Module]
			}
			if m.inst == nil {
				stats.skipAssertion(specGapModuleUnavailable)
				continue
			}
			gap, passed := runTrapAssert(t, base, c, m)
			switch {
			case gap != specGapNone:
				stats.skipAssertion(gap)
				stats.recordActionGap(gap, c)
			case passed:
				stats.assertionsPassed++
			default:
				stats.assertionsFailed++
			}
		}
	}
	return stats
}

func specImportsFor(compiled *wago.Compiled, registered map[string]specModule, standard wago.Imports) (wago.Imports, error) {
	imports := make(wago.Imports, len(standard))
	for key, value := range standard {
		imports[key] = value
	}
	resolve := func(key string) (specModule, string, bool) {
		for i := 0; i < len(key); i++ {
			if key[i] == '.' {
				m, ok := registered[key[:i]]
				return m, key[i+1:], ok
			}
		}
		return specModule{}, "", false
	}
	for _, key := range compiled.Imports {
		m, field, ok := resolve(key)
		if !ok {
			continue
		}
		ex, err := m.inst.ExportedFunc(field)
		if err != nil {
			return nil, err
		}
		imports[key] = ex
	}
	if key, ok := compiled.MemoryImport(); ok {
		if m, field, found := resolve(key); found {
			memory, err := m.inst.ExportedMemory(field)
			if err != nil {
				return nil, err
			}
			imports[key] = memory
		}
	}
	if key, ok := compiled.TableImport(); ok {
		if m, field, found := resolve(key); found {
			table, err := m.inst.ExportedTable(field)
			if err != nil {
				return nil, err
			}
			imports[key] = table
		}
	}
	for _, imp := range compiled.GlobalImports {
		key := imp.Module + "." + imp.Name
		m, field, ok := resolve(key)
		if !ok {
			continue
		}
		global, err := m.inst.ExportedGlobalObject(field)
		if err != nil {
			return nil, err
		}
		imports[key] = global
	}
	return imports, nil
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

type specActionOutcome struct {
	results    []uint64
	trap       error
	gap        specExecGapReason
	harnessErr error
}

// invokeAction performs an assertion's action against inst. Known unsupported
// Release 2 behavior is returned as a bounded gap reason; malformed harness
// values remain assertion failures instead of becoming skips.
func invokeAction(c specExecCmd, m specModule, _ *testing.T) specActionOutcome {
	if gap := classifyAssertionGap(c); gap != specGapNone {
		return specActionOutcome{gap: gap}
	}
	switch c.Action.Type {
	case "invoke":
		if _, _, sigErr := m.compiled.Signature(c.Action.Field); sigErr != nil {
			return specActionOutcome{gap: specGapAbsentExport}
		}
		var args []uint64
		for _, a := range c.Action.Args {
			slots, ok := specArgSlots(a)
			if !ok {
				return specActionOutcome{harnessErr: fmt.Errorf("cannot decode %s argument %s", a.Type, a.Value)}
			}
			args = append(args, slots...)
		}
		res, err := m.inst.Invoke(c.Action.Field, args...)
		return specActionOutcome{results: res, trap: err}
	case "get":
		g, err := m.inst.ExportedGlobalObject(c.Action.Field)
		if err != nil {
			return specActionOutcome{gap: specGapAbsentExport}
		}
		if g.Type == wago.ValV128 {
			v, err := m.inst.GlobalV128(c.Action.Field)
			if err != nil {
				return specActionOutcome{trap: err}
			}
			return specActionOutcome{results: []uint64{binary.LittleEndian.Uint64(v[:8]), binary.LittleEndian.Uint64(v[8:])}}
		}
		bits, err := m.inst.Global(c.Action.Field)
		if err != nil {
			return specActionOutcome{trap: err}
		}
		return specActionOutcome{results: []uint64{bits}}
	default:
		return specActionOutcome{harnessErr: fmt.Errorf("unsupported spec action %q", c.Action.Type)}
	}
}

func runReturnAssert(t *testing.T, base string, c specExecCmd, m specModule) (specExecGapReason, bool) {
	out := invokeAction(c, m, t)
	if out.gap != specGapNone {
		return out.gap, false
	}
	if out.harnessErr != nil {
		t.Errorf("%s.wast:%d %s: harness action failed: %v", base, c.Line, c.Action.Field, out.harnessErr)
		return specGapNone, false
	}
	if out.trap != nil {
		t.Errorf("%s.wast:%d %s(%v): expected return, got trap: %v", base, c.Line, c.Action.Field, argValues(c.Action.Args), out.trap)
		return specGapNone, false
	}
	wantSlots := expectedResultSlots(c.Expected)
	if len(out.results) != wantSlots {
		t.Errorf("%s.wast:%d %s: result slot count got=%d want=%d", base, c.Line, c.Action.Field, len(out.results), wantSlots)
		return specGapNone, false
	}
	for i, off := 0, 0; i < len(c.Expected); i++ {
		want := c.Expected[i]
		n := resultSlotCount(want)
		if !matchResult(out.results[off:off+n], want) {
			t.Errorf("%s.wast:%d %s(%v) result[%d]: got=%#x want=%s/%s:%s", base, c.Line, c.Action.Field, argValues(c.Action.Args), i, out.results[off:off+n], want.Type, want.LaneType, want.Value)
			return specGapNone, false
		}
		off += n
	}
	return specGapNone, true
}

func runTrapAssert(t *testing.T, base string, c specExecCmd, m specModule) (specExecGapReason, bool) {
	out := invokeAction(c, m, t)
	if out.gap != specGapNone {
		return out.gap, false
	}
	if out.harnessErr != nil {
		t.Errorf("%s.wast:%d %s: harness action failed: %v", base, c.Line, c.Action.Field, out.harnessErr)
		return specGapNone, false
	}
	if out.trap == nil {
		t.Errorf("%s.wast:%d %s(%v): expected trap %q, returned normally", base, c.Line, c.Action.Field, argValues(c.Action.Args), c.Text)
		return specGapNone, false
	}
	return specGapNone, true
}

func argValues(args []specValue) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = string(a.Value)
	}
	return strings.Join(parts, ",")
}
