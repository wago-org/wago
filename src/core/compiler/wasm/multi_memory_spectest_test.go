package wasm

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/spectest"
)

func TestRelease2MultipleMemoryValidationSites(t *testing.T) {
	checkout := os.Getenv("WAGO_SPECTEST_DIR")
	if checkout == "" || os.Getenv("WAGO_SPEC_VERSION") != "2.0" {
		t.Skip("set WAGO_SPECTEST_DIR and WAGO_SPEC_VERSION=2.0 to run the Release 2 proof")
	}
	suite, err := spectest.DiscoverRelease2(checkout)
	if err != nil {
		t.Fatal(err)
	}
	wast2json, err := exec.LookPath("wast2json")
	if err != nil {
		t.Skip("wast2json (wabt) not on PATH")
	}

	want := map[string]map[int]bool{
		"imports": {483: true, 487: true, 491: true},
		"memory":  {10: true, 11: true},
	}
	paths := []struct {
		name     string
		validate func([]byte) error
	}{
		{name: "AST", validate: decodeThenValidate},
		{name: "byte-backed", validate: byteBackedDecodeThenValidate},
	}

	for base, lines := range want {
		t.Run(base, func(t *testing.T) {
			tmp := t.TempDir()
			jsonPath := filepath.Join(tmp, base+".json")
			out, err := exec.Command(wast2json, "--enable-all", filepath.Join(suite.CoreDir, base+".wast"), "-o", jsonPath).CombinedOutput()
			if err != nil {
				t.Fatalf("wast2json: %v: %s", err, out)
			}
			raw, err := os.ReadFile(jsonPath)
			if err != nil {
				t.Fatal(err)
			}
			var sf specFile
			if err := json.Unmarshal(raw, &sf); err != nil {
				t.Fatal(err)
			}

			seen := make(map[int]bool, len(lines))
			for _, c := range sf.Commands {
				if c.Type != "assert_invalid" || c.Filename == "" || !lines[c.Line] {
					continue
				}
				data, err := os.ReadFile(filepath.Join(tmp, c.Filename))
				if err != nil {
					t.Fatalf("%s.wast:%d: %v", base, c.Line, err)
				}
				for _, path := range paths {
					err := path.validate(data)
					var ve *ValidationError
					if !errors.As(err, &ve) || ve.Code != ErrUnsupportedFeature || !strings.Contains(err.Error(), "multiple memories") {
						t.Errorf("%s.wast:%d %s error = %v, want ErrUnsupportedFeature containing %q", base, c.Line, path.name, err, "multiple memories")
					}
				}
				seen[c.Line] = true
			}
			for line := range lines {
				if !seen[line] {
					t.Errorf("%s.wast:%d invalid assertion site not emitted", base, line)
				}
			}
		})
	}
}
