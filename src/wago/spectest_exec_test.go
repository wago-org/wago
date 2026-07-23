//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

// This spec-suite harness uses t.Skip/t.Fatal and shells out to wast2json, none
// of which work under TinyGo, so it is excluded there (see docs/tinygo.md).

package wago_test

import (
	"context"
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
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/wago"
	"github.com/wago-org/wago/testutil/wasmtest"
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

// specFilesForVersion returns paths in the preserved legacy testsuite (relative
// to its root and without the .wast extension). 1.0 is the curated MVP core list;
// WAGO_SPEC_VERSION=simd and bulk-memory are focused proposal shortcuts.
// Release 2.0 and 3.0 use spectest.DiscoverRelease2/3 instead.
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
	if version == "bulk-memory" {
		var out []string
		for _, name := range wastNames(filepath.Join(dir, "proposals", "bulk-memory-operations")) {
			out = append(out, filepath.Join("proposals", "bulk-memory-operations", strings.TrimSuffix(name, ".wast")))
		}
		sort.Strings(out)
		return out
	}
	return nil
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
	Module   string      `json:"module"`
	As       string      `json:"as"`
	Action   specAction  `json:"action"`
	Expected []specValue `json:"expected"`
	Either   []specValue `json:"either"`
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

const (
	maxRecordedAbsentExports   = 64
	maxRecordedInstantiateGaps = 64
)

type specGapSite struct {
	line   int
	module string
	field  string
}

type specInstantiateGapKind string

const (
	specInstantiateMissingFunction      specInstantiateGapKind = "missing-standard-function"
	specInstantiateMissingMemory        specInstantiateGapKind = "missing-standard-memory"
	specInstantiateImportedMemoryExport specInstantiateGapKind = "imported-memory-reexport"
	specInstantiateOther                specInstantiateGapKind = "other"
)

type specInstantiateGapSite struct {
	file string
	line int
	kind specInstantiateGapKind
}

type specExecStats struct {
	modulesPassed           int
	modulesSkipped          int
	modulesFailed           int
	assertionsPassed        int
	assertionsSkipped       int
	assertionsFailed        int
	gaps                    [specExecGapReasonCount]int
	absentExports           [maxRecordedAbsentExports]specGapSite
	absentExportSiteCount   int
	instantiateGaps         [maxRecordedInstantiateGaps]specInstantiateGapSite
	instantiateGapSiteCount int
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
	for i := 0; i < other.instantiateGapSiteCount && s.instantiateGapSiteCount < len(s.instantiateGaps); i++ {
		s.instantiateGaps[s.instantiateGapSiteCount] = other.instantiateGaps[i]
		s.instantiateGapSiteCount++
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

func classifyInstantiateGap(err error) specInstantiateGapKind {
	message := err.Error()
	switch {
	case strings.Contains(message, "module imports \"spectest.print"):
		return specInstantiateMissingFunction
	case strings.Contains(message, "missing imported memory \"spectest.memory\""):
		return specInstantiateMissingMemory
	case strings.Contains(message, "cannot re-export an imported memory"):
		return specInstantiateImportedMemoryExport
	default:
		return specInstantiateOther
	}
}

func (s *specExecStats) recordInstantiateGap(file string, line int, err error) {
	if s.instantiateGapSiteCount >= len(s.instantiateGaps) {
		return
	}
	s.instantiateGaps[s.instantiateGapSiteCount] = specInstantiateGapSite{file: file, line: line, kind: classifyInstantiateGap(err)}
	s.instantiateGapSiteCount++
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

func isNullFuncrefSpecValue(v specValue) bool {
	if v.Type != "funcref" {
		return false
	}
	s, ok := v.str()
	return ok && s == "null"
}

func isNullExternrefSpecValue(v specValue) bool {
	if v.Type != "externref" {
		return false
	}
	s, ok := v.str()
	return ok && s == "null"
}

func isNonNullFuncrefSpecValue(v specValue) bool {
	if v.Type != "funcref" {
		return false
	}
	if len(v.Value) == 0 {
		return true
	}
	// WABT encodes the text assertion pattern `(ref.func)` as value "0".
	// It is a non-null wildcard, not Wasm function index zero.
	s, ok := v.str()
	return ok && s == "0"
}

func indexedFuncrefSpecValue(v specValue) (uint32, bool) {
	if v.Type != "funcref" {
		return 0, false
	}
	s, ok := v.str()
	if !ok || s == "null" {
		return 0, false
	}
	index, err := strconv.ParseUint(s, 10, 32)
	return uint32(index), err == nil
}

func classifyAssertionGap(specExecCmd) specExecGapReason {
	// Every reference shape present in the pinned Release 2 corpus is executable.
	// An unknown future shape must reach the normal decoder and fail the harness;
	// do not turn it into a feature skip.
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

func TestSpecInterpreterModuleDefinitionInstances(t *testing.T) {
	tmp := t.TempDir()
	const filename = "definition.0.wasm"
	if err := os.WriteFile(filepath.Join(tmp, filename), []byte("\x00asm\x01\x00\x00\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	sf := specExecFile{Commands: []specExecCmd{
		{Type: "module_definition", Line: 1, Filename: filename, Name: "$M"},
		{Type: "module_instance", Line: 2, Name: "$I1", Module: "$M"},
		{Type: "module_instance", Line: 3, Name: "$I2", Module: "$M"},
	}}
	stats := runSpecExecFile(t, "definition", tmp, sf)
	want := specExecStats{modulesPassed: 2}
	if stats != want {
		t.Fatalf("definition instance stats = %+v, want %+v", stats, want)
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
	if out, err := exec.Command(wast2json, wast, "-o", jsonPath).CombinedOutput(); err != nil {
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
	start, firstModule := -1, -1
	for i, c := range sf.Commands {
		if c.Type == "module" && firstModule < 0 {
			firstModule = i
		}
		if c.Type == "module" && c.Line == moduleLine {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("%s.wast has no module at line %d", base, moduleLine)
	}
	if firstModule >= 0 && firstModule != start {
		focused.Commands = append(focused.Commands, sf.Commands[firstModule])
		for i := firstModule + 1; i < start; i++ {
			if sf.Commands[i].Type == "register" {
				focused.Commands = append(focused.Commands, sf.Commands[i])
				break
			}
		}
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

func runRelease2File(t *testing.T, base string) specExecStats {
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
	if out, err := exec.Command(wast2json, wast, "-o", jsonPath).CombinedOutput(); err != nil {
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
	return runSpecExecFile(t, base, tmp, sf)
}

func TestRelease2InstantiateGapInventory(t *testing.T) {
	var stats specExecStats
	for _, base := range []string{"binary-leb128", "data", "func_ptrs", "imports", "linking", "names", "start", "tokens"} {
		stats.add(runRelease2File(t, base))
	}
	got := stats.instantiateGaps[:stats.instantiateGapSiteCount]
	if len(got) != 0 {
		t.Fatalf("Release 2 instantiate-gap inventory = %+v, want no standard-import or memory re-export gaps", got)
	}
	if stats.modulesSkipped != 0 || stats.assertionsSkipped != 0 {
		t.Fatalf("Release 2 standard-import closeout stats = %+v, want zero skipped modules/assertions", stats)
	}
}

func TestSpectestPrintImportsAreExactNoOps(t *testing.T) {
	table, err := wago.NewTable(10, 20)
	if err != nil {
		t.Fatal(err)
	}
	defer table.Close()
	memory, err := wago.NewSharedMemory(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	imports := spectestImports(table, table, memory)
	want := map[string]wago.FuncSig{
		"spectest.print":         {},
		"spectest.print_i32":     {Params: []wago.ValType{wago.ValI32}},
		"spectest.print_i64":     {Params: []wago.ValType{wago.ValI64}},
		"spectest.print_f32":     {Params: []wago.ValType{wago.ValF32}},
		"spectest.print_f64":     {Params: []wago.ValType{wago.ValF64}},
		"spectest.print_i32_f32": {Params: []wago.ValType{wago.ValI32, wago.ValF32}},
		"spectest.print_f64_f64": {Params: []wago.ValType{wago.ValF64, wago.ValF64}},
	}
	for key, sig := range want {
		fn, ok := imports[key].(wago.HostFunc)
		if !ok || fn == nil {
			t.Errorf("%s = %T, want reflection-free wago.HostFunc", key, imports[key])
			continue
		}
		params, err := specPrintSlots(sig.Params)
		if err != nil {
			t.Fatalf("%s signature: %v", key, err)
		}
		fn(nil, make([]uint64, params), nil)
	}
}

func specPrintSlots(types []wago.ValType) (int, error) {
	n := 0
	for _, typ := range types {
		switch typ {
		case wago.ValI32, wago.ValI64, wago.ValF32, wago.ValF64, wago.ValFuncRef, wago.ValExternRef:
			n++
		case wago.ValV128:
			n += 2
		default:
			return 0, fmt.Errorf("unsupported print parameter type %s", typ)
		}
	}
	return n, nil
}

func TestSpecFuncrefResultMatchesCanonicalFunctionIdentity(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec(append([]byte{0x03, 0x00}, wasmtest.Vec(wasmtest.ULEB(0))...))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x0b}),
			wasmtest.Code([]byte{0xd2, 0x00, 0x0b}),
		)),
	)
	rt := wago.NewRuntime()
	defer rt.Close()
	compiled, err := rt.Compile(module)
	if err != nil {
		t.Fatal(err)
	}
	inst, err := rt.Instantiate(context.Background(), compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close()
	out, err := inst.Invoke("get")
	if err != nil || len(out) != 1 {
		t.Fatalf("get = %#x, %v", out, err)
	}
	m := specModule{inst: inst}
	if !m.matchFuncref(out[0], specValue{Type: "funcref", Value: json.RawMessage(`"0"`)}) {
		t.Fatal("ref.func 0 result did not match its canonical function identity")
	}
	if m.matchFuncref(out[0], specValue{Type: "funcref", Value: json.RawMessage(`"1"`)}) {
		t.Fatal("ref.func 0 result matched ref.func 1")
	}
}

func TestRelease2LocalExternrefGlobalExecution(t *testing.T) {
	stats := runRelease2FocusedModule(t, "global", 3)
	want := specExecStats{modulesPassed: 1, assertionsPassed: 58}
	if stats != want {
		t.Fatalf("global line 3 execution stats = %+v, want %+v", stats, want)
	}
}

func TestRelease2ExternrefSelectExecution(t *testing.T) {
	stats := runRelease2FocusedModule(t, "select", 1)
	want := specExecStats{modulesPassed: 1, assertionsPassed: 118}
	if stats != want {
		t.Fatalf("select externref execution stats = %+v, want %+v", stats, want)
	}
}

func TestRelease2ExternrefBrTableExecution(t *testing.T) {
	stats := runRelease2FocusedModule(t, "br_table", 3)
	want := specExecStats{modulesPassed: 1, assertionsPassed: 149}
	if stats != want {
		t.Fatalf("br_table externref execution stats = %+v, want %+v", stats, want)
	}
}

func TestRelease2ExternrefTableExecution(t *testing.T) {
	for _, tc := range []struct {
		file      string
		line      int
		want      specExecStats
		gapReason specExecGapReason
		gapCount  int
	}{
		{file: "ref_is_null", line: 1, want: specExecStats{modulesPassed: 1, assertionsPassed: 13}},
		{file: "table_get", line: 1, want: specExecStats{modulesPassed: 1, assertionsPassed: 10}},
		{file: "table_set", line: 1, want: specExecStats{modulesPassed: 1, assertionsPassed: 18}},
		{file: "table_size", line: 1, want: specExecStats{modulesPassed: 1, assertionsPassed: 36}},
		{file: "table_grow", line: 1, want: specExecStats{modulesPassed: 1, assertionsPassed: 21}},
		{file: "table_grow", line: 53, want: specExecStats{modulesPassed: 2, assertionsPassed: 5}},
		{file: "table_fill", line: 1, want: specExecStats{modulesPassed: 1, assertionsPassed: 35}},
	} {
		t.Run(fmt.Sprintf("%s-line-%d", tc.file, tc.line), func(t *testing.T) {
			stats := runRelease2FocusedModule(t, tc.file, tc.line)
			want := tc.want
			if tc.gapReason != specGapNone {
				want.gaps[tc.gapReason] = tc.gapCount
			}
			if stats != want {
				t.Fatalf("%s line %d execution stats = %+v, want %+v", tc.file, tc.line, stats, want)
			}
		})
	}
}

func TestRelease2ImportedReferenceGlobalLinkingExecution(t *testing.T) {
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
	if out, err := exec.Command(wast2json, wast, "-o", jsonPath).CombinedOutput(); err != nil {
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
	focused := specExecFile{}
	for _, command := range sf.Commands {
		if command.Line >= 96 && command.Line <= 111 {
			focused.Commands = append(focused.Commands, command)
		}
	}
	stats := runSpecExecFile(t, "linking", tmp, focused)
	want := specExecStats{modulesPassed: 2}
	if stats != want {
		t.Fatalf("linking reference-global execution stats = %+v, want %+v", stats, want)
	}
}

func TestRelease2ImportedExternrefTableLinkingExecution(t *testing.T) {
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
	if out, err := exec.Command(wast2json, wast, "-o", jsonPath).CombinedOutput(); err != nil {
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
	focused := specExecFile{}
	for _, command := range sf.Commands {
		if command.Line >= 291 && command.Line <= 309 {
			focused.Commands = append(focused.Commands, command)
		}
	}
	stats := runSpecExecFile(t, "linking", tmp, focused)
	want := specExecStats{modulesPassed: 2}
	if stats != want {
		t.Fatalf("linking externref-table execution stats = %+v, want %+v", stats, want)
	}
}

func TestRelease2TypedElementCompileGapExecution(t *testing.T) {
	for _, tc := range []struct {
		file string
		line int
	}{
		{file: "bulk", line: 274},
		{file: "bulk", line: 297},
		{file: "table", line: 8},
		{file: "table", line: 9},
	} {
		t.Run(fmt.Sprintf("%s-line-%d", tc.file, tc.line), func(t *testing.T) {
			stats := runRelease2FocusedModule(t, tc.file, tc.line)
			want := specExecStats{modulesPassed: 2}
			if stats != want {
				t.Fatalf("%s line %d execution stats = %+v, want %+v", tc.file, tc.line, stats, want)
			}
		})
	}

	wast := filepath.Clean("../../tests/spec-v2/test/core/elem.wast")
	if _, err := os.Stat(wast); err != nil {
		t.Skipf("Release 2 elem fixture unavailable: %v", err)
	}
	wast2json, err := exec.LookPath("wast2json")
	if err != nil {
		t.Skip("wast2json (wabt) not on PATH")
	}
	tmp := t.TempDir()
	jsonPath := filepath.Join(tmp, "elem.json")
	if out, err := exec.Command(wast2json, wast, "-o", jsonPath).CombinedOutput(); err != nil {
		t.Fatalf("elem.wast wast2json failed (%v): %s", err, out)
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
	for _, command := range sf.Commands {
		if command.Line >= 654 && command.Line <= 677 {
			focused.Commands = append(focused.Commands, command)
		}
	}
	stats := runSpecExecFile(t, "elem", tmp, focused)
	want := specExecStats{modulesPassed: 2, assertionsPassed: 8}
	if stats != want {
		t.Fatalf("elem lines 654-677 execution stats = %+v, want %+v", stats, want)
	}
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

func TestRelease2MultipleImportedThenLocalTableExecution(t *testing.T) {
	stats := runRelease2FocusedModule(t, "imports", 376)
	want := specExecStats{modulesPassed: 2}
	if stats != want {
		t.Fatalf("imports line 376 execution stats = %+v, want %+v", stats, want)
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
	if out, err := exec.Command(wast2json, wast, "-o", jsonPath).CombinedOutput(); err != nil {
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
	if out, err := exec.Command(wast2json, wast, "-o", jsonPath).CombinedOutput(); err != nil {
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
	if isNullFuncrefSpecValue(v) || isNullExternrefSpecValue(v) {
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
	if isNullFuncrefSpecValue(want) || isNullExternrefSpecValue(want) {
		return len(got) > 0 && got[0] == 0
	}
	if isNonNullFuncrefSpecValue(want) {
		return len(got) > 0 && got[0] != 0
	}
	if want.Type == "ref" {
		if len(got) == 0 {
			return false
		}
		s, _ := want.str()
		if s == "null" {
			return got[0] == 0
		}
		return got[0] != 0
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

func TestMatchEitherResult(t *testing.T) {
	alternatives := []specValue{
		{Type: "i32", Value: json.RawMessage(`"1"`)},
		{Type: "i32", Value: json.RawMessage(`"2"`)},
	}
	if !matchEitherResult(specModule{}, []uint64{2}, alternatives) {
		t.Fatal("second allowed result did not match")
	}
	if matchEitherResult(specModule{}, []uint64{3}, alternatives) {
		t.Fatal("unexpected result matched alternatives")
	}
	if matchEitherResult(specModule{}, nil, alternatives) {
		t.Fatal("missing result matched alternatives")
	}
	vectors := []specValue{
		{Type: "v128", LaneType: "i64", Value: json.RawMessage(`["3","4"]`)},
		{Type: "v128", LaneType: "i64", Value: json.RawMessage(`["1","2"]`)},
	}
	if !matchEitherResult(specModule{}, []uint64{1, 2}, vectors) {
		t.Fatal("allowed v128 result did not match")
	}
}

// TestSpecSuiteExec runs the official WebAssembly testsuite as a native
// execution oracle: it compiles each module with the selected backend,
// instantiates it, and replays every assert_return / assert_trap, comparing the
// compiled code's results against the spec's expected values. Release 2 and
// Release 3 use independent official WebAssembly/spec pins, and neither release
// is allowed feature skips. A failure therefore means a real execution or
// harness error.
//
// Gated on WAGO_SPECTEST_DIR (a checked-out WebAssembly testsuite) and wast2json
// (wabt) on PATH; skipped otherwise. This is the authoritative correctness oracle
// for the native code generators.
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
	wast2json, err := resolveWast2JSON()
	if err != nil {
		if version == "3.0" || os.Getenv("WAGO_WAST2JSON") != "" {
			t.Fatal(err)
		}
		t.Skip(err)
	}
	interpreter := ""
	if version == "3.0" {
		interpreter, err = resolveSpecInterpreter()
		if err != nil {
			t.Fatal(err)
		}
	}
	runSpecExec(t, wast2json, interpreter, dir, version, files)
}

const release3SpecRevision = "9d36019973201a19f9c9ebb0f10828b2fe2374aa"

func resolveSpecInterpreter() (string, error) {
	path := os.Getenv("WAGO_SPEC_INTERPRETER")
	if path == "" {
		return "", fmt.Errorf("WAGO_SPEC_INTERPRETER must name the pinned official Release 3 interpreter")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("configured spec interpreter %q is unavailable: %w", path, err)
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("configured spec interpreter %q is not executable", path)
	}
	want := os.Getenv("WAGO_SPEC_INTERPRETER_REVISION")
	if want != release3SpecRevision {
		return "", fmt.Errorf("configured spec interpreter revision = %q, want pinned %q", want, release3SpecRevision)
	}
	stamp, err := os.ReadFile(filepath.Join(filepath.Dir(path), "source-revision"))
	if err != nil {
		return "", fmt.Errorf("configured spec interpreter %q lacks source revision stamp: %w", path, err)
	}
	if got := strings.TrimSpace(string(stamp)); got != want {
		return "", fmt.Errorf("configured spec interpreter %q source revision = %q, want %q", path, got, want)
	}
	out, err := exec.Command(path, "-v", "--help").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("configured spec interpreter %q identity check failed: %w: %s", path, err, firstLine(out))
	}
	if got := firstLine(out); got != "wasm 3.0.0 reference interpreter" {
		return "", fmt.Errorf("configured spec interpreter %q identity = %q, want official 3.0.0 interpreter", path, got)
	}
	return path, nil
}

func resolveWast2JSON() (string, error) {
	path := os.Getenv("WAGO_WAST2JSON")
	if path == "" {
		var err error
		path, err = exec.LookPath("wast2json")
		if err != nil {
			return "", fmt.Errorf("wast2json (wabt) not on PATH")
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("configured wast2json %q is unavailable: %w", path, err)
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("configured wast2json %q is not executable", path)
	}
	if want := os.Getenv("WAGO_WABT_VERSION"); want != "" {
		out, err := exec.Command(path, "--version").CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("configured wast2json %q version check failed: %w: %s", path, err, firstLine(out))
		}
		if got := strings.TrimSpace(string(out)); got != want {
			return "", fmt.Errorf("configured wast2json %q version = %q, want pinned %q", path, got, want)
		}
	}
	return path, nil
}

func TestResolveWast2JSONChecksPinnedVersion(t *testing.T) {
	t.Setenv("PATH", "")
	tool := filepath.Join(t.TempDir(), "wast2json")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nprintf '1.0.41\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_WAST2JSON", tool)
	t.Setenv("WAGO_WABT_VERSION", "1.0.41")
	if got, err := resolveWast2JSON(); err != nil || got != tool {
		t.Fatalf("resolve pinned tool = %q, %v; want %q, nil", got, err, tool)
	}
	t.Setenv("WAGO_WABT_VERSION", "1.0.40")
	if _, err := resolveWast2JSON(); err == nil || !strings.Contains(err.Error(), `version = "1.0.41", want pinned "1.0.40"`) {
		t.Fatalf("version mismatch error = %v", err)
	}
}

func TestResolveSpecInterpreterChecksPinnedRevisionAndIdentity(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "wasm")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nprintf 'wasm 3.0.0 reference interpreter\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "source-revision"), []byte(release3SpecRevision+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SPEC_INTERPRETER", tool)
	t.Setenv("WAGO_SPEC_INTERPRETER_REVISION", release3SpecRevision)
	if got, err := resolveSpecInterpreter(); err != nil || got != tool {
		t.Fatalf("resolve pinned interpreter = %q, %v; want %q, nil", got, err, tool)
	}
	t.Setenv("WAGO_SPEC_INTERPRETER_REVISION", strings.Repeat("0", 40))
	if _, err := resolveSpecInterpreter(); err == nil || !strings.Contains(err.Error(), "want pinned") {
		t.Fatalf("revision mismatch error = %v", err)
	}
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
	if version == "3.0" {
		suite, err := spectest.DiscoverRelease3(checkout)
		if err != nil {
			t.Fatal(err)
		}
		return suite.CoreDir, suite.Files
	}
	dir = resolveSpecDir(t, checkout)
	return dir, specFilesForVersion(version, dir)
}

func TestResolveSpecPlanRelease3UsesOfficialCoreLayout(t *testing.T) {
	checkout := t.TempDir()
	core := filepath.Join(checkout, "test", "core")
	for _, name := range []string{
		"i32.wast", "const.wast", "return_call.wast", "call_ref.wast",
		"gc/struct.wast", "exceptions/throw.wast", "multi-memory/memory-multi.wast",
		"memory64/memory64.wast", "memory64/table64.wast",
		"relaxed-simd/relaxed_laneselect.wast",
	} {
		path := filepath.Join(core, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	dir, files := resolveSpecPlan(t, checkout, "3.0")
	if dir != core {
		t.Fatalf("Release 3 dir = %q, want official core dir %q", dir, core)
	}
	want := filepath.Join("gc", "struct")
	i := sort.SearchStrings(files, want)
	if len(files) != 10 || i >= len(files) || files[i] != want {
		t.Fatalf("Release 3 files = %v, want official mandatory-family sentinels", files)
	}
}

func resolveRepoFile(name string) (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(wd, filepath.FromSlash(name))
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate, nil
		}
		next := filepath.Dir(wd)
		if next == wd {
			return "", fmt.Errorf("repository file %q not found from %s", name, wd)
		}
		wd = next
	}
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

// firstLine returns the first line of b (trimmed), or "(no output)" when empty —
// used to summarize a wast2json failure without dumping its whole error block.
func firstLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "(no output)"
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func runSpecExec(t *testing.T, wast2json, interpreter, dir, version string, files []string) {
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
		args := []string{wast, "-o", jsonPath}
		if version == "3.0" {
			args = append([]string{"--enable-all"}, args...)
		}
		if out, err := exec.Command(wast2json, args...).CombinedOutput(); err != nil {
			wabtError := fmt.Sprintf("%v: %s", err, firstLine(out))
			if version != "3.0" || interpreter == "" {
				t.Errorf("%s: wast2json failed (%s)", base, wabtError)
				continue
			}
			binaryScript := filepath.Join(tmp, name+".bin.wast")
			if interpOut, interpErr := exec.Command(interpreter, "-d", wast, "-o", binaryScript).CombinedOutput(); interpErr != nil {
				t.Errorf("%s: Release 3 text conversion failed; WABT %s; interpreter %v: %s", base, wabtError, interpErr, firstLine(interpOut))
				continue
			}
			converter, resolveErr := resolveRepoFile("scripts/spec-interpreter-json.py")
			if resolveErr != nil {
				t.Errorf("%s: Release 3 converter unavailable after WABT %s: %v", base, wabtError, resolveErr)
				continue
			}
			if convertOut, convertErr := exec.Command(converter, binaryScript, jsonPath).CombinedOutput(); convertErr != nil {
				t.Errorf("%s: Release 3 binary-script conversion failed after WABT %s: %v: %s", base, wabtError, convertErr, firstLine(convertOut))
				continue
			}
			t.Logf("%s: text oracle fallback=WebAssembly/spec interpreter (%s)", base, firstLine(out))
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
	if (version == "2.0" || version == "3.0") && (total.modulesSkipped != 0 || total.assertionsSkipped != 0) {
		t.Errorf("WebAssembly %s execution must have zero feature-related skips: modules=%d assertions=%d gaps %s",
			version, total.modulesSkipped, total.assertionsSkipped, total.gapSummary())
	}
}

// spectestImports supplies the WebAssembly testsuite's file-scoped standard host
// module: exact no-op print functions, four immutable globals, shared memory 1/2,
// and the shared 10/20 funcref table. Extra entries are ignored by modules that do
// not import them, so the same map is safe for every instantiate in one file.
func spectestImports(table, table64 *wago.Table, memory *wago.Memory) wago.Imports {
	noop := wago.HostFunc(func(wago.HostModule, []uint64, []uint64) {})
	return wago.Imports{
		"spectest.print":         noop,
		"spectest.print_i32":     noop,
		"spectest.print_i64":     noop,
		"spectest.print_f32":     noop,
		"spectest.print_f64":     noop,
		"spectest.print_i32_f32": noop,
		"spectest.print_f64_f64": noop,
		"spectest.global_i32":    wago.GlobalImport{Type: wago.ValI32, Bits: wago.I32(666)},
		"spectest.global_i64":    wago.GlobalImport{Type: wago.ValI64, Bits: wago.I64(666)},
		"spectest.global_f32":    wago.GlobalImport{Type: wago.ValF32, Bits: wago.F32(float32(666.6))},
		"spectest.global_f64":    wago.GlobalImport{Type: wago.ValF64, Bits: wago.F64(666.6)},
		"spectest.memory":        memory,
		"spectest.table":         table,
		"spectest.table64":       table64,
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
	standardTable64, err := wago.NewTable64(10, 20)
	if err != nil {
		_ = standardTable.Close()
		t.Fatalf("create spectest.table64: %v", err)
	}
	standardMemory, err := wago.NewSharedMemory(1, 2)
	if err != nil {
		_ = standardTable.Close()
		_ = standardTable64.Close()
		t.Fatalf("create spectest.memory: %v", err)
	}
	cfg := wago.NewRuntimeConfig()
	if os.Getenv("WAGO_SPEC_VERSION") == "3.0" {
		cfg = cfg.WithCoreFeatures(wago.CoreFeaturesV3)
	}
	rt := wago.NewRuntime(wago.WithRuntimeConfig(cfg))
	defer func() {
		for i := range live {
			live[i].close()
		}
		if err := standardTable.Close(); err != nil {
			t.Errorf("close spectest.table: %v", err)
		}
		if err := standardTable64.Close(); err != nil {
			t.Errorf("close spectest.table64: %v", err)
		}
		if err := standardMemory.Close(); err != nil {
			t.Errorf("close spectest.memory: %v", err)
		}
		if err := rt.Close(); err != nil {
			t.Errorf("close spec runtime: %v", err)
		}
	}()
	named := map[string]specModule{}
	registered := map[string]specModule{}
	definitions := map[string][]byte{}
	var latestDefinition []byte
	standardImports := spectestImports(standardTable, standardTable64, standardMemory)
	instantiate := func(data []byte, c specExecCmd) {
		cur = specModule{}
		mod, err := rt.Compile(data)
		if err != nil {
			t.Logf("%s.wast:%d module compile rejected: %v", base, c.Line, err)
			stats.skipModule(specGapCompileRejected)
			return
		}
		compiled := mod.Compiled()
		imports, err := specImportsFor(compiled, registered, standardImports)
		if err != nil {
			t.Logf("%s.wast:%d module imports rejected: %v", base, c.Line, err)
			stats.recordInstantiateGap(base, c.Line, err)
			stats.skipModule(specGapInstantiateRejected)
			return
		}
		in, err := rt.Instantiate(context.Background(), mod, wago.WithImports(imports))
		if err != nil {
			t.Logf("%s.wast:%d module instantiate rejected: %v", base, c.Line, err)
			stats.recordInstantiateGap(base, c.Line, err)
			stats.skipModule(specGapInstantiateRejected)
			return
		}
		stats.modulesPassed++
		cur = specModule{inst: in, compiled: compiled, externrefs: make(map[string]wago.ExternRef)}
		live = append(live, cur)
		if c.Name != "" {
			named[c.Name] = cur
		}
	}

	for _, c := range sf.Commands {
		switch c.Type {
		case "module_definition":
			data, err := os.ReadFile(filepath.Join(tmp, c.Filename))
			if err != nil {
				stats.modulesFailed++
				t.Errorf("%s.wast:%d module definition output %q is unavailable: %v", base, c.Line, c.Filename, err)
				continue
			}
			latestDefinition = data
			if c.Name != "" {
				definitions[c.Name] = data
			}
		case "module_instance":
			data := latestDefinition
			if c.Module != "" {
				data = definitions[c.Module]
			}
			if data == nil {
				stats.modulesFailed++
				cur = specModule{}
				t.Errorf("%s.wast:%d module instance references unavailable definition %q", base, c.Line, c.Module)
				continue
			}
			instantiate(data, c)
		case "module":
			data, err := os.ReadFile(filepath.Join(tmp, c.Filename))
			if err != nil {
				stats.modulesFailed++
				t.Errorf("%s.wast:%d module output %q is unavailable: %v", base, c.Line, c.Filename, err)
				continue
			}
			instantiate(data, c)
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
			mod, err := rt.Compile(data)
			if err != nil {
				stats.skipAssertion(specGapCompileRejected)
				continue
			}
			compiled := mod.Compiled()
			imports, err := specImportsFor(compiled, registered, standardImports)
			if err != nil {
				stats.skipAssertion(specGapInstantiateRejected)
				continue
			}
			in, err := rt.Instantiate(context.Background(), mod, wago.WithImports(imports))
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
			// A failed module command blocks subsequent actions even when they name an
			// earlier registered module: instantiation side effects that the assertion
			// depends on did not occur. Keep the gap visible as module-unavailable.
			if cur.inst == nil {
				stats.skipAssertion(specGapModuleUnavailable)
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
		case "assert_trap", "assert_exhaustion", "assert_exception":
			if gap := classifyAssertionGap(c); gap != specGapNone {
				stats.skipAssertion(gap)
				continue
			}
			if cur.inst == nil {
				stats.skipAssertion(specGapModuleUnavailable)
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
	for _, key := range compiled.MemoryImports() {
		if m, field, found := resolve(key); found {
			memory, err := m.inst.ExportedMemory(field)
			if err != nil {
				return nil, err
			}
			imports[key] = memory
		}
	}
	for _, key := range compiled.TableImports() {
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
	for _, key := range compiled.TagImports() {
		m, field, ok := resolve(key)
		if !ok {
			continue
		}
		tag, err := m.inst.ExportedTag(field)
		if err != nil {
			return nil, err
		}
		imports[key] = tag
	}
	return imports, nil
}

// specModule is the current module under test: the native instance plus the
// compiled metadata (used to confirm an export exists before invoking, so an
// absent-export skip is never confused with a trap).
type specModule struct {
	inst       *wago.Instance
	compiled   *wago.Compiled
	externrefs map[string]wago.ExternRef
}

func (m specModule) externrefArg(v specValue) (uint64, error) {
	if isNullExternrefSpecValue(v) {
		return 0, nil
	}
	id, ok := v.str()
	if !ok {
		return 0, fmt.Errorf("externref value is not a string")
	}
	if ref, ok := m.externrefs[id]; ok {
		return wago.ValueExternRef(ref).Bits(), nil
	}
	ref, err := m.inst.NewExternRef(id)
	if err != nil {
		return 0, err
	}
	m.externrefs[id] = ref
	return wago.ValueExternRef(ref).Bits(), nil
}

func (m specModule) matchExternref(got uint64, want specValue) bool {
	if isNullExternrefSpecValue(want) {
		return got == 0
	}
	id, ok := want.str()
	if !ok || got == 0 {
		return false
	}
	if id == "0" {
		return true // binary-script convention for an anonymous non-null externref
	}
	value, ok := m.inst.ExternRefValue(wago.ValueOf(wago.ValExternRef, got).ExternRef())
	return ok && value == id
}

func (m specModule) matchFuncref(got uint64, want specValue) bool {
	if isNullFuncrefSpecValue(want) {
		return got == 0
	}
	if isNonNullFuncrefSpecValue(want) {
		return got != 0
	}
	index, ok := indexedFuncrefSpecValue(want)
	return ok && got != 0 && m.inst.FuncRefMatchesFunction(wago.ValueOf(wago.ValFuncRef, got).FuncRef(), index)
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
		if m.externrefs == nil {
			m.externrefs = make(map[string]wago.ExternRef)
		}
		var args []uint64
		var transientGCRefs []uint64
		defer func() {
			for _, token := range transientGCRefs {
				_ = m.inst.ReleaseGCRef(wago.ValueOf(wago.ValAnyRef, token).GCRef())
			}
		}()
		for _, a := range c.Action.Args {
			if a.Type == "ref" {
				id, ok := a.str()
				if !ok {
					return specActionOutcome{harnessErr: fmt.Errorf("cannot decode ref argument %s", a.Value)}
				}
				if id == "null" {
					args = append(args, 0)
					continue
				}
				ext := a
				ext.Type = "externref"
				extern, err := m.externrefArg(ext)
				if err != nil {
					return specActionOutcome{harnessErr: fmt.Errorf("cannot create host ref %s: %w", a.Value, err)}
				}
				internal, err := m.inst.Invoke("internalize", extern)
				if err != nil || len(internal) != 1 || internal[0] == 0 {
					return specActionOutcome{harnessErr: fmt.Errorf("cannot internalize host ref %s: %v", a.Value, err)}
				}
				args = append(args, internal[0])
				transientGCRefs = append(transientGCRefs, internal[0])
				continue
			}
			if a.Type == "externref" {
				token, err := m.externrefArg(a)
				if err != nil {
					return specActionOutcome{harnessErr: fmt.Errorf("cannot decode externref argument %s: %w", a.Value, err)}
				}
				args = append(args, token)
				continue
			}
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
		if g.Type == wago.ValFuncRef || g.Type == wago.ValExternRef {
			v, err := m.inst.GlobalValue(c.Action.Field)
			if err != nil {
				return specActionOutcome{trap: err}
			}
			return specActionOutcome{results: []uint64{v.Bits()}}
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
	if len(c.Either) != 0 {
		if len(c.Expected) != 0 {
			t.Errorf("%s.wast:%d %s: harness command has both expected and either result patterns", base, c.Line, c.Action.Field)
			return specGapNone, false
		}
		if !matchEitherResult(m, out.results, c.Either) {
			t.Errorf("%s.wast:%d %s(%v): got=%#x, want one of %+v", base, c.Line, c.Action.Field, argValues(c.Action.Args), out.results, c.Either)
			return specGapNone, false
		}
		return specGapNone, true
	}
	wantSlots := expectedResultSlots(c.Expected)
	if len(out.results) != wantSlots {
		t.Errorf("%s.wast:%d %s: result slot count got=%d want=%d", base, c.Line, c.Action.Field, len(out.results), wantSlots)
		return specGapNone, false
	}
	for i, off := 0, 0; i < len(c.Expected); i++ {
		want := c.Expected[i]
		n := resultSlotCount(want)
		matched := matchResult(out.results[off:off+n], want)
		if want.Type == "externref" && n == 1 {
			matched = m.matchExternref(out.results[off], want)
		}
		if want.Type == "funcref" && n == 1 {
			matched = m.matchFuncref(out.results[off], want)
		}
		if !matched {
			t.Errorf("%s.wast:%d %s(%v) result[%d]: got=%#x want=%s/%s:%s", base, c.Line, c.Action.Field, argValues(c.Action.Args), i, out.results[off:off+n], want.Type, want.LaneType, want.Value)
			return specGapNone, false
		}
		if want.Type == "ref" && n == 1 && out.results[off] != 0 && out.results[off]>>32 != 0 {
			_ = m.inst.ReleaseGCRef(wago.ValueOf(wago.ValAnyRef, out.results[off]).GCRef())
		}
		off += n
	}
	return specGapNone, true
}

func matchEitherResult(m specModule, got []uint64, alternatives []specValue) bool {
	for _, want := range alternatives {
		n := resultSlotCount(want)
		if len(got) != n {
			continue
		}
		matched := matchResult(got, want)
		if want.Type == "externref" && n == 1 {
			matched = m.matchExternref(got[0], want)
		}
		if want.Type == "funcref" && n == 1 {
			matched = m.matchFuncref(got[0], want)
		}
		if matched {
			return true
		}
	}
	return false
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
