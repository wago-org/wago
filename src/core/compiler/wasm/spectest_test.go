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

// coreFiles are spec-testsuite .wast files whose modules are within this
// validator's current smoke-test scope. The harness validates modules and
// assertion-invalid cases; execution assertions remain out of scope here.
var coreFiles = []string{
	"i32", "i64", "f32", "f64", "f32_cmp", "f64_cmp", "f32_bitwise", "f64_bitwise",
	"int_exprs", "int_literals", "conversions", "forward", "fac",
	"block", "loop", "if", "br", "br_if", "br_table", "return", "call", "call_indirect",
	"select", "nop", "unreachable", "unreached-invalid", "unwind", "func", "labels",
	"switch", "stack", "local_get", "local_set", "local_tee", "global",
	"load", "store", "address", "align", "endianness", "memory_redundancy",
	"memory_size", "memory_grow", "left-to-right", "type", "func_ptrs",
}

type specCmd struct {
	Type       string `json:"type"`
	Line       int    `json:"line"`
	Filename   string `json:"filename"`
	Text       string `json:"text"`
	ModuleType string `json:"module_type"`
}

type specFile struct {
	Commands []specCmd `json:"commands"`
}

type specValidationStats struct {
	modulesPassed     int
	modulesSkipped    int
	modulesFailed     int
	assertionsPassed  int
	assertionsSkipped int
	assertionsFailed  int
}

func (s *specValidationStats) add(other specValidationStats) {
	s.modulesPassed += other.modulesPassed
	s.modulesSkipped += other.modulesSkipped
	s.modulesFailed += other.modulesFailed
	s.assertionsPassed += other.assertionsPassed
	s.assertionsSkipped += other.assertionsSkipped
	s.assertionsFailed += other.assertionsFailed
}

func TestSpecValidationStatsAccounting(t *testing.T) {
	var total specValidationStats
	total.add(specValidationStats{modulesPassed: 3, modulesSkipped: 2, assertionsPassed: 7})
	total.add(specValidationStats{modulesFailed: 1, assertionsPassed: 4, assertionsFailed: 2})
	want := specValidationStats{
		modulesPassed:    3,
		modulesSkipped:   2,
		modulesFailed:    1,
		assertionsPassed: 11,
		assertionsFailed: 2,
	}
	if total != want {
		t.Fatalf("stats = %+v, want %+v", total, want)
	}
}

func TestSpecSuitePlanIncludesGlobalWast(t *testing.T) {
	for _, base := range coreFiles {
		if base == "global" {
			return
		}
	}
	t.Fatal("coreFiles does not include official global.wast")
}

func isUnsupportedValidation(err error) bool {
	var ve *ValidationError
	return errors.As(err, &ve) && ve.Code == ErrUnsupportedValidationOpcode
}

// TestSpecSuite runs the official WebAssembly testsuite as a differential
// validation oracle. It is gated on WAGO_SPECTEST_DIR (the selected release
// checkout) and wast2json on PATH; skipped otherwise.
func TestSpecSuite(t *testing.T) {
	checkout := os.Getenv("WAGO_SPECTEST_DIR")
	if checkout == "" {
		t.Skip("set WAGO_SPECTEST_DIR to a checked-out WebAssembly testsuite to run")
	}
	version := os.Getenv("WAGO_SPEC_VERSION")
	if version == "" {
		version = "1.0"
	}
	dir, files := validationSpecPlan(t, checkout, version)
	wast2json, err := exec.LookPath("wast2json")
	if err != nil {
		t.Skip("wast2json (wabt) not on PATH")
	}
	tmp := t.TempDir()

	var total specValidationStats
	for _, base := range files {
		wast := filepath.Join(dir, base+".wast")
		if _, err := os.Stat(wast); err != nil {
			t.Errorf("%s: discovered corpus file is unavailable: %v", base, err)
			continue
		}
		name := strings.ReplaceAll(base, string(filepath.Separator), "_")
		jsonPath := filepath.Join(tmp, name+".json")
		out, err := exec.Command(wast2json, "--enable-all", wast, "-o", jsonPath).CombinedOutput()
		if err != nil {
			t.Errorf("%s: wast2json failed (%v): %s", base, err, out)
			continue
		}
		raw, err := os.ReadFile(jsonPath)
		if err != nil {
			t.Fatal(err)
		}
		var sf specFile
		if err := json.Unmarshal(raw, &sf); err != nil {
			t.Fatal(err)
		}

		var stats specValidationStats
		for _, c := range sf.Commands {
			if c.Filename == "" {
				continue
			}
			data, err := os.ReadFile(filepath.Join(tmp, c.Filename))
			if err != nil {
				switch c.Type {
				case "module":
					stats.modulesFailed++
				case "assert_invalid", "assert_malformed":
					stats.assertionsFailed++
				}
				t.Errorf("%s.wast:%d module output %q is unavailable: %v", base, c.Line, c.Filename, err)
				continue
			}
			switch c.Type {
			case "module":
				m, derr := DecodeModule(data)
				var verr error
				if derr == nil {
					verr = ValidateModule(m)
				}
				switch {
				case derr == nil && verr == nil:
					stats.modulesPassed++
				case isUnsupportedValidation(verr):
					stats.modulesSkipped++
				default:
					stats.modulesFailed++
					t.Errorf("%s.wast:%d valid module REJECTED: decode=%v validate=%v", base, c.Line, derr, verr)
				}
			case "assert_invalid":
				m, derr := DecodeModule(data)
				if derr == nil && ValidateModule(m) == nil {
					stats.assertionsFailed++
					t.Errorf("%s.wast:%d invalid module ACCEPTED (expected: %s)", base, c.Line, c.Text)
				} else {
					stats.assertionsPassed++
				}
			case "assert_malformed":
				if c.ModuleType != "binary" {
					stats.assertionsSkipped++
					continue
				}
				if _, derr := DecodeModule(data); derr == nil {
					stats.assertionsFailed++
					t.Errorf("%s.wast:%d malformed binary ACCEPTED (expected: %s)", base, c.Line, c.Text)
				} else {
					stats.assertionsPassed++
				}
			}
		}
		total.add(stats)
		t.Logf("%-40s modules(pass=%d fail=%d skip=%d) assertions(pass=%d fail=%d skip=%d)",
			base, stats.modulesPassed, stats.modulesFailed, stats.modulesSkipped,
			stats.assertionsPassed, stats.assertionsFailed, stats.assertionsSkipped)
	}
	t.Logf("TOTAL[%s]: modules passed=%d failed=%d skipped=%d | assertions passed=%d failed=%d skipped=%d",
		version, total.modulesPassed, total.modulesFailed, total.modulesSkipped,
		total.assertionsPassed, total.assertionsFailed, total.assertionsSkipped)
	if total.modulesPassed+total.modulesSkipped+total.modulesFailed == 0 {
		t.Errorf("no validation modules were accounted — harness or corpus misconfigured")
	}
	if total.assertionsPassed+total.assertionsSkipped+total.assertionsFailed == 0 {
		t.Errorf("no validation assertions were accounted — harness or corpus misconfigured")
	}
}

func validationSpecPlan(t *testing.T, checkout, version string) (dir string, files []string) {
	t.Helper()
	if version == "2.0" {
		suite, err := spectest.DiscoverRelease2(checkout)
		if err != nil {
			t.Fatal(err)
		}
		return suite.CoreDir, suite.Files
	}
	return checkout, coreFiles
}
