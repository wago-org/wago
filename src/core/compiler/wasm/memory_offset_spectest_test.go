package wasm

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/internal/spectest"
)

func TestRelease2MalformedMemoryOffsetSites(t *testing.T) {
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
		"binary": {
			483: true, 540: true, 620: true, 639: true, 733: true, 752: true,
		},
		"binary-leb128": {
			405: true, 462: true, 731: true, 750: true, 844: true, 863: true,
		},
	}
	paths := []struct {
		name   string
		decode func([]byte) error
	}{
		{name: "DecodeModule", decode: func(b []byte) error { _, err := DecodeModule(b); return err }},
		{name: "DecodeModuleByteBacked", decode: func(b []byte) error { _, err := DecodeModuleByteBacked(b); return err }},
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
				if c.Type != "assert_malformed" || c.ModuleType != "binary" || c.Filename == "" || !lines[c.Line] {
					continue
				}
				data, err := os.ReadFile(filepath.Join(tmp, c.Filename))
				if err != nil {
					t.Fatalf("%s.wast:%d: %v", base, c.Line, err)
				}
				for _, path := range paths {
					if err := path.decode(data); err == nil {
						t.Errorf("%s.wast:%d %s accepted malformed memory32 offset", base, c.Line, path.name)
					}
				}
				seen[c.Line] = true
			}
			for line := range lines {
				if !seen[line] {
					t.Errorf("%s.wast:%d malformed assertion site not emitted", base, line)
				}
			}
		})
	}
}
