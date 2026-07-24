package wasm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWazeroPortExtendedConstValidation(t *testing.T) {
	root := filepath.Clean("../../../../testdata/wazero/extended-const")
	var valid, invalid, malformedBinary, malformedText int
	for _, base := range []string{"data", "elem", "global"} {
		raw, err := os.ReadFile(filepath.Join(root, base+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var sf specFile
		if err := json.Unmarshal(raw, &sf); err != nil {
			t.Fatalf("decode %s.json: %v", base, err)
		}
		for _, cmd := range sf.Commands {
			if cmd.Filename == "" {
				continue
			}
			path := filepath.Join(root, cmd.Filename)
			switch cmd.Type {
			case "module":
				valid++
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				m, err := DecodeModule(data)
				if err != nil {
					t.Errorf("%s:%d valid module decode: %v", base, cmd.Line, err)
					continue
				}
				if err := ValidateModule(m); err != nil {
					t.Errorf("%s:%d valid module validate: %v", base, cmd.Line, err)
				}
			case "assert_invalid":
				invalid++
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				m, decodeErr := DecodeModule(data)
				var validateErr error
				if decodeErr == nil {
					validateErr = ValidateModule(m)
				}
				if decodeErr == nil && validateErr == nil {
					t.Errorf("%s:%d invalid module accepted: %s", base, cmd.Line, cmd.Text)
				} else if isUnsupportedValidation(validateErr) {
					t.Errorf("%s:%d extended-const invalid module reached unsupported validation: %v", base, cmd.Line, validateErr)
				}
			case "assert_malformed":
				if cmd.ModuleType != "binary" {
					malformedText++ // Wago intentionally has no WAT parser.
					if _, err := os.Stat(path); err != nil {
						t.Errorf("%s:%d malformed text fixture missing: %v", base, cmd.Line, err)
					}
					continue
				}
				malformedBinary++
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := DecodeModule(data); err == nil {
					t.Errorf("%s:%d malformed binary accepted: %s", base, cmd.Line, cmd.Text)
				}
			default:
				t.Errorf("%s:%d unhandled fixture command %q with file %s", base, cmd.Line, cmd.Type, cmd.Filename)
			}
		}
	}
	if valid != 13 || invalid != 37 || malformedBinary != 4 || malformedText != 3 {
		t.Fatalf("extended-const accounting = valid %d invalid %d malformed binary/text %d/%d, want 13/37/4/3", valid, invalid, malformedBinary, malformedText)
	}
}
