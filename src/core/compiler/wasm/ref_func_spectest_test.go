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

func TestRelease2RefFuncValidationSites(t *testing.T) {
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
	jsonPath := filepath.Join(tmp, "ref_func.json")
	out, err := exec.Command(wast2json, "--enable-all", filepath.Join(suite.CoreDir, "ref_func.wast"), "-o", jsonPath).CombinedOutput()
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

	wantValid := map[int]bool{1: true, 6: true, 80: true}
	wantInvalid := map[int]string{
		69:  "unknown function",
		109: "undeclared function reference",
		113: "undeclared function reference",
	}
	seenValid := map[int]bool{}
	seenInvalid := map[int]bool{}
	for _, c := range sf.Commands {
		if c.Filename == "" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(tmp, c.Filename))
		if err != nil {
			t.Fatalf("ref_func.wast:%d: %v", c.Line, err)
		}
		switch c.Type {
		case "module":
			if !wantValid[c.Line] {
				continue
			}
			m, err := DecodeModule(data)
			if err == nil {
				err = ValidateModule(m)
			}
			if err != nil {
				t.Errorf("ref_func.wast:%d valid module rejected: %v", c.Line, err)
			}
			seenValid[c.Line] = true
		case "assert_invalid":
			want, ok := wantInvalid[c.Line]
			if !ok {
				continue
			}
			m, err := DecodeModule(data)
			if err == nil {
				err = ValidateModule(m)
			}
			var ve *ValidationError
			if !errors.As(err, &ve) || ve.Code != ErrUnknownFunc || !strings.Contains(err.Error(), want) {
				t.Errorf("ref_func.wast:%d error = %v, want ErrUnknownFunc containing %q", c.Line, err, want)
			}
			seenInvalid[c.Line] = true
		}
	}
	for line := range wantValid {
		if !seenValid[line] {
			t.Errorf("ref_func.wast:%d valid module site not emitted", line)
		}
	}
	for line := range wantInvalid {
		if !seenInvalid[line] {
			t.Errorf("ref_func.wast:%d invalid assertion site not emitted", line)
		}
	}
}
