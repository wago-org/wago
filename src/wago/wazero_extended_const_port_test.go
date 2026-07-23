//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/wago-org/wago/src/wago"
)

func TestWazeroPortExtendedConstFixtureManifest(t *testing.T) {
	root := filepath.Clean("../../testdata/wazero/extended-const")
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

func TestWazeroPortExtendedConstCodecExecution(t *testing.T) {
	root := filepath.Clean("../../testdata/wazero/extended-const")
	imports := wago.Imports{
		"spectest.global_i32": wago.GlobalImport{Type: wago.ValI32, Bits: wago.I32(666)},
		"spectest.global_i64": wago.GlobalImport{Type: wago.ValI64, Bits: wago.I64(666)},
	}
	for _, tc := range []struct {
		name   string
		file   string
		export string
		args   []uint64
		want   uint64
	}{
		{name: "data offset", file: "data.3.wasm"},
		{name: "element offset", file: "elem.3.wasm", export: "call_in_table", args: []uint64{wago.I32(6)}, want: 42},
		{name: "global i32 arithmetic", file: "global.0.wasm", export: "get-z5", want: 708},
		{name: "global i64 arithmetic", file: "global.0.wasm", export: "get-z6", want: 708},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, tc.file))
			if err != nil {
				t.Fatal(err)
			}
			compiled, err := wago.Compile(nil, data)
			if err != nil {
				t.Fatal(err)
			}
			blob, err := compiled.MarshalBinary()
			_ = compiled.Close()
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := wago.Load(blob)
			if err != nil {
				t.Fatal(err)
			}
			defer loaded.Close()
			in, err := wago.Instantiate(loaded, wago.InstantiateOptions{Imports: imports})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if tc.export == "" {
				return
			}
			got, err := in.Invoke(tc.export, tc.args...)
			if err != nil || len(got) != 1 || got[0] != tc.want {
				t.Fatalf("%s%v = %v, %v; want [%d]", tc.export, tc.args, got, err, tc.want)
			}
		})
	}
}

func TestWazeroPortExtendedConstSpecExecution(t *testing.T) {
	root := filepath.Clean("../../testdata/wazero/extended-const")
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
