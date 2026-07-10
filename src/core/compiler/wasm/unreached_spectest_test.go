package wasm

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/internal/spectest"
)

func TestRelease2UnreachedBrTableValidationSite(t *testing.T) {
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

	tmp := t.TempDir()
	jsonPath := filepath.Join(tmp, "unreached-valid.json")
	out, err := exec.Command(wast2json, "--enable-all", filepath.Join(suite.CoreDir, "unreached-valid.wast"), "-o", jsonPath).CombinedOutput()
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

	const wantLine = 49
	seen := false
	for _, c := range sf.Commands {
		if c.Type != "module" || c.Filename == "" || c.Line != wantLine {
			continue
		}
		data, err := os.ReadFile(filepath.Join(tmp, c.Filename))
		if err != nil {
			t.Fatalf("unreached-valid.wast:%d: %v", wantLine, err)
		}
		for _, path := range []struct {
			name     string
			validate func([]byte) error
		}{
			{name: "AST", validate: decodeThenValidate},
			{name: "byte-backed", validate: byteBackedDecodeThenValidate},
		} {
			if err := path.validate(data); err != nil {
				t.Errorf("unreached-valid.wast:%d %s valid module rejected: %v", wantLine, path.name, err)
			}
		}
		seen = true
	}
	if !seen {
		t.Fatalf("unreached-valid.wast:%d valid module site not emitted", wantLine)
	}
}
