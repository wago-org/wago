package wasm

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/internal/spectest"
)

func TestRelease2MalformedMemoryReservedZeroSites(t *testing.T) {
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

	want := map[int]bool{
		857: true, 877: true, 897: true, 916: true, 935: true,
		955: true, 974: true, 993: true, 1011: true, 1029: true,
	}
	tmp := t.TempDir()
	jsonPath := filepath.Join(tmp, "binary.json")
	out, err := exec.Command(wast2json, "--enable-all", filepath.Join(suite.CoreDir, "binary.wast"), "-o", jsonPath).CombinedOutput()
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

	paths := []struct {
		name   string
		decode func([]byte) error
	}{
		{name: "DecodeModule", decode: func(b []byte) error { _, err := DecodeModule(b); return err }},
		{name: "DecodeModuleByteBacked", decode: func(b []byte) error { _, err := DecodeModuleByteBacked(b); return err }},
	}
	seen := make(map[int]bool, len(want))
	for _, c := range sf.Commands {
		if c.Type != "assert_malformed" || c.ModuleType != "binary" || c.Filename == "" || !want[c.Line] {
			continue
		}
		data, err := os.ReadFile(filepath.Join(tmp, c.Filename))
		if err != nil {
			t.Fatalf("binary.wast:%d: %v", c.Line, err)
		}
		for _, path := range paths {
			if err := path.decode(data); err == nil {
				t.Errorf("binary.wast:%d %s accepted malformed memory reserved immediate", c.Line, path.name)
			}
		}
		seen[c.Line] = true
	}
	for line := range want {
		if !seen[line] {
			t.Errorf("binary.wast:%d malformed assertion site not emitted", line)
		}
	}
}
