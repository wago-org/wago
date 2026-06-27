package wasm3

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
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
// validation oracle. It is gated on WAGO_SPECTEST_DIR (a checked-out
// WebAssembly/testsuite) and wast2json on PATH; skipped otherwise.
func TestSpecSuite(t *testing.T) {
	dir := os.Getenv("WAGO_SPECTEST_DIR")
	if dir == "" {
		t.Skip("set WAGO_SPECTEST_DIR to a checked-out WebAssembly/testsuite to run")
	}
	wast2json, err := exec.LookPath("wast2json")
	if err != nil {
		t.Skip("wast2json (wabt) not on PATH")
	}
	tmp := t.TempDir()

	var totModOK, totModSkip, totInvalidRej, totInvalidAcc, totMalRej, totMalAcc int
	for _, base := range coreFiles {
		wast := filepath.Join(dir, base+".wast")
		if _, err := os.Stat(wast); err != nil {
			continue
		}
		jsonPath := filepath.Join(tmp, base+".json")
		out, err := exec.Command(wast2json, "--enable-all", wast, "-o", jsonPath).CombinedOutput()
		if err != nil {
			t.Logf("%s: wast2json failed (%v): %s", base, err, out)
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

		modOK, modSkip, invRej, invAcc, malRej, malAcc := 0, 0, 0, 0, 0, 0
		for _, c := range sf.Commands {
			if c.Filename == "" {
				continue
			}
			data, err := os.ReadFile(filepath.Join(tmp, c.Filename))
			if err != nil {
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
					modOK++
				case isUnsupportedValidation(verr):
					modSkip++
				default:
					t.Errorf("%s.wast:%d valid module REJECTED: decode=%v validate=%v", base, c.Line, derr, verr)
				}
			case "assert_invalid":
				m, derr := DecodeModule(data)
				if derr == nil && ValidateModule(m) == nil {
					invAcc++
					t.Errorf("%s.wast:%d invalid module ACCEPTED (expected: %s)", base, c.Line, c.Text)
				} else {
					invRej++
				}
			case "assert_malformed":
				if c.ModuleType != "binary" {
					continue
				}
				if _, derr := DecodeModule(data); derr == nil {
					malAcc++ // soft: decoder did not catch a malformed binary
				} else {
					malRej++
				}
			}
		}
		t.Logf("%-18s modOK=%d skip=%d  invalid rej=%d acc=%d  malformed rej=%d acc=%d",
			base, modOK, modSkip, invRej, invAcc, malRej, malAcc)
		totModOK += modOK
		totModSkip += modSkip
		totInvalidRej += invRej
		totInvalidAcc += invAcc
		totMalRej += malRej
		totMalAcc += malAcc
	}
	t.Logf("TOTAL: valid modules ok=%d skipped(unsupported validation)=%d | assert_invalid rejected=%d accepted=%d | assert_malformed rejected=%d accepted=%d",
		totModOK, totModSkip, totInvalidRej, totInvalidAcc, totMalRej, totMalAcc)
}
