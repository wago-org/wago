package wasmtimecorpus

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateWASTJSONFixtureRejectsSchemaAndArtifactDrift(t *testing.T) {
	valid := `{"source_filename":"testdata/wasmtime/wasm2/add/source.wast","commands":[
{"type":"module","line":1,"filename":"commands.0.wasm"},
{"type":"assert_return","line":2,"action":{"type":"invoke","field":"run","args":[]},"expected":[]}
]}`
	for _, tc := range []struct {
		name string
		json string
		wasm []string
		ok   bool
	}{
		{name: "valid", json: valid, wasm: []string{"commands.0.wasm"}, ok: true},
		{name: "unknown field", json: strings.Replace(valid, `"line":1`, `"line":1,"typo":true`, 1), wasm: []string{"commands.0.wasm"}},
		{name: "unsafe filename", json: strings.Replace(valid, `commands.0.wasm`, `../module.wasm`, 1), wasm: []string{"commands.0.wasm"}},
		{name: "missing artifact", json: valid},
		{name: "orphan artifact", json: valid, wasm: []string{"commands.0.wasm", "commands.1.wasm"}},
		{name: "duplicate module", json: strings.Replace(valid, `{"type":"assert_return"`, `{"type":"module","line":2,"filename":"commands.0.wasm"},{"type":"assert_return"`, 1), wasm: []string{"commands.0.wasm"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "commands.json"), []byte(tc.json), 0o644); err != nil {
				t.Fatal(err)
			}
			for _, name := range tc.wasm {
				if err := os.WriteFile(filepath.Join(dir, name), []byte("wasm"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			counts, err := ValidateWASTJSONFixture(dir)
			if tc.ok {
				if err != nil || counts.Modules != 1 || counts.Assertions != 1 {
					t.Fatalf("valid fixture = %+v, %v", counts, err)
				}
			} else if err == nil {
				t.Fatal("invalid command graph was accepted")
			}
		})
	}
}
