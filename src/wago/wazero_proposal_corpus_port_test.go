//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package wago

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type wazeroProposalFile struct {
	Commands []struct {
		Type       string `json:"type"`
		Line       int    `json:"line"`
		Filename   string `json:"filename"`
		ModuleType string `json:"module_type"`
		Text       string `json:"text"`
	} `json:"commands"`
}

func TestWazeroPortUnsupportedProposalCorporaFailClosed(t *testing.T) {
	root := filepath.Clean("../../testdata/wazero/spectest-proposals")
	type proposalWant struct {
		files, accepted, unsupported, invalid, malformedBinary, malformedText int
	}
	wants := map[string]proposalWant{
		"exception-handling":        {files: 40, unsupported: 14, invalid: 16, malformedText: 2},
		"tail-call":                 {files: 45, unsupported: 6, invalid: 24, malformedText: 11},
		"threads":                   {files: 55, unsupported: 5, invalid: 48},
		"typed-function-references": {files: 642, accepted: 139, unsupported: 86, invalid: 333, malformedText: 40},
	}
	for proposal, want := range wants {
		proposal := proposal
		t.Run(proposal, func(t *testing.T) {
			dir := filepath.Join(root, proposal)
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != want.files {
				t.Fatalf("fixture count = %d, want %d", len(entries), want.files)
			}
			jsonPaths, err := filepath.Glob(filepath.Join(dir, "*.json"))
			if err != nil {
				t.Fatal(err)
			}
			seen := map[string]bool{}
			var validAccepted, validRejected, invalidRejected, malformedBinary, malformedText int
			for _, jsonPath := range jsonPaths {
				raw, err := os.ReadFile(jsonPath)
				if err != nil {
					t.Fatal(err)
				}
				var sf wazeroProposalFile
				if err := json.Unmarshal(raw, &sf); err != nil {
					t.Fatalf("decode %s: %v", filepath.Base(jsonPath), err)
				}
				for _, cmd := range sf.Commands {
					if cmd.Filename == "" {
						continue
					}
					path := filepath.Join(dir, cmd.Filename)
					if cmd.ModuleType == "text" || filepath.Ext(path) == ".wat" {
						malformedText++
						if _, err := os.Stat(path); err != nil {
							t.Errorf("%s:%d text fixture missing: %v", filepath.Base(jsonPath), cmd.Line, err)
						}
						continue
					}
					seen[cmd.Filename] = true
					data, err := os.ReadFile(path)
					if err != nil {
						t.Errorf("%s:%d read %s: %v", filepath.Base(jsonPath), cmd.Line, cmd.Filename, err)
						continue
					}
					compiled, compileErr := Compile(nil, data)
					if compiled != nil {
						_ = compiled.Close()
					}
					switch cmd.Type {
					case "assert_invalid":
						if compileErr == nil {
							t.Errorf("%s:%d invalid module accepted: %s", filepath.Base(jsonPath), cmd.Line, cmd.Text)
						} else {
							invalidRejected++
						}
					case "assert_malformed":
						malformedBinary++
						if compileErr == nil {
							t.Errorf("%s:%d malformed binary accepted: %s", filepath.Base(jsonPath), cmd.Line, cmd.Text)
						}
					default:
						if compileErr == nil {
							validAccepted++
						} else if strings.Contains(strings.ToLower(compileErr.Error()), "unsupported") || strings.Contains(strings.ToLower(compileErr.Error()), "shared memory") || strings.Contains(strings.ToLower(compileErr.Error()), "atomic") {
							validRejected++
						} else {
							t.Errorf("%s:%d valid proposal module failed without explicit unsupported rejection: %v", filepath.Base(jsonPath), cmd.Line, compileErr)
						}
					}
				}
			}
			wasmPaths, err := filepath.Glob(filepath.Join(dir, "*.wasm"))
			if err != nil {
				t.Fatal(err)
			}
			if len(seen) != len(wasmPaths) {
				t.Fatalf("JSON accounts for %d unique binaries, directory has %d", len(seen), len(wasmPaths))
			}
			if validAccepted != want.accepted || validRejected != want.unsupported || invalidRejected != want.invalid || malformedBinary != want.malformedBinary || malformedText != want.malformedText {
				t.Fatalf("corpus accounting = accepted %d unsupported %d invalid %d malformed binary/text %d/%d, want %d/%d/%d/%d/%d", validAccepted, validRejected, invalidRejected, malformedBinary, malformedText, want.accepted, want.unsupported, want.invalid, want.malformedBinary, want.malformedText)
			}
		})
	}
}
