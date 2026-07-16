//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	corergc "github.com/wago-org/wago/src/core/runtime/gc"
)

func stagedGCArrayInitLeaderBytes(t testing.TB, filename string) []byte {
	t.Helper()
	base := "gc/array_init_data"
	var script stagedSpecScript
	tmp := stagedOfficialTypedReferenceJSON(t, base, &script)
	for _, cmd := range script.Commands {
		if cmd.Type != "module_definition" || cmd.Filename != filename {
			continue
		}
		data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	t.Fatalf("%s has no module definition", filename)
	return nil
}

func TestStagedGCArrayInitDataProductBoundary(t *testing.T) {
	for _, tc := range []struct {
		filename string
		roots    uint8
	}{
		{filename: "array_init_data.2.wasm", roots: 3},
		{filename: "array_init_data.3.wasm", roots: 0},
	} {
		t.Run(tc.filename, func(t *testing.T) {
			data := stagedGCArrayInitLeaderBytes(t, tc.filename)
			guardCfg := NewRuntimeConfig()
			guardCfg.boundsChecks = BoundsChecksSignalsBased
			features := guardCfg.frontendFeatures()
			features.TypedFunctionReferences = true
			features.GCArrayProducts = true
			if _, err := compileWithFrontendFeatures(guardCfg, data, features); err == nil || !strings.Contains(err.Error(), "signals-based") {
				t.Fatalf("guard compile=%v, want explicit array-init rejection", err)
			}

			c, err := compileStagedGCArrayInit(data)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if got := c.stagedGCArrayProduct(); got != stagedGCArrayProductInitData || !c.usesGCArrayHelpers() {
				t.Fatalf("product/helper=%s/%v, want init-data/true", got, c.usesGCArrayHelpers())
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
				t.Fatal("codec reload inherited array-init product/helper admission")
			}
			if in, err := instantiateCore(&loaded, InstantiateOptions{}); err == nil {
				_ = in.Close()
				t.Fatal("codec-loaded array-init artifact instantiated")
			}

			in, err := instantiateCore(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 16, TinyStepEveryAlloc: true, VerifyAfterCollect: true}})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			state := in.pluginState.Load()
			var roots uint8
			if state != nil {
				roots = state.gcGlobalRootCount
			}
			if roots != tc.roots {
				t.Fatalf("global roots=%d, want %d", roots, tc.roots)
			}
			if tc.filename == "array_init_data.2.wasm" {
				if _, err := in.Invoke("array_init_data", 4, 2, 2); err != nil {
					t.Fatal(err)
				}
				got, err := in.Invoke("array_get_nth", 4)
				if err != nil || len(got) != 1 || got[0] != 99 {
					t.Fatalf("i8 init value=%v,%v want 99", got, err)
				}
				if _, err := in.Invoke("array_init_data_i16", 2, 5, 2); err != nil {
					t.Fatal(err)
				}
				got, err = in.Invoke("array_get_nth_i16", 2)
				if err != nil || len(got) != 1 || got[0] != 0x6766 {
					t.Fatalf("i16 init value=%v,%v want %#x", got, err, 0x6766)
				}
			} else {
				if _, err := in.Invoke("f4"); err != nil {
					t.Fatalf("f4: %v", err)
				}
				if _, err := in.Invoke("g8"); err != nil {
					t.Fatalf("g8: %v", err)
				}
				if _, err := in.Invoke("f3"); err == nil {
					t.Fatal("f3 returned normally with a three-byte i32 source")
				}
				if _, err := in.Invoke("g7"); err == nil {
					t.Fatal("g7 returned normally with a seven-byte i64 source")
				}
			}
			t.Logf("%s product: wasm=%d code=%d codec=%d", tc.filename, len(data), len(c.Code), len(blob))
		})
	}
	t.Logf("array-init layouts: Compiled=%d Instance=%d codeCache=%d memoryDir=%d arrayGlobal=%d plugin=%d collector=%d", unsafe.Sizeof(Compiled{}), unsafe.Sizeof(Instance{}), unsafe.Sizeof(compiledCodeCache{}), unsafe.Sizeof(compiledMemoryDirectory{}), unsafe.Sizeof(gcArrayGlobalInit{}), unsafe.Sizeof(instancePluginState{}), unsafe.Sizeof(corergc.Collector{}))
}

func TestStagedGCArrayInitDataTinyLifecycle(t *testing.T) {
	data := stagedGCArrayInitLeaderBytes(t, "array_init_data.2.wasm")
	c, err := compileStagedGCArrayInit(data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 96, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	state := in.pluginState.Load()
	if state == nil || state.gcGlobalRootCount != 3 {
		t.Fatalf("root state=%#v, want three global roots", state)
	}
	for i := 0; i < 100; i++ {
		if _, err := in.Invoke("array_init_data", 4, 2, 2); err != nil {
			t.Fatalf("iteration %d i8 init: %v", i, err)
		}
		if _, err := in.Invoke("array_init_data_i16", 2, 5, 2); err != nil {
			t.Fatalf("iteration %d i16 init: %v", i, err)
		}
	}
	before, err := in.Invoke("array_get_nth_i16", 2)
	if err != nil || len(before) != 1 || before[0] != 0x6766 {
		t.Fatalf("i16 before trap=%v,%v", before, err)
	}
	if _, err := in.Invoke("array_init_data_i16", 2, 11, 1); err == nil {
		t.Fatal("short i16 source returned normally")
	}
	after, err := in.Invoke("array_get_nth_i16", 2)
	if err != nil || len(after) != 1 || after[0] != before[0] {
		t.Fatalf("trapping init changed destination: before=%v after=%v err=%v", before, after, err)
	}
	if err := in.gc.CollectFull(nil); err != nil {
		t.Fatal(err)
	}
	if live := in.gc.Stats().LiveObjects; live != 3 {
		t.Fatalf("live objects=%d, want three rooted arrays", live)
	}
	if _, err := in.Invoke("drop_segs"); err != nil {
		t.Fatal(err)
	}
	if _, err := in.Invoke("array_init_data", 0, 0, 0); err != nil {
		t.Fatalf("zero length after drop: %v", err)
	}
	if _, err := in.Invoke("array_init_data", 0, 0, 1); err == nil {
		t.Fatal("non-zero init after drop returned normally")
	}
}

func TestStagedGCArrayInitDataTransientTinyRootProof(t *testing.T) {
	data := stagedGCArrayInitLeaderBytes(t, "array_init_data.3.wasm")
	c, err := compileStagedGCArrayInit(data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 24, TinyBlockBytes: 8, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for i := 0; i < 100; i++ {
		field := "f4"
		if i&1 != 0 {
			field = "g8"
		}
		if _, err := in.Invoke(field); err != nil {
			t.Fatalf("iteration %d %s: %v", i, field, err)
		}
	}
	if _, err := in.Invoke("f3"); err == nil {
		t.Fatal("short i32 source returned normally")
	}
	if _, err := in.Invoke("g8"); err != nil {
		t.Fatalf("recovery after trap: %v", err)
	}
}

func BenchmarkStagedGCArrayInitData(b *testing.B) {
	data := stagedGCArrayInitLeaderBytes(b, "array_init_data.2.wasm")
	c, err := compileStagedGCArrayInit(data)
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("array_init_data", 4, 2, 2); err != nil {
			b.Fatal(err)
		}
	}
}
