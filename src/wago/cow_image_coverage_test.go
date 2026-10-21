//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

type imageSpan struct{ start, end uint64 }

// TestCOWImageCoverage surveys constant-offset active data in real modules.
// Touched page counts are an opportunity bound, not measured RSS/PSS.
func TestCOWImageCoverage(t *testing.T) {
	for _, rel := range []string{
		"corpus/workloads/applications/sqlite3/sqlite3.wasm",
		"corpus/workloads/applications/quickjs/qjs.wasm",
		"corpus/workloads/applications/jq/jq.wasm",
		"corpus/workloads/applications/php/php.wasm",
		"corpus/workloads/applications/lua/lua.wasm",
		"corpus/workloads/semantic/yyjson/yyjson.wasm",
	} {
		t.Run(filepath.Base(rel), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("../..", rel))
			if err != nil {
				t.Fatal(err)
			}
			c, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).Compile(data)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			eligibleMemory := c.memoryCount() == 1 && c.memoryDef(0).ImportKey == "" && !c.memoryDef(0).Shared && !c.memoryDef(0).Addr64
			var payload uint64
			var spans []imageSpan
			pages := make(map[uint64]bool)
			constantSegments := 0
			for i := 0; i < c.activeDataCount(); i++ {
				d := c.activeDataAt(i)
				payload += uint64(len(d.Bytes))
				if d.MemoryIndex != 0 || d.Offset.HasGlobal || len(d.Offset.Expr) != 0 {
					continue
				}
				constantSegments++
				if len(d.Bytes) == 0 {
					continue
				}
				start, end := uint64(d.Offset.Base), uint64(d.Offset.Base)+uint64(len(d.Bytes))
				spans = append(spans, imageSpan{start, end})
				for p := start / 4096; p <= (end-1)/4096; p++ {
					pages[p] = true
				}
			}
			sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
			overlaps := 0
			var priorEnd uint64
			for _, span := range spans {
				if span.start < priorEnd {
					overlaps++
				}
				if span.end > priorEnd {
					priorEnd = span.end
				}
			}
			initial, max := c.memorySizeBytes()
			t.Logf("owned_unshared_memory32=%v initial_bytes=%d max_bytes=%d function_imports=%d has_start=%v active_segments=%d constant_offset_segments=%d source_payload_bytes=%d touched_4k_pages=%d overlap_intervals=%d image_end=%d", eligibleMemory, initial, max, len(c.Imports), c.HasStart, c.activeDataCount(), constantSegments, payload, len(pages), overlaps, priorEnd)
		})
	}
}
