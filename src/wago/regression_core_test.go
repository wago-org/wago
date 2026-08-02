//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago/src/wago"
	"github.com/wago-org/wago/tests/regressioncorpus"
	"github.com/wago-org/wago/tests/regressiontest"
)

type regressionCoreFixture struct {
	path     string
	coverage string
	mode     string
}

type regressionProvenance = regressioncorpus.Provenance

const (
	regressionFixtureOutcomeMarker = "WAGO_REGRESSION_FIXTURE_OUTCOME="
	regressionFixtureEnv           = "WAGO_REGRESSION_FIXTURE"
)

type regressionFixtureOutcome struct {
	Protocol          string `json:"protocol"`
	Fixture           string `json:"fixture"`
	Nonce             string `json:"nonce"`
	ModulesPassed     int    `json:"modules_passed"`
	ModulesSkipped    int    `json:"modules_skipped"`
	ModulesFailed     int    `json:"modules_failed"`
	AssertionsPassed  int    `json:"assertions_passed"`
	AssertionsSkipped int    `json:"assertions_skipped"`
	AssertionsFailed  int    `json:"assertions_failed"`
}

func regressionOutcomeFromStats(fixture, nonce string, stats specExecStats) regressionFixtureOutcome {
	return regressionFixtureOutcome{
		Protocol: regressiontest.Protocol, Fixture: fixture, Nonce: nonce,
		ModulesPassed: stats.modulesPassed, ModulesSkipped: stats.modulesSkipped, ModulesFailed: stats.modulesFailed,
		AssertionsPassed: stats.assertionsPassed, AssertionsSkipped: stats.assertionsSkipped, AssertionsFailed: stats.assertionsFailed,
	}
}

func (o regressionFixtureOutcome) stats() specExecStats {
	return specExecStats{
		modulesPassed: o.ModulesPassed, modulesSkipped: o.ModulesSkipped, modulesFailed: o.ModulesFailed,
		assertionsPassed: o.AssertionsPassed, assertionsSkipped: o.AssertionsSkipped, assertionsFailed: o.AssertionsFailed,
	}
}

func TestRuntimeRegressionPortCoreManifest(t *testing.T) {
	provenance := loadRegressionProvenance(t)
	if provenance.UpstreamRepo != "https://github.com/bytecodealliance/wasmtime.git" || provenance.SourceRoot != "tests/misc_testsuite" || provenance.WABTRepo != "https://github.com/WebAssembly/wabt.git" {
		t.Fatalf("unexpected Regression/WABT provenance origin: %+v", provenance)
	}
	fixtures := loadRegressionCoreManifest(t)
	if len(fixtures) != 104 {
		t.Fatalf("Regression core manifest has %d entries, want 104", len(fixtures))
	}

	modes := map[string]int{}
	coverage := map[string]int{}
	seen := map[string]bool{}
	for _, fixture := range fixtures {
		seen[fixture.path] = true
		modes[fixture.mode]++
		for _, label := range strings.Split(fixture.coverage, ",") {
			coverage[label]++
		}
		dir := regressionCoreFixtureDir(fixture.path)
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
		}
	}

	if modes["wast-json"] != 97 || modes["direct-go"] != 3 || modes["direct-invalid"] != 2 || modes["direct-concurrency"] != 2 || len(modes) != 4 {
		t.Fatalf("Regression core port modes = %v, want wast-json=97 direct-go=3 direct-invalid=2 direct-concurrency=2", modes)
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
			t.Errorf("Regression core manifest has no %s tests", feature)
		}
	}

	root := filepath.Clean("../../tests/regressions/runtime/core")
	parsed, err := regressioncorpus.LoadManifest(filepath.Clean("../../tests/regressions/runtime/MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := regressioncorpus.ValidateCorpusTree(root, parsed); err != nil {
		t.Fatalf("Regression corpus tree: %v", err)
	}
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
			t.Errorf("orphan Regression fixture source %q is not in MANIFEST.tsv", manifestPath)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeRegressionRustPortLedger(t *testing.T) {
	ports, err := regressioncorpus.LoadRustPorts(filepath.Clean("../../tests/regressions/runtime/RUST_PORTS.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	documented := discoverRegressionRustPortTests(t)
	ledger := map[string]map[string]bool{}
	for _, port := range ports {
		if ledger[port.LocalTest] == nil {
			ledger[port.LocalTest] = map[string]bool{}
		}
		ledger[port.LocalTest][port.Scope] = true
	}
	for testName, scopes := range documented {
		for scope := range scopes {
			if !ledger[testName][scope] {
				t.Errorf("Go documentation maps %s to %s, but RUST_PORTS.tsv does not", testName, scope)
			}
		}
	}
	for testName, scopes := range ledger {
		for scope := range scopes {
			if !documented[testName][scope] {
				t.Errorf("RUST_PORTS.tsv maps %s to %s, but its Go documentation does not", testName, scope)
			}
		}
	}
}

func discoverRegressionRustPortTests(t *testing.T) map[string]map[string]bool {
	t.Helper()
	ported := map[string]map[string]bool{}
	scopeRE := regexp.MustCompile(`tests/all/[A-Za-z0-9_./-]+\.rs::[A-Za-z_][A-Za-z0-9_]*`)
	matches, err := filepath.Glob("regression_*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, sourcePath := range matches {
		file, err := parser.ParseFile(token.NewFileSet(), sourcePath, nil, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, "TestRuntimeRegressionPort") || fn.Doc == nil {
				continue
			}
			scopes := scopeRE.FindAllString(fn.Doc.Text(), -1)
			if len(scopes) == 0 {
				continue
			}
			if ported[fn.Name.Name] == nil {
				ported[fn.Name.Name] = map[string]bool{}
			}
			for _, scope := range scopes {
				ported[fn.Name.Name][scope] = true
			}
		}
	}
	if len(ported) == 0 {
		t.Fatal("no Go tests documented as Regression tests/all ports")
	}
	return ported
}

func TestRuntimeRegressionUpstreamInventoryLedger(t *testing.T) {
	entries, err := regressioncorpus.LoadInventory(filepath.Clean("../../tests/regressions/runtime/UPSTREAM_INVENTORY.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	upstream := make([]string, len(entries))
	statusCounts := map[string]int{}
	for i, entry := range entries {
		upstream[i] = entry.Path
		statusCounts[entry.Status]++
	}
	parsed, err := regressioncorpus.LoadManifest(filepath.Clean("../../tests/regressions/runtime/MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := regressioncorpus.ValidateInventory(entries, parsed, upstream); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 324 || statusCounts[regressioncorpus.InventoryPorted] != 104 || statusCounts[regressioncorpus.InventoryExcluded] != 4 || statusCounts[regressioncorpus.InventoryOutOfScope] != 216 {
		t.Fatalf("Regression upstream inventory = total %d statuses %v, want total=324 ported=104 excluded=4 out-of-scope=216", len(entries), statusCounts)
	}
}

func TestRuntimeRegressionDirectArtifactLedger(t *testing.T) {
	entries, err := regressioncorpus.LoadDirectArtifacts(filepath.Clean("../../tests/regressions/runtime/DIRECT_ARTIFACTS.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	directModes := map[string]bool{
		regressioncorpus.ModeDirectGo:          true,
		regressioncorpus.ModeDirectInvalid:     true,
		regressioncorpus.ModeDirectConcurrency: true,
	}
	fixtureModes := map[string]string{}
	for _, fixture := range loadRegressionCoreManifest(t) {
		fixtureModes[fixture.path] = fixture.mode
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		if !directModes[fixtureModes[entry.Path]] {
			t.Errorf("direct artifact ledger path %q is not a direct fixture", entry.Path)
		}
		dir := regressionCoreFixtureDir(entry.Path)
		sourceDigest, err := regressioncorpus.FileSHA256(filepath.Join(dir, "source.wast"))
		if err != nil {
			t.Fatal(err)
		}
		artifactDigest, err := regressioncorpus.DirectArtifactsSHA256(dir)
		if err != nil {
			t.Fatal(err)
		}
		if sourceDigest != entry.SourceSHA256 || artifactDigest != entry.ArtifactsSHA256 {
			t.Errorf("%s direct hashes = source %s artifacts %s, want %s and %s", entry.Path, sourceDigest, artifactDigest, entry.SourceSHA256, entry.ArtifactsSHA256)
		}
		seen[entry.Path] = true
	}
	for path, mode := range fixtureModes {
		if directModes[mode] && !seen[path] {
			t.Errorf("direct artifact ledger is missing %s", path)
		}
	}
}

func TestRuntimeRegressionPortCoreFixtureTreeDigest(t *testing.T) {
	root := filepath.Clean("../../tests/regressions/runtime/core")
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
		t.Fatalf("Regression core fixture tree has %d files, want 364", len(paths))
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
	want := loadRegressionProvenance(t).FixtureTreeSHA256
	if got != want {
		t.Fatalf("Regression core fixture tree digest = %s, want %s", got, want)
	}
}

func TestRuntimeRegressionPortCoreWastExecution(t *testing.T) {
	if childFixture := os.Getenv(regressionFixtureEnv); childFixture != "" {
		nonce := regressiontest.RequireProtocol(t)
		stats := runRegressionWastFixtureInProcess(t, childFixture)
		outcome, err := json.Marshal(regressionOutcomeFromStats(childFixture, nonce, stats))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Printf("%s%s\n", regressionFixtureOutcomeMarker, outcome)
		return
	}

	var total specExecStats
	for _, fixture := range loadRegressionCoreManifest(t) {
		if fixture.mode != "wast-json" {
			continue
		}
		fixture := fixture
		t.Run(strings.TrimSuffix(fixture.path, ".wast"), func(t *testing.T) {
			sf := loadRegressionWastCommands(t, fixture.path)
			expected := expectedRegressionFixtureStats(t, fixture.path, sf)
			stats := runRegressionWastFixtureChild(t, fixture.path)
			if stats.modulesFailed != 0 || stats.modulesSkipped != 0 || stats.assertionsFailed != 0 || stats.assertionsSkipped != 0 {
				t.Fatalf("Regression fixture stats = %+v, want no failures or skips", stats)
			}
			if stats.modulesPassed != expected.modulesPassed || stats.assertionsPassed != expected.assertionsPassed {
				t.Fatalf("Regression fixture accounting = modules %d assertions %d, want %d and %d from commands.json", stats.modulesPassed, stats.assertionsPassed, expected.modulesPassed, expected.assertionsPassed)
			}
			total.add(stats)
		})
	}
	if total.modulesPassed != 141 || total.assertionsPassed != 535 {
		t.Fatalf("Regression WAST totals = modules %d assertions %d, want 141 and 535", total.modulesPassed, total.assertionsPassed)
	}
}

func loadRegressionWastCommands(t *testing.T, fixture string) specExecFile {
	t.Helper()
	dir := regressionCoreFixtureDir(fixture)
	raw, err := os.ReadFile(filepath.Join(dir, "commands.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := regressioncorpus.ValidateWASTJSONFixture(dir); err != nil {
		t.Fatalf("validate %s command graph: %v", fixture, err)
	}
	var sf specExecFile
	if err := regressiontest.DecodeStrictJSON(raw, &sf); err != nil {
		t.Fatalf("decode %s commands strictly: %v", fixture, err)
	}
	return sf
}

func runRegressionWastFixtureInProcess(t *testing.T, fixture string) specExecStats {
	t.Helper()
	found := false
	for _, candidate := range loadRegressionCoreManifest(t) {
		if candidate.path == fixture && candidate.mode == "wast-json" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("unknown Regression wast-json fixture %q", fixture)
	}
	dir := regressionCoreFixtureDir(fixture)
	return runSpecExecFile(t, fixture, dir, loadRegressionWastCommands(t, fixture))
}

func runRegressionIsolatedPortTest(t *testing.T) bool {
	t.Helper()
	return regressiontest.RunIsolated(t, regressiontest.Timeout(t, 30*time.Second))
}

func runRegressionWastFixtureChild(t *testing.T, fixture string) specExecStats {
	t.Helper()
	timeout := regressiontest.Timeout(t, 20*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	nonce := regressiontest.NewNonce(t)
	args := []string{"-test.run=^TestRuntimeRegressionPortCoreWastExecution$", "-test.count=1"}
	args = append(args, regressiontest.CoverageArgs()...)
	cmd := exec.CommandContext(ctx, os.Args[0], args...)
	regressiontest.PrepareCommand(cmd)
	cmd.Env = regressiontest.ChildEnvironment(map[string]string{
		regressionFixtureEnv:       fixture,
		regressiontest.ProtocolEnv: regressiontest.Protocol,
		regressiontest.NonceEnv:    nonce,
		"WAGO_BOUNDS":              regressiontest.ExpectedBounds,
	})
	capture := regressiontest.NewCapture(8<<10, regressionFixtureOutcomeMarker)
	cmd.Stdout, cmd.Stderr = capture, capture
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("Regression fixture exceeded %s deadline\n%s", timeout, capture.Output())
	}
	markers := capture.Markers()
	if err != nil {
		t.Fatalf("Regression fixture child failed: %v\n%s", err, capture.Output())
	}
	if len(markers) != 1 {
		t.Fatalf("Regression fixture child emitted %d outcomes, want exactly one\n%s", len(markers), capture.Output())
	}
	var outcome regressionFixtureOutcome
	if decodeErr := regressiontest.DecodeStrictJSON([]byte(markers[0]), &outcome); decodeErr != nil {
		t.Fatalf("decode Regression child outcome: %v\n%s", decodeErr, capture.Output())
	}
	if outcome.Protocol != regressiontest.Protocol || outcome.Fixture != fixture || outcome.Nonce != nonce {
		t.Fatalf("Regression fixture child outcome identity = protocol %q fixture %q nonce %q, want %q %q %q", outcome.Protocol, outcome.Fixture, outcome.Nonce, regressiontest.Protocol, fixture, nonce)
	}
	stats := outcome.stats()
	if stats.modulesFailed != 0 || stats.modulesSkipped != 0 || stats.assertionsFailed != 0 || stats.assertionsSkipped != 0 {
		t.Fatalf("Regression fixture child reported failures or skips: %+v\n%s", stats, capture.Output())
	}
	return stats
}

func expectedRegressionFixtureStats(t *testing.T, fixture string, sf specExecFile) specExecStats {
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

func TestRuntimeRegressionPortBranchHints(t *testing.T) {
	if runRegressionIsolatedPortTest(t) {
		return
	}
	in := instantiateRegressionCoreDirectFixture(t, "branch-hinting.wast", 0, nil)
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

// Regression ignores this malformed advisory section. Wago intentionally rejects
// malformed structured custom sections, so the applicable adaptation preserves
// the bytes while asserting Wago's documented strict-decode policy.
func TestRuntimeRegressionPortMalformedBranchHintIsRejected(t *testing.T) {
	if runRegressionIsolatedPortTest(t) {
		return
	}
	data, err := os.ReadFile(filepath.Join(regressionCoreFixtureDir("branch-hinting-invalid.wast"), "module.0.wasm"))
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

func TestRuntimeRegressionPortMalformedTrailingInstructions(t *testing.T) {
	if runRegressionIsolatedPortTest(t) {
		return
	}
	data, err := os.ReadFile(filepath.Join(regressionCoreFixtureDir("no-panic-on-invalid.wast"), "module.0.wasm"))
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

func TestRuntimeRegressionPortConcurrentInstanceDropOrder(t *testing.T) {
	if runRegressionIsolatedPortTest(t) {
		return
	}
	firstCode := compileRegressionCoreDirectFixture(t, "pooling-drop-out-of-order.wast", 0)
	defer firstCode.Close()
	threadCode := compileRegressionCoreDirectFixture(t, "pooling-drop-out-of-order.wast", 1)
	defer threadCode.Close()

	for i := 0; i < 32; i++ {
		first, err := wago.Instantiate(firstCode, wago.InstantiateOptions{})
		if err != nil {
			t.Fatalf("iteration %d instantiate first module: %v", i, err)
		}
		assertRegressionResult(t, first, "load", []uint64{42})

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

func TestRuntimeRegressionPortReferenceTypesBasic(t *testing.T) {
	if runRegressionIsolatedPortTest(t) {
		return
	}
	in := instantiateRegressionCoreDirectFixture(t, "winch/ref-types-basic.wast", 0, nil)

	assertRegressionResult(t, in, "null-is-null", []uint64{1})
	assertRegressionResult(t, in, "func-is-not-null", []uint64{0})
	assertRegressionResult(t, in, "call-indirect-0", []uint64{42})
	assertRegressionResult(t, in, "call-indirect-1", []uint64{7})
	assertRegressionTrapCode(t, in, "call-indirect-null", wago.TrapIndirectOutOfBounds)

	f0 := assertRegressionSingleResult(t, in, "select-funcref", wago.I32(1))
	f1 := assertRegressionSingleResult(t, in, "select-funcref", wago.I32(0))
	if f0 == 0 || f1 == 0 || f0 == f1 {
		t.Fatalf("selected function identities = %#x/%#x, want distinct non-null refs", f0, f1)
	}
	assertRegressionResult(t, in, "select-null-first", []uint64{0}, wago.I32(1))
	assertRegressionResult(t, in, "select-null-first", []uint64{f1}, wago.I32(0))

	assertRegressionResult(t, in, "set-and-call", []uint64{42})
	assertRegressionResult(t, in, "table-get", []uint64{f0}, wago.I32(0))
	assertRegressionResult(t, in, "table-get", []uint64{f1}, wago.I32(1))
	assertRegressionResult(t, in, "table-get", []uint64{0}, wago.I32(2))
	assertRegressionTrapCode(t, in, "table-get", wago.TrapTableOutOfBounds, wago.I32(10))

	assertRegressionResult(t, in, "table-set-null", nil, wago.I32(0))
	assertRegressionResult(t, in, "table-get", []uint64{0}, wago.I32(0))
	assertRegressionResult(t, in, "table-set-ref", nil, wago.I32(0))
	assertRegressionResult(t, in, "table-get", []uint64{f0}, wago.I32(0))
	assertRegressionTrapCode(t, in, "table-set-null", wago.TrapTableOutOfBounds, wago.I32(10))

	assertRegressionResult(t, in, "table-size", []uint64{10})
	assertRegressionResult(t, in, "table-grow", []uint64{10}, wago.I32(5))
	assertRegressionResult(t, in, "table-size", []uint64{15})
}

func TestRuntimeRegressionPortTableFill(t *testing.T) {
	if runRegressionIsolatedPortTest(t) {
		return
	}
	in := instantiateRegressionCoreDirectFixture(t, "winch/table_fill.wast", 0, nil)
	get := func(index int32) uint64 {
		t.Helper()
		return assertRegressionSingleResult(t, in, "get", wago.I32(index))
	}
	fill := func(offset, source, count int32) {
		t.Helper()
		assertRegressionResult(t, in, "fill", nil, wago.I32(offset), wago.I32(source), wago.I32(count))
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
	assertRegressionTrapCode(t, in, "fill", wago.TrapTableOutOfBounds, wago.I32(8), wago.I32(0), wago.I32(3))

	t.Run("imported and local tables", func(t *testing.T) {
		rt := wago.NewRuntime()
		t.Cleanup(func() { _ = rt.Close() })
		provider := instantiateRegressionCoreRuntimeFixture(t, rt, "winch/table_fill.wast", 1, nil)
		table, err := provider.ExportedTable("t")
		if err != nil {
			t.Fatal(err)
		}
		consumer := instantiateRegressionCoreRuntimeFixture(t, rt, "winch/table_fill.wast", 2, wago.Imports{"t.t": table})

		assertRegressionResult(t, consumer, "fill1", nil, wago.I32(0), 0, wago.I32(0))
		assertRegressionResult(t, consumer, "fill1", nil, wago.I32(0), 0, wago.I32(1))
		assertRegressionResult(t, consumer, "fill1", nil, wago.I32(1), 0, wago.I32(0))
		assertRegressionTrapCode(t, consumer, "fill1", wago.TrapTableOutOfBounds, wago.I32(2), 0, wago.I32(0))

		for _, args := range [][3]int32{{0, 0, 0}, {0, 0, 1}, {0, 0, 2}, {1, 0, 0}, {1, 0, 1}, {2, 0, 0}} {
			assertRegressionResult(t, consumer, "fill2", nil, wago.I32(args[0]), 0, wago.I32(args[2]))
		}
		assertRegressionTrapCode(t, consumer, "fill2", wago.TrapTableOutOfBounds, wago.I32(3), 0, wago.I32(0))
	})
}

func loadRegressionProvenance(t *testing.T) regressionProvenance {
	t.Helper()
	provenance, err := regressioncorpus.LoadProvenance(filepath.Clean("../../tests/regressions/runtime/PROVENANCE.json"))
	if err != nil {
		t.Fatal(err)
	}
	return provenance
}

func loadRegressionCoreManifest(t *testing.T) []regressionCoreFixture {
	t.Helper()
	parsed, err := regressioncorpus.LoadManifest(filepath.Clean("../../tests/regressions/runtime/MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := regressioncorpus.ValidateProvenanceFixtureSets(loadRegressionProvenance(t), parsed); err != nil {
		t.Fatal(err)
	}
	fixtures := make([]regressionCoreFixture, len(parsed))
	for i, fixture := range parsed {
		fixtures[i] = regressionCoreFixture{path: fixture.Path, coverage: fixture.Coverage, mode: fixture.Mode}
	}
	return fixtures
}

func regressionCoreFixtureDir(path string) string {
	return filepath.Clean(filepath.Join("../../tests/regressions/runtime/core", strings.TrimSuffix(path, ".wast")))
}

func compileRegressionCoreDirectFixture(t *testing.T, path string, module int) *wago.Compiled {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(regressionCoreFixtureDir(path), "module."+strconv.Itoa(module)+".wasm"))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := wago.Compile(nil, data)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func instantiateRegressionCoreDirectFixture(t *testing.T, path string, module int, imports wago.Imports) *wago.Instance {
	t.Helper()
	compiled := compileRegressionCoreDirectFixture(t, path, module)
	t.Cleanup(func() { _ = compiled.Close() })
	in, err := wago.Instantiate(compiled, wago.InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = in.Close() })
	return in
}

func instantiateRegressionCoreRuntimeFixture(t *testing.T, rt *wago.Runtime, path string, module int, imports wago.Imports) *wago.Instance {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(regressionCoreFixtureDir(path), "module."+strconv.Itoa(module)+".wasm"))
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

func assertRegressionSingleResult(t *testing.T, in *wago.Instance, export string, args ...uint64) uint64 {
	t.Helper()
	got, err := in.Invoke(export, args...)
	if err != nil || len(got) != 1 {
		t.Fatalf("%s%v = %v, %v; want one result", export, args, got, err)
	}
	return got[0]
}

func assertRegressionResult(t *testing.T, in *wago.Instance, export string, want []uint64, args ...uint64) {
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

func assertRegressionTrapCode(t *testing.T, in *wago.Instance, export string, want wago.TrapCode, args ...uint64) {
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
