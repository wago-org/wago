//go:build (linux || darwin) && (amd64 || arm64) && !tinygo && !wago_guardpage

package wago_test

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/wago"
)

type wasmtimeWasm2Fixture struct {
	path     string
	features string
	mode     string
}

func TestWasmtimePortCoreManifest(t *testing.T) {
	fixtures := loadWasmtimeWasm2Manifest(t)
	if len(fixtures) != 104 {
		t.Fatalf("Wasmtime core manifest has %d entries, want 104", len(fixtures))
	}

	modes := map[string]int{}
	features := map[string]int{}
	for _, fixture := range fixtures {
		modes[fixture.mode]++
		for _, feature := range strings.Split(fixture.features, ",") {
			features[feature]++
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
		case "direct-go", "direct-invalid", "direct-concurrency":
			if _, err := os.Stat(filepath.Join(dir, "module.0.wasm")); err != nil {
				t.Errorf("%s direct module fixture: %v", fixture.path, err)
			}
		default:
			t.Errorf("%s has unknown port mode %q", fixture.path, fixture.mode)
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
		if features[feature] == 0 {
			t.Errorf("Wasmtime core manifest has no %s tests", feature)
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
	const want = "b3bbb1072672801a3ce89a12deb953b10a2fe4a2b690f67b9de54489f7c6b243"
	if got != want {
		t.Fatalf("Wasmtime core fixture tree digest = %s, want %s", got, want)
	}
}

func TestWasmtimePortCoreWastExecution(t *testing.T) {
	var total specExecStats
	for _, fixture := range loadWasmtimeWasm2Manifest(t) {
		if fixture.mode != "wast-json" {
			continue
		}
		fixture := fixture
		t.Run(strings.TrimSuffix(fixture.path, ".wast"), func(t *testing.T) {
			dir := wasmtimeWasm2FixtureDir(fixture.path)
			raw, err := os.ReadFile(filepath.Join(dir, "commands.json"))
			if err != nil {
				t.Fatal(err)
			}
			var sf specExecFile
			if err := json.Unmarshal(raw, &sf); err != nil {
				t.Fatalf("decode commands: %v", err)
			}
			stats := runSpecExecFile(t, fixture.path, dir, sf)
			if stats.modulesFailed != 0 || stats.modulesSkipped != 0 || stats.assertionsFailed != 0 || stats.assertionsSkipped != 0 {
				t.Fatalf("Wasmtime fixture stats = %+v, want no failures or skips", stats)
			}
			total.add(stats)
		})
	}
	if total.modulesPassed != 141 || total.assertionsPassed != 535 {
		t.Fatalf("Wasmtime WAST totals = modules %d assertions %d, want 141 and 535", total.modulesPassed, total.assertionsPassed)
	}
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
		if err := <-done; err != nil {
			_ = first.Close()
			t.Fatalf("iteration %d threaded instantiate/drop: %v", i, err)
		}
		if err := first.Close(); err != nil {
			t.Fatalf("iteration %d close first module after threaded drop: %v", i, err)
		}
	}
}

func TestWasmtimePortMemoryReuseKeepsBounds(t *testing.T) {
	growCode := compileWasmtimeWasm2DirectFixture(t, "pooling-oob-on-reuse.wast", 0)
	defer growCode.Close()
	oobCode := compileWasmtimeWasm2DirectFixture(t, "pooling-oob-on-reuse.wast", 1)
	defer oobCode.Close()

	for i := 0; i < 32; i++ {
		done := make(chan error, 1)
		go func() {
			in, err := wago.Instantiate(growCode, wago.InstantiateOptions{})
			if err == nil {
				_, err = in.Invoke("grow")
			}
			if in != nil {
				if closeErr := in.Close(); err == nil {
					err = closeErr
				}
			}
			done <- err
		}()
		if err := <-done; err != nil {
			t.Fatalf("iteration %d grow/drop module: %v", i, err)
		}

		in, err := wago.Instantiate(oobCode, wago.InstantiateOptions{})
		if err != nil {
			t.Fatalf("iteration %d instantiate bounds module: %v", i, err)
		}
		if got, err := in.Invoke("read_oob"); err == nil {
			_ = in.Close()
			t.Fatalf("iteration %d stale grown bounds returned %v, want trap", i, got)
		}
		if err := in.Close(); err != nil {
			t.Fatalf("iteration %d close bounds module: %v", i, err)
		}
	}
}

func TestWasmtimePortReferenceTypesBasic(t *testing.T) {
	in := instantiateWasmtimeWasm2DirectFixture(t, "winch/ref-types-basic.wast", 0, nil)

	assertWasmtimeResult(t, in, "null-is-null", []uint64{1})
	assertWasmtimeResult(t, in, "func-is-not-null", []uint64{0})
	assertWasmtimeResult(t, in, "call-indirect-0", []uint64{42})
	assertWasmtimeResult(t, in, "call-indirect-1", []uint64{7})
	assertWasmtimeTrap(t, in, "call-indirect-null")

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
	assertWasmtimeTrap(t, in, "table-get", wago.I32(10))

	assertWasmtimeResult(t, in, "table-set-null", nil, wago.I32(0))
	assertWasmtimeResult(t, in, "table-get", []uint64{0}, wago.I32(0))
	assertWasmtimeResult(t, in, "table-set-ref", nil, wago.I32(0))
	assertWasmtimeResult(t, in, "table-get", []uint64{f0}, wago.I32(0))
	assertWasmtimeTrap(t, in, "table-set-null", wago.I32(10))

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
	assertWasmtimeTrap(t, in, "fill", wago.I32(8), wago.I32(0), wago.I32(3))

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
		assertWasmtimeTrap(t, consumer, "fill1", wago.I32(2), 0, wago.I32(0))

		for _, args := range [][3]int32{{0, 0, 0}, {0, 0, 1}, {0, 0, 2}, {1, 0, 0}, {1, 0, 1}, {2, 0, 0}} {
			assertWasmtimeResult(t, consumer, "fill2", nil, wago.I32(args[0]), 0, wago.I32(args[2]))
		}
		assertWasmtimeTrap(t, consumer, "fill2", wago.I32(3), 0, wago.I32(0))
	})
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
		fixtures = append(fixtures, wasmtimeWasm2Fixture{path: fields[0], features: fields[1], mode: fields[2]})
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

func assertWasmtimeTrap(t *testing.T, in *wago.Instance, export string, args ...uint64) {
	t.Helper()
	if got, err := in.Invoke(export, args...); err == nil {
		t.Fatalf("%s%v returned %v, want trap", export, args, got)
	}
}
