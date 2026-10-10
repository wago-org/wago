//go:build amd64

package amd64

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestControlScratchCorpusDepth(t *testing.T) {
	for _, rel := range []string{
		"corpus/workloads/applications/sqlite3/sqlite3.wasm",
		"corpus/workloads/applications/quickjs/qjs.wasm",
		"corpus/workloads/applications/jq/jq.wasm",
		"corpus/workloads/applications/php/php.wasm",
		"corpus/workloads/applications/lua/lua.wasm",
	} {
		t.Run(filepath.Base(rel), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("../../../../../..", rel))
			if err != nil {
				t.Fatal(err)
			}
			m, err := frontend.DecodeValidate(data)
			if err != nil {
				t.Fatal(err)
			}
			hints, _, _, err := computeModuleHints(m, m.GlobalCount(), m.ImportedFuncCount(), nil, false)
			if err != nil {
				t.Fatal(err)
			}
			maxDepth, beyondHint := 0, 0
			for _, h := range hints {
				if d := int(h.maxControlDepth); d > maxDepth {
					maxDepth = d
				}
				if h.maxControlDepth == 255 {
					beyondHint++
				}
			}
			t.Logf("functions=%d max_hinted_depth=%d saturated_depth_functions=%d", len(m.Code), maxDepth, beyondHint)
		})
	}
}

func benchmarkControlScratchModule(b *testing.B, deepEverywhere bool) {
	m := benchParallelControlOutlierModule(b, 64, 768)
	if deepEverywhere {
		deep := m.Code[0]
		for i := range m.Code {
			m.Code[i] = deep
		}
	}
	if err := wasm.ValidateModule(m); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		cm, err := CompileModuleWith(m, CompileOptions{Workers: 1})
		if err != nil {
			b.Fatal(err)
		}
		if cm.CodeImage != nil {
			_ = cm.CodeImage.Close()
		}
	}
}

func BenchmarkControlScratchDeepThenTiny(b *testing.B) { benchmarkControlScratchModule(b, false) }
func BenchmarkControlScratchDeepThenDeep(b *testing.B) { benchmarkControlScratchModule(b, true) }
