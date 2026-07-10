package wasm

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/internal/spectest"
)

func TestRelease2ImplicitReferenceSelectValidationSite(t *testing.T) {
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
	jsonPath := filepath.Join(tmp, "select.json")
	out, err := exec.Command(wast2json, "--enable-all", filepath.Join(suite.CoreDir, "select.wast"), "-o", jsonPath).CombinedOutput()
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

	const line = 340
	paths := []struct {
		name     string
		validate func([]byte) error
	}{
		{name: "AST", validate: decodeThenValidate},
		{name: "byte-backed", validate: byteBackedDecodeThenValidate},
	}
	seen := false
	for _, c := range sf.Commands {
		if c.Type != "assert_invalid" || c.Filename == "" || c.Line != line {
			continue
		}
		data, err := os.ReadFile(filepath.Join(tmp, c.Filename))
		if err != nil {
			t.Fatalf("select.wast:%d: %v", line, err)
		}
		for _, path := range paths {
			err := path.validate(data)
			var ve *ValidationError
			if !errors.As(err, &ve) || ve.Code != ErrTypeMismatch {
				t.Errorf("select.wast:%d %s error = %v, want ErrTypeMismatch", line, path.name, err)
			}
		}
		seen = true
	}
	if !seen {
		t.Fatalf("select.wast:%d invalid assertion site not emitted", line)
	}
}
