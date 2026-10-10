//go:build amd64

package amd64

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/frontend"
)

func TestControlScratchRealCodeDigest(t *testing.T) {
	for _, rel := range []string{
		"corpus/workloads/applications/sqlite3/sqlite3.wasm",
		"corpus/workloads/applications/quickjs/qjs.wasm",
		"corpus/workloads/applications/jq/jq.wasm",
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
			cm, err := CompileModuleWith(m, CompileOptions{Workers: 1})
			if err != nil {
				t.Fatal(err)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			t.Logf("native_bytes=%d sha256=%x entry_count=%d", len(cm.Code), sha256.Sum256(cm.Code), len(cm.Entry))
		})
	}
}

func BenchmarkControlScratchRealCompile(b *testing.B) {
	for _, rel := range []string{
		"corpus/workloads/applications/sqlite3/sqlite3.wasm",
		"corpus/workloads/applications/quickjs/qjs.wasm",
		"corpus/workloads/applications/jq/jq.wasm",
	} {
		b.Run(filepath.Base(rel), func(b *testing.B) {
			data, err := os.ReadFile(filepath.Join("../../../../../..", rel))
			if err != nil {
				b.Fatal(err)
			}
			m, err := frontend.DecodeValidate(data)
			if err != nil {
				b.Fatal(err)
			}
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			b.ResetTimer()
			codeBytes := 0
			for range b.N {
				cm, err := CompileModuleWith(m, CompileOptions{Workers: 1})
				if err != nil {
					b.Fatal(err)
				}
				codeBytes = len(cm.Code)
				if cm.CodeImage != nil {
					_ = cm.CodeImage.Close()
				}
			}
			b.ReportMetric(float64(codeBytes), "native-B")
		})
	}
}
