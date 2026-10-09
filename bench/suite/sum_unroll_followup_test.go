//go:build linux && amd64

package wagobench

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	wago "github.com/wago-org/wago"
	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Confirm function-level admission before timings. Other modules are controls,
// not positive sum coverage. The ordinary test build skips diagnostic assertions.
func TestSumUnrollCorpusAdmission(t *testing.T) {
	dir := os.Getenv("WAGO_SUM_ARTIFACT_DIR")
	if dir == "" {
		t.Skip("set WAGO_SUM_ARTIFACT_DIR")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	wanted := map[string]bool{"memory": true, "linked_list": true, "many_funcs": true, "blake-as-simd": true, "utf-as-simd": true, "yyjson": true, "xxhash": true, "drwav": true}
	for _, entry := range loadCorpus(t) {
		if !wanted[entry.ID] {
			continue
		}
		m, err := wasm.DecodeModule(entry.bytes)
		if err != nil {
			t.Fatal(err)
		}
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		var stats railshot.ModuleStats
		cm, err := railshot.CompileModuleWith(m, railshot.CompileOptions{Workers: 1, Stats: &stats})
		if err != nil {
			t.Fatal(err)
		}
		if len(stats.Funcs) == 0 {
			cm.CodeImage.Close()
			t.Skip("requires wago_codegenstats")
		}
		base, candidate := 0, 0
		for _, s := range stats.Funcs {
			base += s.Peephole["linear-sum-unroll4"]
			candidate += s.Peephole["experimental-linear-sum"]
		}
		if entry.ID == "memory" {
			s := stats.Funcs[1]
			want := os.Getenv("WAGO_SUM_VARIANT") != "" && os.Getenv("WAGO_SUM_VARIANT") != "baseline" && os.Getenv("WAGO_SUM_VARIANT") != "default"
			if (s.Peephole["experimental-linear-sum"] == 1) != want || (!want && s.Peephole["linear-sum-unroll4"] != 1) {
				t.Fatalf("sum selection: %v", s.Peephole)
			}
			if os.Getenv("WAGO_SUM_VARIANT") == "H" && s.Peephole["experimental-linear-sum-hybrid"] != 1 {
				t.Fatalf("hybrid not selected: %v", s.Peephole)
			}
			if strings.HasPrefix(os.Getenv("WAGO_SUM_VARIANT"), "T") && s.Peephole["experimental-linear-sum-threshold"] != 1 {
				t.Fatalf("threshold not selected: %v", s.Peephole)
			}
			if err := os.WriteFile(filepath.Join(dir, "memory.bin"), cm.Code, 0644); err != nil {
				t.Fatal(err)
			}
		} else if base+candidate != 0 {
			t.Fatalf("new eligible control %s: %d/%d", entry.ID, base, candidate)
		}
		record := struct {
			ID, SHA256                              string
			TotalBytes, BaselineHits, CandidateHits int
			Exports                                 []wasm.Export
			Stats                                   railshot.ModuleStats
		}{entry.ID, fmt.Sprintf("%x", sha256.Sum256(entry.bytes)), len(cm.Code), base, candidate, m.Exports, stats}
		text, err := json.MarshalIndent(record, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, entry.ID+".json"), append(text, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
		cm.CodeImage.Close()
	}
}

// A complete one-call lifecycle for the unchanged corpus module. This is still
// the corpus synthetic sum, not an application-level performance claim.
func BenchmarkSumUnrollLifecycle(b *testing.B) {
	for _, m := range loadCorpus(b) {
		if m.ID != "memory" {
			continue
		}
		b.Run(m.ID, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c, err := wago.Compile(m.bytes)
				if err != nil {
					b.Fatal(err)
				}
				in, err := wago.Instantiate(c, wago.InstantiateOptions{})
				if err != nil {
					c.Close()
					b.Fatal(err)
				}
				got, err := in.Invoke("sum", wago.I32(512))
				in.Close()
				c.Close()
				if err != nil || len(got) != 1 || got[0] != 0 {
					b.Fatalf("sum: %v %v", got, err)
				}
			}
		})
	}
}
