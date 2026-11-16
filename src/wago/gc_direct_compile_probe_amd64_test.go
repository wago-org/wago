//go:build linux && amd64 && !tinygo

package wago

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkGCDirectCompileProbe measures Compile+Close only. It is opt-in so
// routine benchmark discovery does not silently add this experimental corpus.
func BenchmarkGCDirectCompileProbe(b *testing.B) {
	if os.Getenv("GC_COMPILE_PROBE") != "1" {
		b.Skip("set GC_COMPILE_PROBE=1")
	}
	dir := filepath.Join("..", "..", "bench", "experiments", "gc-v8")
	raw, err := os.ReadFile(filepath.Join(dir, "cases.json"))
	if err != nil {
		b.Fatal(err)
	}
	var cases []struct {
		Name   string `json:"name"`
		File   string `json:"file"`
		SHA256 string `json:"sha256"`
	}
	if err = json.Unmarshal(raw, &cases); err != nil {
		b.Fatal(err)
	}
	selected := map[string]bool{"struct-allocation-access": true, "array-allocation-access": true, "array-fill": true, "array-copy-checksum": true, "array-loop-copy-checksum": true, "tree-survival": true}
	for _, tc := range cases {
		if !selected[tc.Name] {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, tc.File))
		if err != nil {
			b.Fatal(err)
		}
		if fmt.Sprintf("%x", sha256.Sum256(data)) != tc.SHA256 {
			b.Fatal("artifact digest mismatch")
		}
		b.Run(tc.Name, func(b *testing.B) {
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("dead-gc-new", false)
			size := 0
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				compiled, err := Compile(cfg, data)
				if err != nil {
					b.Fatal(err)
				}
				size = compiled.CodeSize()
				if err = compiled.Close(); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(size), "code-B")
		})
	}
}
