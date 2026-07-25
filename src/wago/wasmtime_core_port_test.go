//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo

package wago_test

import (
	"context"
	"crypto/rand"
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

	"github.com/wago-org/wago/internal/wasmtimecorpus"
	"github.com/wago-org/wago/src/wago"
)

type wasmtimeCoreFixture struct {
	path     string
	coverage string
	mode     string
}

type wasmtimeProvenance = wasmtimecorpus.Provenance

const (
	wasmtimeFixtureOutcomeMarker = "WAGO_WASMTIME_FIXTURE_OUTCOME="
	wasmtimeChildProtocol        = "1"
	wasmtimeFixtureEnv           = "WAGO_WASMTIME_FIXTURE"
	wasmtimePortTestEnv          = "WAGO_WASMTIME_PORT_TEST"
	wasmtimeChildProtocolEnv     = "WAGO_WASMTIME_CHILD_PROTOCOL"
	wasmtimeChildNonceEnv        = "WAGO_WASMTIME_CHILD_NONCE"
)

type wasmtimeFixtureOutcome struct {
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

func wasmtimeOutcomeFromStats(fixture, nonce string, stats specExecStats) wasmtimeFixtureOutcome {
	return wasmtimeFixtureOutcome{
		Protocol: wasmtimeChildProtocol, Fixture: fixture, Nonce: nonce,
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
	if provenance.UpstreamRepo != "https://github.com/bytecodealliance/wasmtime.git" || provenance.SourceRoot != "tests/misc_testsuite" || provenance.WABTRepo != "https://github.com/WebAssembly/wabt.git" {
		t.Fatalf("unexpected Wasmtime/WABT provenance origin: %+v", provenance)
	}
	fixtures := loadWasmtimeCoreManifest(t)
	if len(fixtures) != 104 {
		t.Fatalf("Wasmtime core manifest has %d entries, want 104", len(fixtures))
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
		dir := wasmtimeCoreFixtureDir(fixture.path)
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
	ports, err := wasmtimecorpus.LoadRustPorts(filepath.Clean("../../testdata/wasmtime/RUST_PORTS.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	knownTests := discoverWasmtimeRustPortTests(t)

	seenTests := map[string]bool{}
	for _, port := range ports {
		if !knownTests[port.LocalTest] {
			t.Errorf("unknown Wasmtime local port test %q", port.LocalTest)
		}
		seenTests[port.LocalTest] = true
	}
	if len(seenTests) != len(knownTests) {
		t.Fatalf("Wasmtime Rust port ledger covers %d local tests, want %d", len(seenTests), len(knownTests))
	}
	for testName := range knownTests {
		if !seenTests[testName] {
			t.Errorf("Wasmtime Rust port ledger is missing %s", testName)
		}
	}
}

func discoverWasmtimeRustPortTests(t *testing.T) map[string]bool {
	t.Helper()
	ported := map[string]bool{}
	matches, err := filepath.Glob("wasmtime_*_port_test.go")
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
			if !ok || fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, "TestWasmtimePort") || fn.Doc == nil {
				continue
			}
			if strings.Contains(fn.Doc.Text(), "tests/all/") {
				ported[fn.Name.Name] = true
			}
		}
	}
	if len(ported) == 0 {
		t.Fatal("no Go tests documented as Wasmtime tests/all ports")
	}
	return ported
}

func TestWasmtimeDirectArtifactLedger(t *testing.T) {
	entries, err := wasmtimecorpus.LoadDirectArtifacts(filepath.Clean("../../testdata/wasmtime/DIRECT_ARTIFACTS.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	directModes := map[string]bool{
		wasmtimecorpus.ModeDirectGo:          true,
		wasmtimecorpus.ModeDirectInvalid:     true,
		wasmtimecorpus.ModeDirectConcurrency: true,
	}
	fixtureModes := map[string]string{}
	for _, fixture := range loadWasmtimeCoreManifest(t) {
		fixtureModes[fixture.path] = fixture.mode
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		if !directModes[fixtureModes[entry.Path]] {
			t.Errorf("direct artifact ledger path %q is not a direct fixture", entry.Path)
		}
		dir := wasmtimeCoreFixtureDir(entry.Path)
		sourceDigest, err := wasmtimecorpus.FileSHA256(filepath.Join(dir, "source.wast"))
		if err != nil {
			t.Fatal(err)
		}
		artifactDigest, err := wasmtimecorpus.DirectArtifactsSHA256(dir)
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
	if childFixture := os.Getenv(wasmtimeFixtureEnv); childFixture != "" {
		nonce := requireWasmtimeChildProtocol(t)
		stats := runWasmtimeWastFixtureInProcess(t, childFixture)
		outcome, err := json.Marshal(wasmtimeOutcomeFromStats(childFixture, nonce, stats))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Printf("%s%s\n", wasmtimeFixtureOutcomeMarker, outcome)
		return
	}

	var total specExecStats
	for _, fixture := range loadWasmtimeCoreManifest(t) {
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
			if testing.CoverMode() != "" {
				covered := runWasmtimeWastFixtureInProcess(t, fixture.path)
				if covered.modulesPassed != stats.modulesPassed || covered.assertionsPassed != stats.assertionsPassed || covered.modulesFailed != 0 || covered.assertionsFailed != 0 || covered.modulesSkipped != 0 || covered.assertionsSkipped != 0 {
					t.Fatalf("coverage replay stats = %+v, isolated stats = %+v", covered, stats)
				}
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
	raw, err := os.ReadFile(filepath.Join(wasmtimeCoreFixtureDir(fixture), "commands.json"))
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
	for _, candidate := range loadWasmtimeCoreManifest(t) {
		if candidate.path == fixture && candidate.mode == "wast-json" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("unknown Wasmtime wast-json fixture %q", fixture)
	}
	dir := wasmtimeCoreFixtureDir(fixture)
	return runSpecExecFile(t, fixture, dir, loadWasmtimeWastCommands(t, fixture))
}

func requireWasmtimeChildProtocol(t *testing.T) string {
	t.Helper()
	if got := os.Getenv(wasmtimeChildProtocolEnv); got != wasmtimeChildProtocol {
		t.Fatalf("invalid Wasmtime child protocol %q", got)
	}
	nonce := os.Getenv(wasmtimeChildNonceEnv)
	decoded, err := hex.DecodeString(nonce)
	if err != nil || len(decoded) != 16 {
		t.Fatalf("invalid Wasmtime child nonce %q", nonce)
	}
	return nonce
}

func newWasmtimeChildNonce(t *testing.T) string {
	t.Helper()
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(nonce[:])
}

func wasmtimeChildEnvironment(overrides map[string]string) []string {
	blocked := map[string]bool{
		wasmtimeFixtureEnv:       true,
		wasmtimePortTestEnv:      true,
		wasmtimeChildProtocolEnv: true,
		wasmtimeChildNonceEnv:    true,
	}
	env := make([]string, 0, len(os.Environ())+len(overrides))
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if !blocked[key] {
			env = append(env, item)
		}
	}
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		env = append(env, key+"="+overrides[key])
	}
	return env
}

func wasmtimeTestTimeout(t *testing.T, fallback time.Duration) time.Duration {
	t.Helper()
	if raw := os.Getenv("WAGO_WASMTIME_TIMEOUT"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			t.Fatalf("invalid WAGO_WASMTIME_TIMEOUT %q", raw)
		}
		return parsed
	}
	return fallback
}

func runWasmtimeIsolatedPortTest(t *testing.T) bool {
	t.Helper()
	if target := os.Getenv(wasmtimePortTestEnv); target != "" {
		requireWasmtimeChildProtocol(t)
		if target != t.Name() {
			t.Skip("different isolated Wasmtime subtest")
			return true
		}
		return false
	}

	timeout := wasmtimeTestTimeout(t, 30*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	top := strings.SplitN(t.Name(), "/", 2)[0]
	nonce := newWasmtimeChildNonce(t)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+regexp.QuoteMeta(top)+"$", "-test.count=1")
	cmd.Env = wasmtimeChildEnvironment(map[string]string{
		wasmtimePortTestEnv:      t.Name(),
		wasmtimeChildProtocolEnv: wasmtimeChildProtocol,
		wasmtimeChildNonceEnv:    nonce,
	})
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("isolated Wasmtime test exceeded %s deadline\n%s", timeout, truncateWasmtimeChildOutput(out))
	}
	if err != nil {
		t.Fatalf("isolated Wasmtime test failed: %v\n%s", err, truncateWasmtimeChildOutput(out))
	}
	return testing.CoverMode() == ""
}

func TestWasmtimeChildEnvironmentReplacesProtocolKeys(t *testing.T) {
	for _, key := range []string{wasmtimeFixtureEnv, wasmtimePortTestEnv, wasmtimeChildProtocolEnv, wasmtimeChildNonceEnv} {
		t.Setenv(key, "stale")
	}
	env := wasmtimeChildEnvironment(map[string]string{
		wasmtimeFixtureEnv:       "add.wast",
		wasmtimeChildProtocolEnv: wasmtimeChildProtocol,
		wasmtimeChildNonceEnv:    strings.Repeat("a", 32),
	})
	counts := map[string]int{}
	values := map[string]string{}
	for _, item := range env {
		key, value, _ := strings.Cut(item, "=")
		if key == wasmtimeFixtureEnv || key == wasmtimePortTestEnv || key == wasmtimeChildProtocolEnv || key == wasmtimeChildNonceEnv {
			counts[key]++
			values[key] = value
		}
	}
	if counts[wasmtimeFixtureEnv] != 1 || values[wasmtimeFixtureEnv] != "add.wast" ||
		counts[wasmtimePortTestEnv] != 0 || counts[wasmtimeChildProtocolEnv] != 1 || counts[wasmtimeChildNonceEnv] != 1 {
		t.Fatalf("child protocol environment counts=%v values=%v", counts, values)
	}
}

func runWasmtimeWastFixtureChild(t *testing.T, fixture string) specExecStats {
	t.Helper()
	timeout := wasmtimeTestTimeout(t, 20*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	nonce := newWasmtimeChildNonce(t)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWasmtimePortCoreWastExecution$", "-test.count=1")
	cmd.Env = wasmtimeChildEnvironment(map[string]string{
		wasmtimeFixtureEnv:       fixture,
		wasmtimeChildProtocolEnv: wasmtimeChildProtocol,
		wasmtimeChildNonceEnv:    nonce,
	})
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
		if found {
			t.Fatalf("Wasmtime fixture child emitted duplicate outcomes\n%s", truncateWasmtimeChildOutput(out))
		}
		found = true
	}
	if err != nil {
		t.Fatalf("Wasmtime fixture child failed: %v\n%s", err, truncateWasmtimeChildOutput(out))
	}
	if !found {
		t.Fatalf("Wasmtime fixture child exited without an outcome (native crash or harness failure)\n%s", truncateWasmtimeChildOutput(out))
	}
	if outcome.Protocol != wasmtimeChildProtocol || outcome.Fixture != fixture || outcome.Nonce != nonce {
		t.Fatalf("Wasmtime fixture child outcome identity = protocol %q fixture %q nonce %q, want %q %q %q", outcome.Protocol, outcome.Fixture, outcome.Nonce, wasmtimeChildProtocol, fixture, nonce)
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
	half := limit / 2
	return string(out[:half]) + "\n... child output truncated ...\n" + string(out[len(out)-half:])
}

func TestWasmtimePortBranchHints(t *testing.T) {
	if runWasmtimeIsolatedPortTest(t) {
		return
	}
	in := instantiateWasmtimeCoreDirectFixture(t, "branch-hinting.wast", 0, nil)
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
	if runWasmtimeIsolatedPortTest(t) {
		return
	}
	data, err := os.ReadFile(filepath.Join(wasmtimeCoreFixtureDir("branch-hinting-invalid.wast"), "module.0.wasm"))
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
	if runWasmtimeIsolatedPortTest(t) {
		return
	}
	data, err := os.ReadFile(filepath.Join(wasmtimeCoreFixtureDir("no-panic-on-invalid.wast"), "module.0.wasm"))
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
	if runWasmtimeIsolatedPortTest(t) {
		return
	}
	firstCode := compileWasmtimeCoreDirectFixture(t, "pooling-drop-out-of-order.wast", 0)
	defer firstCode.Close()
	threadCode := compileWasmtimeCoreDirectFixture(t, "pooling-drop-out-of-order.wast", 1)
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
	if runWasmtimeIsolatedPortTest(t) {
		return
	}
	in := instantiateWasmtimeCoreDirectFixture(t, "winch/ref-types-basic.wast", 0, nil)

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
	assertWasmtimeTrapCode(t, in, "table-get", wago.TrapTableOutOfBounds, wago.I32(10))

	assertWasmtimeResult(t, in, "table-set-null", nil, wago.I32(0))
	assertWasmtimeResult(t, in, "table-get", []uint64{0}, wago.I32(0))
	assertWasmtimeResult(t, in, "table-set-ref", nil, wago.I32(0))
	assertWasmtimeResult(t, in, "table-get", []uint64{f0}, wago.I32(0))
	assertWasmtimeTrapCode(t, in, "table-set-null", wago.TrapTableOutOfBounds, wago.I32(10))

	assertWasmtimeResult(t, in, "table-size", []uint64{10})
	assertWasmtimeResult(t, in, "table-grow", []uint64{10}, wago.I32(5))
	assertWasmtimeResult(t, in, "table-size", []uint64{15})
}

func TestWasmtimePortTableFill(t *testing.T) {
	if runWasmtimeIsolatedPortTest(t) {
		return
	}
	in := instantiateWasmtimeCoreDirectFixture(t, "winch/table_fill.wast", 0, nil)
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
		provider := instantiateWasmtimeCoreRuntimeFixture(t, rt, "winch/table_fill.wast", 1, nil)
		table, err := provider.ExportedTable("t")
		if err != nil {
			t.Fatal(err)
		}
		consumer := instantiateWasmtimeCoreRuntimeFixture(t, rt, "winch/table_fill.wast", 2, wago.Imports{"t.t": table})

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
	provenance, err := wasmtimecorpus.LoadProvenance(filepath.Clean("../../testdata/wasmtime/PROVENANCE.json"))
	if err != nil {
		t.Fatal(err)
	}
	return provenance
}

func loadWasmtimeCoreManifest(t *testing.T) []wasmtimeCoreFixture {
	t.Helper()
	parsed, err := wasmtimecorpus.LoadManifest(filepath.Clean("../../testdata/wasmtime/MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := wasmtimecorpus.ValidateProvenanceFixtureSets(loadWasmtimeProvenance(t), parsed); err != nil {
		t.Fatal(err)
	}
	fixtures := make([]wasmtimeCoreFixture, len(parsed))
	for i, fixture := range parsed {
		fixtures[i] = wasmtimeCoreFixture{path: fixture.Path, coverage: fixture.Coverage, mode: fixture.Mode}
	}
	return fixtures
}

func wasmtimeCoreFixtureDir(path string) string {
	return filepath.Clean(filepath.Join("../../testdata/wasmtime/core", strings.TrimSuffix(path, ".wast")))
}

func compileWasmtimeCoreDirectFixture(t *testing.T, path string, module int) *wago.Compiled {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(wasmtimeCoreFixtureDir(path), "module."+strconv.Itoa(module)+".wasm"))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := wago.Compile(nil, data)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func instantiateWasmtimeCoreDirectFixture(t *testing.T, path string, module int, imports wago.Imports) *wago.Instance {
	t.Helper()
	compiled := compileWasmtimeCoreDirectFixture(t, path, module)
	t.Cleanup(func() { _ = compiled.Close() })
	in, err := wago.Instantiate(compiled, wago.InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = in.Close() })
	return in
}

func instantiateWasmtimeCoreRuntimeFixture(t *testing.T, rt *wago.Runtime, path string, module int, imports wago.Imports) *wago.Instance {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(wasmtimeCoreFixtureDir(path), "module."+strconv.Itoa(module)+".wasm"))
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
