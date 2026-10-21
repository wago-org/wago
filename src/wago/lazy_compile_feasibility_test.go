//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/frontend"
)

// TestLazyCompileFeasibility is a phase-0 accounting control, not a lazy JIT.
// Validation is kept eager. Executed-function coverage needs instrumentation
// of the actual native entries before any saved-code estimate is credible.
func TestLazyCompileFeasibility(t *testing.T) {
	for _, rel := range []string{
		"corpus/workloads/applications/sqlite3/sqlite3.wasm",
		"corpus/workloads/applications/quickjs/qjs.wasm",
		"corpus/workloads/applications/jq/jq.wasm",
		"corpus/workloads/applications/php/php.wasm",
		"corpus/workloads/applications/lua/lua.wasm",
	} {
		t.Run(filepath.Base(rel), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("../..", rel))
			if err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			m, err := frontend.DecodeValidate(append([]byte(nil), data...))
			if err != nil {
				t.Fatal(err)
			}
			validationTime := time.Since(started)
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			started = time.Now()
			c, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).Compile(data)
			compileTime := time.Since(started)
			runtime.ReadMemStats(&after)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			artifact, err := c.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			var elemRefs int
			for _, seg := range c.Elems {
				for _, value := range seg.Values {
					if !value.Null && !value.HasGlobal && value.Expr == nil {
						elemRefs++
					}
				}
			}
			t.Logf("source_bytes=%d local_functions=%d total_functions=%d exports=%d element_ref_entries=%d native_code_bytes=%d serialized_bytes=%d validate_ms=%.2f compile_ms=%.2f compile_alloc_bytes=%d compile_mallocs=%d", len(data), len(m.Code), len(c.FuncTypeID), len(c.Exports), elemRefs, c.CodeSize(), len(artifact), float64(validationTime.Microseconds())/1000, float64(compileTime.Microseconds())/1000, after.TotalAlloc-before.TotalAlloc, after.Mallocs-before.Mallocs)
		})
	}
}
