//go:build ((linux || darwin || windows) && (amd64 || arm64) && !tinygo) || (linux && riscv64 && !tinygo && !wago_target_tinygo)

package wago_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestExtendedConstFixtureManifest(t *testing.T) {
	root := filepath.Clean("../../tests/corpus/regressions/extended-const")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			got = append(got, entry.Name())
		}
	}
	sort.Strings(got)

	want := []string{
		"data.0.wasm", "data.1.wasm", "data.2.wasm", "data.3.wasm", "data.json", "data.wast",
		"elem.0.wasm", "elem.1.wasm", "elem.2.wasm", "elem.3.wasm", "elem.json", "elem.wast",
	}
	for i := 0; i <= 45; i++ {
		want = append(want, fmt.Sprintf("global.%d.wasm", i))
	}
	for i := 46; i <= 48; i++ {
		want = append(want, fmt.Sprintf("global.%d.wat", i))
	}
	want = append(want, "global.json", "global.wast")
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extended-const fixture manifest = %v, want %v", got, want)
	}
}

func TestExtendedConstSpecExecution(t *testing.T) {
	root := filepath.Clean("../../tests/corpus/regressions/extended-const")
	var total specExecStats
	for _, base := range []string{"data", "elem", "global"} {
		raw, err := os.ReadFile(filepath.Join(root, base+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var sf specExecFile
		if err := json.Unmarshal(raw, &sf); err != nil {
			t.Fatalf("decode %s.json: %v", base, err)
		}
		stats := runSpecExecFile(t, "extended-const/"+base, root, sf)
		total.add(stats)
		if stats.modulesFailed != 0 || stats.modulesSkipped != 0 || stats.assertionsFailed != 0 || stats.assertionsSkipped != 0 {
			t.Errorf("%s stats = %+v, want no failures or skips", base, stats)
		}
	}
	if total.modulesPassed != 13 || total.assertionsPassed != 60 {
		t.Fatalf("extended-const totals = modules %d assertions %d, want 13 and 60", total.modulesPassed, total.assertionsPassed)
	}
}
