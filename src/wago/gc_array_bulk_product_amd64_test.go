//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stagedGCArrayBulkLeaderBytes(t testing.TB, base string) []byte {
	t.Helper()
	var script stagedSpecScript
	tmp := stagedOfficialTypedReferenceJSON(t, base, &script)
	for _, cmd := range script.Commands {
		if cmd.Type != "module_definition" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	t.Fatalf("%s has no module definition", base)
	return nil
}

func TestStagedGCArrayBulkProductBoundary(t *testing.T) {
	for _, tc := range []struct {
		base        string
		product     stagedGCArrayProduct
		tinyBytes   uint32
		invoke      string
		args        []uint64
		getIndex    uint64
		wantElement uint64
	}{
		{base: "gc/array_fill", product: stagedGCArrayProductBulkFill, tinyBytes: 64, invoke: "array_fill", args: []uint64{2, 0x10b, 2}, getIndex: 2, wantElement: 11},
		{base: "gc/array_copy", product: stagedGCArrayProductBulkCopy, tinyBytes: 64, invoke: "array_copy", args: []uint64{0, 0, 2}, getIndex: 0, wantElement: 10},
	} {
		t.Run(tc.base, func(t *testing.T) {
			data := stagedGCArrayBulkLeaderBytes(t, tc.base)
			guardCfg := NewRuntimeConfig()
			guardCfg.boundsChecks = BoundsChecksSignalsBased
			features := guardCfg.frontendFeatures()
			features.TypedFunctionReferences = true
			features.GCArrayProducts = true
			if _, err := compileWithFrontendFeatures(guardCfg, data, features); err == nil || !strings.Contains(err.Error(), "signals-based") {
				t.Fatalf("guard compile=%v, want explicit bulk-array rejection", err)
			}

			c, err := compileStagedGCArrayBulk(data)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if got := c.stagedGCArrayProduct(); got != tc.product {
				t.Fatalf("product=%s, want %s", got, tc.product)
			}
			if !c.usesGCArrayHelpers() {
				t.Fatal("bulk array product omitted helper admission")
			}
			if _, err := Capture(c, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "WasmGC") {
				t.Fatalf("snapshot capture=%v, want WasmGC rejection", err)
			}
			blob, err := marshalCompiled(c)
			if err != nil {
				t.Fatal(err)
			}
			var loaded Compiled
			if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
				t.Fatal(err)
			}
			defer loaded.Close()
			if loaded.stagedGCArrayProduct() != 0 || loaded.usesGCArrayHelpers() {
				t.Fatal("codec reload inherited bulk array product/helper admission")
			}
			if in, err := instantiateCore(&loaded, InstantiateOptions{}); err == nil {
				_ = in.Close()
				t.Fatal("codec-loaded bulk array artifact instantiated")
			}

			in, err := instantiateCore(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: tc.tinyBytes, TinyBlockBytes: 16, TinyStepEveryAlloc: true, VerifyAfterCollect: true}})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if state := in.pluginState.Load(); state == nil || state.gcGlobalRootCount != 2 {
				t.Fatalf("bulk array global roots=%#v, want two", state)
			}
			if _, err := in.Invoke(tc.invoke, tc.args...); err != nil {
				t.Fatalf("%s=%v", tc.invoke, err)
			}
			got, err := in.Invoke("array_get_nth", tc.getIndex)
			if err != nil || len(got) != 1 || got[0] != tc.wantElement {
				t.Fatalf("array_get_nth=%v,%v want %d", got, err, tc.wantElement)
			}
		})
	}
}
