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
		files, accepted, unsupported, invalid, negativeInstantiation, malformedBinary, malformedText int
	}
	wants := map[string]proposalWant{
		"exception-handling":        {files: 40, unsupported: 14, invalid: 16, malformedText: 2},
		"tail-call":                 {files: 45, unsupported: 6, invalid: 24, malformedText: 11},
		"threads":                   {files: 55, unsupported: 5, invalid: 48},
		"typed-function-references": {files: 642, accepted: 97, unsupported: 86, invalid: 333, negativeInstantiation: 42, malformedText: 40},
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
			var validAccepted, validRejected, invalidRejected, negativeInstantiation, malformedBinary, malformedText int
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
						if cmd.Type != "assert_malformed" {
							t.Errorf("%s:%d unexpected text fixture command %q", filepath.Base(jsonPath), cmd.Line, cmd.Type)
						}
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
					if compileErr != nil && compiled != nil {
						t.Errorf("%s:%d compile returned both artifact and error: %v", filepath.Base(jsonPath), cmd.Line, compileErr)
						_ = compiled.Close()
						compiled = nil
					}
					unsupported := compileErr != nil && isExplicitProposalRejection(compileErr)
					switch cmd.Type {
					case "assert_invalid":
						if compiled != nil {
							_ = compiled.Close()
						}
						if compileErr == nil {
							t.Errorf("%s:%d invalid module accepted: %s", filepath.Base(jsonPath), cmd.Line, cmd.Text)
						} else {
							invalidRejected++
						}
					case "assert_malformed":
						malformedBinary++
						if compiled != nil {
							_ = compiled.Close()
						}
						if compileErr == nil {
							t.Errorf("%s:%d malformed binary accepted: %s", filepath.Base(jsonPath), cmd.Line, cmd.Text)
						}
					case "assert_uninstantiable", "assert_unlinkable":
						if compiled != nil {
							_ = compiled.Close()
						}
						if unsupported {
							validRejected++
						} else if compileErr != nil {
							t.Errorf("%s:%d valid negative module failed without explicit unsupported rejection: %v", filepath.Base(jsonPath), cmd.Line, compileErr)
						} else {
							// Count these separately from ordinary accepted modules. Correctly
							// replaying them requires the WAST file's registered providers;
							// empty imports would turn unrelated missing-import errors into false
							// positives. Focused tests below exercise the self-contained cases.
							negativeInstantiation++
						}
					case "module":
						if compiled != nil {
							_ = compiled.Close()
						}
						if compileErr == nil {
							validAccepted++
						} else if unsupported {
							validRejected++
						} else {
							t.Errorf("%s:%d valid proposal module failed without explicit unsupported rejection: %v", filepath.Base(jsonPath), cmd.Line, compileErr)
						}
					default:
						if compiled != nil {
							_ = compiled.Close()
						}
						t.Errorf("%s:%d unhandled fixture command %q with binary %s", filepath.Base(jsonPath), cmd.Line, cmd.Type, cmd.Filename)
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
			if validAccepted != want.accepted || validRejected != want.unsupported || invalidRejected != want.invalid || negativeInstantiation != want.negativeInstantiation || malformedBinary != want.malformedBinary || malformedText != want.malformedText {
				t.Fatalf("corpus accounting = accepted %d unsupported %d invalid %d negative-instantiation %d malformed binary/text %d/%d, want %d/%d/%d/%d/%d/%d", validAccepted, validRejected, invalidRejected, negativeInstantiation, malformedBinary, malformedText, want.accepted, want.unsupported, want.invalid, want.negativeInstantiation, want.malformedBinary, want.malformedText)
			}
		})
	}
}

func TestWazeroPortTypedFunctionReferenceElemInstantiationFailuresUseSpectestTable(t *testing.T) {
	dir := filepath.Clean("../../testdata/wazero/spectest-proposals/typed-function-references")
	table, err := NewTable(10, 20)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := table.Close(); err != nil {
			t.Errorf("close spectest.table: %v", err)
		}
	}()
	imports := Imports{"spectest.table": table}
	fixtures := []string{
		"elem.61.wasm", "elem.62.wasm", "elem.63.wasm", "elem.64.wasm",
		"elem.65.wasm", "elem.66.wasm", "elem.67.wasm", "elem.68.wasm",
		"elem.69.wasm", "elem.70.wasm", "elem.71.wasm", "elem.72.wasm",
	}
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, fixture))
			if err != nil {
				t.Fatal(err)
			}
			compiled, err := Compile(nil, data)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			in, instantiateErr := Instantiate(compiled, InstantiateOptions{Imports: imports})
			if in != nil {
				if err := in.Close(); err != nil {
					t.Errorf("close unexpected instance: %v", err)
				}
			}
			if err := compiled.Close(); err != nil {
				t.Errorf("close compiled module: %v", err)
			}
			if instantiateErr == nil {
				t.Fatal("module instantiated successfully, want active element-segment bounds failure")
			}
			message := strings.ToLower(instantiateErr.Error())
			if strings.Contains(message, "missing imported table") {
				t.Fatalf("failure came from the harness import setup, not the fixture oracle: %v", instantiateErr)
			}
			if !strings.Contains(message, "active element segment") || !strings.Contains(message, "out of bounds") {
				t.Fatalf("instantiate error = %v, want active element-segment out-of-bounds failure", instantiateErr)
			}
		})
	}
}

func isExplicitProposalRejection(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unsupported") || strings.Contains(message, "shared memory") || strings.Contains(message, "atomic")
}
