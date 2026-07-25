//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo

package wago_test

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago/src/wago"
)

type wasmtimeWasm2Fixture struct {
	path     string
	coverage string
	mode     string
}

type wasmtimeProvenance struct {
	UpstreamRepo                  string   `json:"upstream_repo"`
	Revision                      string   `json:"revision"`
	RevisionDate                  string   `json:"revision_date"`
	SourceRoot                    string   `json:"source_root"`
	WABTVersion                   string   `json:"wabt_version"`
	LegacyCoreSourceFilenamePaths []string `json:"legacy_core_source_filenames"`
	NormalizedWABTJSONFixtures    []string `json:"normalized_wabt_json_fixtures"`
	FixtureTreeSHA256             string   `json:"fixture_tree_sha256"`
}

const wasmtimeFixtureOutcomeMarker = "WAGO_WASMTIME_FIXTURE_OUTCOME="

type wasmtimeFixtureOutcome struct {
	ModulesPassed     int `json:"modules_passed"`
	ModulesSkipped    int `json:"modules_skipped"`
	ModulesFailed     int `json:"modules_failed"`
	AssertionsPassed  int `json:"assertions_passed"`
	AssertionsSkipped int `json:"assertions_skipped"`
	AssertionsFailed  int `json:"assertions_failed"`
}

func wasmtimeOutcomeFromStats(stats specExecStats) wasmtimeFixtureOutcome {
	return wasmtimeFixtureOutcome{
		ModulesPassed: stats.modulesPassed, ModulesSkipped: stats.modulesSkipped, ModulesFailed: stats.modulesFailed,
		AssertionsPassed: stats.assertionsPassed, AssertionsSkipped: stats.assertionsSkipped, AssertionsFailed: stats.assertionsFailed,
	}
}

func (o wasmtimeFixtureOutcome) stats() specExecStats {
	return specExecStats{
		modulesPassed: o.ModulesPassed, modulesSkipped: o.ModulesSkipped, modulesFailed: o.ModulesFailed,
		assertionsPassed: o.AssertionsPassed, assertionsSkipped: o.AssertionsSkipped, assertionsFailed: o.AssertionsFailed,
	}
}

func TestWasmtimePortCoreManifest(t *testing.T) {
	provenance := loadWasmtimeProvenance(t)
	if provenance.UpstreamRepo != "https://github.com/bytecodealliance/wasmtime.git" || provenance.SourceRoot != "tests/misc_testsuite" {
		t.Fatalf("unexpected Wasmtime provenance origin: %+v", provenance)
	}
	if revision, err := hex.DecodeString(provenance.Revision); err != nil || len(revision) != 20 {
		t.Fatalf("invalid Wasmtime revision %q", provenance.Revision)
	}
	if _, err := time.Parse("2006-01-02", provenance.RevisionDate); err != nil {
		t.Fatalf("invalid Wasmtime revision date %q: %v", provenance.RevisionDate, err)
	}
	versionParts := strings.Split(provenance.WABTVersion, ".")
	if len(versionParts) != 3 {
		t.Fatalf("invalid WABT version %q", provenance.WABTVersion)
	}
	for _, part := range versionParts {
		if _, err := strconv.ParseUint(part, 10, 32); err != nil {
			t.Fatalf("invalid WABT version %q", provenance.WABTVersion)
		}
	}
	fixtures := loadWasmtimeWasm2Manifest(t)
	if len(fixtures) != 104 {
		t.Fatalf("Wasmtime core manifest has %d entries, want 104", len(fixtures))
	}

	knownCoverage := map[string]bool{
		"branch-hinting":                true,
		"bulk-memory":                   true,
		"compile-link-workload":         true,
		"concurrent-instance-lifecycle": true,
		"malformed-validation":          true,
		"memory-reuse-bounds":           true,
		"multi-value":                   true,
		"nontrapping-float-to-int":      true,
		"reference-types":               true,
		"runtime-regression":            true,
		"sign-extension":                true,
		"simd":                          true,
	}
	modes := map[string]int{}
	coverage := map[string]int{}
	seen := map[string]bool{}
	modeByPath := map[string]string{}
	previous := ""
	for _, fixture := range fixtures {
		if fixture.path != filepath.ToSlash(filepath.Clean(fixture.path)) || strings.HasPrefix(fixture.path, "../") || !strings.HasSuffix(fixture.path, ".wast") {
			t.Errorf("invalid Wasmtime manifest path %q", fixture.path)
		}
		if previous != "" && fixture.path <= previous {
			t.Errorf("Wasmtime manifest paths are not strictly sorted: %q after %q", fixture.path, previous)
		}
		previous = fixture.path
		if seen[fixture.path] {
			t.Errorf("duplicate Wasmtime manifest path %q", fixture.path)
		}
		seen[fixture.path] = true
		modeByPath[fixture.path] = fixture.mode

		modes[fixture.mode]++
		for _, label := range strings.Split(fixture.coverage, ",") {
			if !knownCoverage[label] {
				t.Errorf("%s has unknown coverage label %q", fixture.path, label)
			}
			coverage[label]++
		}
		dir := wasmtimeWasm2FixtureDir(fixture.path)
		if _, err := os.Stat(filepath.Join(dir, "source.wast")); err != nil {
			t.Errorf("%s source fixture: %v", fixture.path, err)
		}
		switch fixture.mode {
		case "wast-json":
			if _, err := os.Stat(filepath.Join(dir, "commands.json")); err != nil {
				t.Errorf("%s command fixture: %v", fixture.path, err)
			}
			if wasm, err := filepath.Glob(filepath.Join(dir, "commands.*.wasm")); err != nil || len(wasm) == 0 {
				t.Errorf("%s generated modules = %v, %v; want at least one", fixture.path, wasm, err)
			}
		case "direct-go", "direct-invalid", "direct-concurrency":
			if wasm, err := filepath.Glob(filepath.Join(dir, "module.*.wasm")); err != nil || len(wasm) == 0 {
				t.Errorf("%s direct modules = %v, %v; want at least one", fixture.path, wasm, err)
			}
		default:
			t.Errorf("%s has unknown port mode %q", fixture.path, fixture.mode)
		}
	}
	for name, paths := range map[string][]string{
		"legacy core source filename": provenance.LegacyCoreSourceFilenamePaths,
		"normalized WABT JSON":        provenance.NormalizedWABTJSONFixtures,
	} {
		previous = ""
		for _, path := range paths {
			if path <= previous {
				t.Errorf("%s fixture paths are not strictly sorted: %q after %q", name, path, previous)
			}
			if modeByPath[path] != "wast-json" {
				t.Errorf("%s fixture path %q is not a wast-json fixture", name, path)
			}
			previous = path
		}
	}

	if modes["wast-json"] != 97 || modes["direct-go"] != 3 || modes["direct-invalid"] != 2 || modes["direct-concurrency"] != 2 || len(modes) != 4 {
		t.Fatalf("Wasmtime core port modes = %v, want wast-json=97 direct-go=3 direct-invalid=2 direct-concurrency=2", modes)
	}
	for _, feature := range []string{
		"sign-extension",
		"nontrapping-float-to-int",
		"multi-value",
		"reference-types",
		"bulk-memory",
		"simd",
		"branch-hinting",
	} {
		if coverage[feature] == 0 {
			t.Errorf("Wasmtime core manifest has no %s tests", feature)
		}
	}

	root := filepath.Clean("../../testdata/wasmtime/core")
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() != "source.wast" {
			return nil
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		manifestPath := filepath.ToSlash(rel) + ".wast"
		if !seen[manifestPath] {
			t.Errorf("orphan Wasmtime fixture source %q is not in MANIFEST.tsv", manifestPath)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestWasmtimeRustPortLedger(t *testing.T) {
	path := filepath.Clean("../../testdata/wasmtime/RUST_PORTS.tsv")
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	knownTests := map[string]bool{
		"TestWasmtimePortFailedInstantiationMemoryDoesNotLeak":      true,
		"TestWasmtimePortLargeAddChainDoesNotOverflowCompilerStack": true,
		"TestWasmtimePortMultiResultCallBoundaries":                 true,
		"TestWasmtimePortParallelValidationErrorIsDeterministic":    true,
		"TestWasmtimePortReusedFuncrefTableIsZeroed":                true,
		"TestWasmtimePortReusedMemoryIsZeroed":                      true,
		"TestWasmtimePortSameNamedImportDeclarationsRemainDistinct": true,
		"TestWasmtimePortTrapsSurviveConcurrentGoroutines":          true,
		"TestWasmtimePortV128TypedCallBoundaries":                   true,
	}
	var portSource strings.Builder
	for _, sourcePath := range []string{
		"wasmtime_api_port_test.go",
		"wasmtime_remaining_port_test.go",
	} {
		data, err := os.ReadFile(sourcePath)
		if err != nil {
			t.Fatal(err)
		}
		portSource.Write(data)
	}
	for testName := range knownTests {
		if !strings.Contains(portSource.String(), "func "+testName+"(") {
			t.Fatalf("Wasmtime Rust port ledger references missing Go test %s", testName)
		}
	}

	seenScopes := map[string]bool{}
	seenTests := map[string]bool{}
	previous := ""
	rows := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			t.Fatalf("malformed Wasmtime Rust port ledger line %q", line)
		}
		scope, testName, adaptation := fields[0], fields[1], fields[2]
		if previous != "" && scope <= previous {
			t.Errorf("Wasmtime Rust port scopes are not strictly sorted: %q after %q", scope, previous)
		}
		previous = scope
		if seenScopes[scope] {
			t.Errorf("duplicate Wasmtime Rust port scope %q", scope)
		}
		if seenTests[testName] {
			t.Errorf("duplicate Wasmtime local port test %q", testName)
		}
		seenScopes[scope], seenTests[testName] = true, true
		if !strings.HasPrefix(scope, "tests/all/") || !strings.Contains(scope, ".rs::") {
			t.Errorf("invalid Wasmtime Rust port scope %q", scope)
		}
		if !knownTests[testName] {
			t.Errorf("unknown Wasmtime local port test %q", testName)
		}
		if strings.TrimSpace(adaptation) == "" {
			t.Errorf("%s has no adaptation note", scope)
		}
		rows++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if rows != len(knownTests) || len(seenTests) != len(knownTests) {
		t.Fatalf("Wasmtime Rust port ledger has %d rows covering %d tests, want %d", rows, len(seenTests), len(knownTests))
	}
	for testName := range knownTests {
		if !seenTests[testName] {
			t.Errorf("Wasmtime Rust port ledger is missing %s", testName)
		}
	}
}

func TestWasmtimePortCoreFixtureTreeDigest(t *testing.T) {
	root := filepath.Clean("../../testdata/wasmtime/core")
	var paths []string
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	if len(paths) != 364 {
		t.Fatalf("Wasmtime core fixture tree has %d files, want 364", len(paths))
	}

	h := sha256.New()
	for _, rel := range paths {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		_, _ = h.Write([]byte(rel))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(data)
		_, _ = h.Write([]byte{0})
	}
	got := hex.EncodeToString(h.Sum(nil))
	want := loadWasmtimeProvenance(t).FixtureTreeSHA256
	if got != want {
		t.Fatalf("Wasmtime core fixture tree digest = %s, want %s", got, want)
	}
}

func TestWasmtimePortCoreWastExecution(t *testing.T) {
	if childFixture := os.Getenv("WAGO_WASMTIME_FIXTURE"); childFixture != "" {
		stats := runWasmtimeWastFixtureInProcess(t, childFixture)
		outcome, err := json.Marshal(wasmtimeOutcomeFromStats(stats))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Printf("%s%s\n", wasmtimeFixtureOutcomeMarker, outcome)
		return
	}

	var total specExecStats
	for _, fixture := range loadWasmtimeWasm2Manifest(t) {
		if fixture.mode != "wast-json" {
			continue
		}
		fixture := fixture
		t.Run(strings.TrimSuffix(fixture.path, ".wast"), func(t *testing.T) {
			sf := loadWasmtimeWastCommands(t, fixture.path)
			expected := expectedWasmtimeFixtureStats(t, fixture.path, sf)
			stats := runWasmtimeWastFixtureChild(t, fixture.path)
			if stats.modulesFailed != 0 || stats.modulesSkipped != 0 || stats.assertionsFailed != 0 || stats.assertionsSkipped != 0 {
				t.Fatalf("Wasmtime fixture stats = %+v, want no failures or skips", stats)
			}
			if stats.modulesPassed != expected.modulesPassed || stats.assertionsPassed != expected.assertionsPassed {
				t.Fatalf("Wasmtime fixture accounting = modules %d assertions %d, want %d and %d from commands.json", stats.modulesPassed, stats.assertionsPassed, expected.modulesPassed, expected.assertionsPassed)
			}
			total.add(stats)
		})
	}
	if total.modulesPassed != 141 || total.assertionsPassed != 535 {
		t.Fatalf("Wasmtime WAST totals = modules %d assertions %d, want 141 and 535", total.modulesPassed, total.assertionsPassed)
	}
}

func loadWasmtimeWastCommands(t *testing.T, fixture string) specExecFile {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(wasmtimeWasm2FixtureDir(fixture), "commands.json"))
	if err != nil {
		t.Fatal(err)
	}
	var sf specExecFile
	if err := json.Unmarshal(raw, &sf); err != nil {
		t.Fatalf("decode %s commands: %v", fixture, err)
	}
	return sf
}

func runWasmtimeWastFixtureInProcess(t *testing.T, fixture string) specExecStats {
	t.Helper()
	found := false
	for _, candidate := range loadWasmtimeWasm2Manifest(t) {
		if candidate.path == fixture && candidate.mode == "wast-json" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("unknown Wasmtime wast-json fixture %q", fixture)
	}
	dir := wasmtimeWasm2FixtureDir(fixture)
	return runSpecExecFile(t, fixture, dir, loadWasmtimeWastCommands(t, fixture))
}

func runWasmtimeWastFixtureChild(t *testing.T, fixture string) specExecStats {
	t.Helper()
	timeout := 20 * time.Second
	if raw := os.Getenv("WAGO_WASMTIME_TIMEOUT"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			t.Fatalf("invalid WAGO_WASMTIME_TIMEOUT %q", raw)
		}
		timeout = parsed
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWasmtimePortCoreWastExecution$", "-test.count=1")
	cmd.Env = append(os.Environ(), "WAGO_WASMTIME_FIXTURE="+fixture)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("Wasmtime fixture exceeded %s deadline\n%s", timeout, truncateWasmtimeChildOutput(out))
	}
	var outcome wasmtimeFixtureOutcome
	found := false
	for _, line := range strings.Split(string(out), "\n") {
		payload, ok := strings.CutPrefix(line, wasmtimeFixtureOutcomeMarker)
		if !ok {
			continue
		}
		if decodeErr := json.Unmarshal([]byte(payload), &outcome); decodeErr != nil {
			t.Fatalf("decode Wasmtime child outcome: %v\n%s", decodeErr, truncateWasmtimeChildOutput(out))
		}
		found = true
		break
	}
	if err != nil {
		t.Fatalf("Wasmtime fixture child failed: %v\n%s", err, truncateWasmtimeChildOutput(out))
	}
	if !found {
		t.Fatalf("Wasmtime fixture child exited without an outcome (native crash or harness failure)\n%s", truncateWasmtimeChildOutput(out))
	}
	stats := outcome.stats()
	if stats.modulesFailed != 0 || stats.modulesSkipped != 0 || stats.assertionsFailed != 0 || stats.assertionsSkipped != 0 {
		t.Fatalf("Wasmtime fixture child reported failures or skips: %+v\n%s", stats, truncateWasmtimeChildOutput(out))
	}
	return stats
}

func expectedWasmtimeFixtureStats(t *testing.T, fixture string, sf specExecFile) specExecStats {
	t.Helper()
	var expected specExecStats
	for _, command := range sf.Commands {
		switch command.Type {
		case "module":
			expected.modulesPassed++
		case "assert_return", "action", "assert_trap", "assert_exhaustion", "assert_uninstantiable", "assert_unlinkable":
			expected.assertionsPassed++
		case "register":
			// Registration changes subsequent module resolution but is not itself
			// an execution assertion.
		default:
			t.Fatalf("%s commands.json contains unaccounted command type %q at line %d", fixture, command.Type, command.Line)
		}
	}
	return expected
}

func truncateWasmtimeChildOutput(out []byte) string {
	const limit = 8 << 10
	if len(out) <= limit {
		return string(out)
	}
	return string(out[:limit]) + "\n... child output truncated ..."
}

func TestWasmtimePortBranchHints(t *testing.T) {
	in := instantiateWasmtimeWasm2DirectFixture(t, "branch-hinting.wast", 0, nil)
	for _, export := range []string{"via_if", "via_br_if"} {
		for _, tc := range []struct {
			arg  int32
			want int32
		}{{0, 20}, {1, 10}, {7, 10}, {-3, 10}} {
			got, err := in.Invoke(export, wago.I32(tc.arg))
			if err != nil || len(got) != 1 || wago.AsI32(got[0]) != tc.want {
				t.Fatalf("%s(%d) = %v, %v; want %d", export, tc.arg, got, err, tc.want)
			}
		}
	}
}

// Wasmtime ignores this malformed advisory section. Wago intentionally rejects
// malformed structured custom sections, so the applicable adaptation preserves
// the bytes while asserting Wago's documented strict-decode policy.
func TestWasmtimePortMalformedBranchHintIsRejected(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(wasmtimeWasm2FixtureDir("branch-hinting-invalid.wast"), "module.0.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := wago.Compile(nil, data)
	if compiled != nil {
		_ = compiled.Close()
	}
	if err == nil {
		t.Fatal("malformed metadata.code.branch_hint section compiled successfully")
	}
	if !strings.Contains(err.Error(), "invalid section") {
		t.Fatalf("malformed branch-hint error = %v", err)
	}
}

func TestWasmtimePortMalformedTrailingInstructions(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(wasmtimeWasm2FixtureDir("no-panic-on-invalid.wast"), "module.0.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := wago.Compile(nil, data)
	if compiled != nil {
		_ = compiled.Close()
		t.Fatal("malformed module with instructions after the function end compiled")
	}
	if err == nil {
		t.Fatal("malformed module with instructions after the function end was accepted")
	}
}

func TestWasmtimePortConcurrentInstanceDropOrder(t *testing.T) {
	firstCode := compileWasmtimeWasm2DirectFixture(t, "pooling-drop-out-of-order.wast", 0)
	defer firstCode.Close()
	threadCode := compileWasmtimeWasm2DirectFixture(t, "pooling-drop-out-of-order.wast", 1)
	defer threadCode.Close()

	for i := 0; i < 32; i++ {
		first, err := wago.Instantiate(firstCode, wago.InstantiateOptions{})
		if err != nil {
			t.Fatalf("iteration %d instantiate first module: %v", i, err)
		}
		assertWasmtimeResult(t, first, "load", []uint64{42})

		done := make(chan error, 1)
		go func() {
			in, err := wago.Instantiate(threadCode, wago.InstantiateOptions{})
			if err == nil {
				err = in.Close()
			}
			done <- err
		}()
		select {
		case err := <-done:
			if err != nil {
				_ = first.Close()
				t.Fatalf("iteration %d threaded instantiate/drop: %v", i, err)
			}
		case <-time.After(5 * time.Second):
			_ = first.Close()
			t.Fatalf("iteration %d threaded instantiate/drop timed out", i)
		}
		if err := first.Close(); err != nil {
			t.Fatalf("iteration %d close first module after threaded drop: %v", i, err)
		}
	}
}

func TestWasmtimePortReferenceTypesBasic(t *testing.T) {
	in := instantiateWasmtimeWasm2DirectFixture(t, "winch/ref-types-basic.wast", 0, nil)

	assertWasmtimeResult(t, in, "null-is-null", []uint64{1})
	assertWasmtimeResult(t, in, "func-is-not-null", []uint64{0})
	assertWasmtimeResult(t, in, "call-indirect-0", []uint64{42})
	assertWasmtimeResult(t, in, "call-indirect-1", []uint64{7})
	assertWasmtimeTrapCode(t, in, "call-indirect-null", wago.TrapIndirectOutOfBounds)

	f0 := assertWasmtimeSingleResult(t, in, "select-funcref", wago.I32(1))
	f1 := assertWasmtimeSingleResult(t, in, "select-funcref", wago.I32(0))
	if f0 == 0 || f1 == 0 || f0 == f1 {
		t.Fatalf("selected function identities = %#x/%#x, want distinct non-null refs", f0, f1)
	}
	assertWasmtimeResult(t, in, "select-null-first", []uint64{0}, wago.I32(1))
	assertWasmtimeResult(t, in, "select-null-first", []uint64{f1}, wago.I32(0))

	assertWasmtimeResult(t, in, "set-and-call", []uint64{42})
	assertWasmtimeResult(t, in, "table-get", []uint64{f0}, wago.I32(0))
	assertWasmtimeResult(t, in, "table-get", []uint64{f1}, wago.I32(1))
	assertWasmtimeResult(t, in, "table-get", []uint64{0}, wago.I32(2))
	assertWasmtimeTrapCode(t, in, "table-get", wago.TrapIndirectOutOfBounds, wago.I32(10))

	assertWasmtimeResult(t, in, "table-set-null", nil, wago.I32(0))
	assertWasmtimeResult(t, in, "table-get", []uint64{0}, wago.I32(0))
	assertWasmtimeResult(t, in, "table-set-ref", nil, wago.I32(0))
	assertWasmtimeResult(t, in, "table-get", []uint64{f0}, wago.I32(0))
	assertWasmtimeTrapCode(t, in, "table-set-null", wago.TrapIndirectOutOfBounds, wago.I32(10))

	assertWasmtimeResult(t, in, "table-size", []uint64{10})
	assertWasmtimeResult(t, in, "table-grow", []uint64{10}, wago.I32(5))
	assertWasmtimeResult(t, in, "table-size", []uint64{15})
}

func TestWasmtimePortTableFill(t *testing.T) {
	in := instantiateWasmtimeWasm2DirectFixture(t, "winch/table_fill.wast", 0, nil)
	get := func(index int32) uint64 {
		t.Helper()
		return assertWasmtimeSingleResult(t, in, "get", wago.I32(index))
	}
	fill := func(offset, source, count int32) {
		t.Helper()
		assertWasmtimeResult(t, in, "fill", nil, wago.I32(offset), wago.I32(source), wago.I32(count))
	}

	for i := int32(1); i <= 5; i++ {
		if got := get(i); got != 0 {
			t.Fatalf("initial table[%d] = %#x, want null", i, got)
		}
	}
	fill(2, 0, 3)
	if got := get(1); got != 0 {
		t.Fatalf("table[1] = %#x, want null", got)
	}
	f0 := get(2)
	if f0 == 0 || get(3) != f0 || get(4) != f0 || get(5) != 0 {
		t.Fatal("first table.fill did not write one exact function identity to [2,5)")
	}

	fill(4, 1, 2)
	f1 := get(4)
	if f1 == 0 || f1 == f0 || get(3) != f0 || get(5) != f1 || get(6) != 0 {
		t.Fatal("second table.fill did not preserve f0 and write a distinct f1")
	}
	fill(4, 2, 0)
	if get(3) != f0 || get(4) != f1 || get(5) != f1 {
		t.Fatal("zero-length table.fill mutated the table")
	}

	fill(8, 0, 2)
	if get(7) != 0 || get(8) != f0 || get(9) != f0 {
		t.Fatal("table.fill at the upper boundary produced the wrong entries")
	}
	fill(9, 2, 1)
	f2 := get(9)
	if get(8) != f0 || f2 == 0 || f2 == f0 || f2 == f1 {
		t.Fatal("table.fill did not preserve f0 and write a distinct f2")
	}
	fill(10, 1, 0)
	if get(9) != f2 {
		t.Fatal("zero-length fill at table.size mutated the final entry")
	}
	assertWasmtimeTrapCode(t, in, "fill", wago.TrapTableOutOfBounds, wago.I32(8), wago.I32(0), wago.I32(3))

	t.Run("imported and local tables", func(t *testing.T) {
		rt := wago.NewRuntime()
		t.Cleanup(func() { _ = rt.Close() })
		provider := instantiateWasmtimeWasm2RuntimeFixture(t, rt, "winch/table_fill.wast", 1, nil)
		table, err := provider.ExportedTable("t")
		if err != nil {
			t.Fatal(err)
		}
		consumer := instantiateWasmtimeWasm2RuntimeFixture(t, rt, "winch/table_fill.wast", 2, wago.Imports{"t.t": table})

		assertWasmtimeResult(t, consumer, "fill1", nil, wago.I32(0), 0, wago.I32(0))
		assertWasmtimeResult(t, consumer, "fill1", nil, wago.I32(0), 0, wago.I32(1))
		assertWasmtimeResult(t, consumer, "fill1", nil, wago.I32(1), 0, wago.I32(0))
		assertWasmtimeTrapCode(t, consumer, "fill1", wago.TrapTableOutOfBounds, wago.I32(2), 0, wago.I32(0))

		for _, args := range [][3]int32{{0, 0, 0}, {0, 0, 1}, {0, 0, 2}, {1, 0, 0}, {1, 0, 1}, {2, 0, 0}} {
			assertWasmtimeResult(t, consumer, "fill2", nil, wago.I32(args[0]), 0, wago.I32(args[2]))
		}
		assertWasmtimeTrapCode(t, consumer, "fill2", wago.TrapTableOutOfBounds, wago.I32(3), 0, wago.I32(0))
	})
}

func loadWasmtimeProvenance(t *testing.T) wasmtimeProvenance {
	t.Helper()
	data, err := os.ReadFile(filepath.Clean("../../testdata/wasmtime/PROVENANCE.json"))
	if err != nil {
		t.Fatal(err)
	}
	var provenance wasmtimeProvenance
	if err := json.Unmarshal(data, &provenance); err != nil {
		t.Fatalf("decode Wasmtime provenance: %v", err)
	}
	if provenance.UpstreamRepo == "" || provenance.Revision == "" || provenance.RevisionDate == "" || provenance.SourceRoot == "" || provenance.WABTVersion == "" || provenance.FixtureTreeSHA256 == "" {
		t.Fatalf("Wasmtime provenance has empty required fields: %+v", provenance)
	}
	return provenance
}

func loadWasmtimeWasm2Manifest(t *testing.T) []wasmtimeWasm2Fixture {
	t.Helper()
	path := filepath.Clean("../../testdata/wasmtime/MANIFEST.tsv")
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var fixtures []wasmtimeWasm2Fixture
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			t.Fatalf("malformed Wasmtime manifest line %q", line)
		}
		fixtures = append(fixtures, wasmtimeWasm2Fixture{path: fields[0], coverage: fields[1], mode: fields[2]})
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return fixtures
}

func wasmtimeWasm2FixtureDir(path string) string {
	return filepath.Clean(filepath.Join("../../testdata/wasmtime/core", strings.TrimSuffix(path, ".wast")))
}

func compileWasmtimeWasm2DirectFixture(t *testing.T, path string, module int) *wago.Compiled {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(wasmtimeWasm2FixtureDir(path), "module."+strconv.Itoa(module)+".wasm"))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := wago.Compile(nil, data)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func instantiateWasmtimeWasm2DirectFixture(t *testing.T, path string, module int, imports wago.Imports) *wago.Instance {
	t.Helper()
	compiled := compileWasmtimeWasm2DirectFixture(t, path, module)
	t.Cleanup(func() { _ = compiled.Close() })
	in, err := wago.Instantiate(compiled, wago.InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = in.Close() })
	return in
}

func instantiateWasmtimeWasm2RuntimeFixture(t *testing.T, rt *wago.Runtime, path string, module int, imports wago.Imports) *wago.Instance {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(wasmtimeWasm2FixtureDir(path), "module."+strconv.Itoa(module)+".wasm"))
	if err != nil {
		t.Fatal(err)
	}
	mod, err := rt.Compile(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mod.Compiled().Close() })
	in, err := rt.Instantiate(context.Background(), mod, wago.WithImports(imports))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = in.Close() })
	return in
}

func assertWasmtimeSingleResult(t *testing.T, in *wago.Instance, export string, args ...uint64) uint64 {
	t.Helper()
	got, err := in.Invoke(export, args...)
	if err != nil || len(got) != 1 {
		t.Fatalf("%s%v = %v, %v; want one result", export, args, got, err)
	}
	return got[0]
}

func assertWasmtimeResult(t *testing.T, in *wago.Instance, export string, want []uint64, args ...uint64) {
	t.Helper()
	got, err := in.Invoke(export, args...)
	if err != nil {
		t.Fatalf("%s%v: %v", export, args, err)
	}
	if len(got) != len(want) {
		t.Fatalf("%s%v returned %v, want %v", export, args, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s%v result[%d] = %#x, want %#x", export, args, i, got[i], want[i])
		}
	}
}

func assertWasmtimeTrapCode(t *testing.T, in *wago.Instance, export string, want wago.TrapCode, args ...uint64) {
	t.Helper()
	got, err := in.Invoke(export, args...)
	if err == nil {
		t.Fatalf("%s%v returned %v, want %s", export, args, got, want)
	}
	var trap *wago.TrapError
	if !errors.As(err, &trap) {
		t.Fatalf("%s%v returned non-trap error %v, want %s", export, args, err, want)
	}
	if trap.Code != want {
		t.Fatalf("%s%v trapped with %s, want %s", export, args, trap.Code, want)
	}
}
