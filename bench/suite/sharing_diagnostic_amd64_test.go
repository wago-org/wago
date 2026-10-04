//go:build amd64 && wago_codegenstats

package wagobench

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestSharingDiagnostic(t *testing.T) {
	for _, f := range sharingFixtures() {
		sharingReport(t, f.name, f.bytes)
	}
	for _, m := range loadCorpus(t) {
		if m.supports("Compile") {
			sharingReport(t, m.ID, m.bytes)
		}
	}
}
func sharingReport(t *testing.T, name string, data []byte) {
	m, e := wasm.DecodeModule(data)
	if e != nil {
		t.Fatal(e)
	}
	if e = wasm.ValidateModule(m); e != nil {
		t.Fatal(e)
	}
	var stats railshot.ModuleStats
	c, e := railshot.CompileModuleWith(m, railshot.CompileOptions{Workers: 1, Stats: &stats})
	if e != nil {
		t.Fatal(e)
	}
	body := 0
	for _, f := range m.Code {
		body += len(f.BodyBytes)
	}
	report, _ := json.Marshal(map[string]any{"name": name, "corpus_sha256": fmtHash(data), "native_sha256": fmtHash(c.Code), "code_bytes": len(c.Code), "functions": len(m.Code), "body_bytes": body, "stats": stats})
	t.Log(string(report))
	if c.CodeImage != nil {
		if e = c.CodeImage.Close(); e != nil {
			t.Fatal(e)
		}
	}
}
func fmtHash(b []byte) string { v := sha256.Sum256(b); return fmt.Sprintf("%x", v) }
