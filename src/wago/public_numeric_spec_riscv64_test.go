//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/simdfixture"
)

const rv64NumericSpecSourcesSHA256 = "835b071fa6954918a49af2d7af5efc38c3af60e86d0ef5f1e5bea2dd79037614"
const rv64NumericSpecFilesSHA256 = "e0db306f33f940b5ace33f3a2c77e977a8533d30627b4f1c66603bad8384fec9"

// TestRV64PublicPinnedNumericSpecExecution qualifies four complete pinned
// numeric files. Text syntax assertions are explicitly outside the binary API.
func TestRV64PublicPinnedNumericSpecExecution(t *testing.T) {
	dir := os.Getenv("WAGO_RV64_NUMERIC_SPEC_JSON_DIR")
	if dir == "" {
		t.Skip("prepare repository-pinned fixtures with scripts/prepare-riscv64-numeric-spec.py and set WAGO_RV64_NUMERIC_SPEC_JSON_DIR")
	}
	manifest, err := rv64PublicNumericManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"modules": 22, "assert_return": 1339, "assert_trap": 101, "assert_invalid": 137, "assert_invalid_validate": 137, "excluded_text_assert_malformed": 4}
	rv64PublicSpecRun(t, dir, manifest, want)
}

func rv64PublicNumericManifest(dir string) (*simdfixture.Manifest, error) {
	data, err := simdfixture.Read(dir, "rv64-numeric-manifest.json", 256<<10)
	if err != nil {
		return nil, err
	}
	var manifest simdfixture.Manifest
	if err := simdfixture.Decode(data, &manifest); err != nil {
		return nil, err
	}
	if err := rv64PublicNumericProvenance(&manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func rv64PublicNumericProvenance(manifest *simdfixture.Manifest) error {
	archives := map[string]bool{
		"83f8122e924745fcd70636e3594bc01c4c47f2d4c8f3c63b5d70d3f83a482677": true, // Linux amd64
		"5e35416ee8725dc7cc0572e4392a8117cbf008b0e34c0db65c75506b0299cdbf": true, // Linux arm64
		"e5269d6bbe05dfeb179e4f21111b3a641d6ccaa38b0b21d472ae5c65f8c4ff5d": true, // Darwin arm64
		"ca9e69cc1de13b4633a3c74fd697319303b21108529d4f10960af4e1f4a65893": true, // Darwin amd64 source
	}
	binaryHash, err := hex.DecodeString(manifest.WabtBinarySHA256)
	if manifest.SpecCommit != "05ca4182176763112561ae20153975c12bd689e4" || manifest.WabtVersion != "1.0.41" ||
		!archives[manifest.WabtArchiveSHA256] || err != nil || len(binaryHash) != sha256.Size || len(manifest.Sources) != 4 || len(manifest.Files) != 167 {
		return fmt.Errorf("unexpected pinned numeric source/tool provenance")
	}
	for _, expected := range []struct {
		rows map[string]string
		hash string
	}{{manifest.Sources, rv64NumericSpecSourcesSHA256}, {manifest.Files, rv64NumericSpecFilesSHA256}} {
		actual, err := simdfixture.HashRows(expected.rows)
		if err != nil || actual != expected.hash {
			return fmt.Errorf("pinned numeric fixture manifest differs: got %s, want %s (%v)", actual, expected.hash, err)
		}
	}
	return nil
}

func TestRV64PublicSpecNumericTrapGate(t *testing.T) {
	cases := []struct {
		text     string
		overflow coreruntime.TrapCode
		want     coreruntime.TrapCode
		known    bool
	}{
		{"integer divide by zero", coreruntime.TrapDivOverflow, coreruntime.TrapDivZero, true},
		{"integer overflow", coreruntime.TrapDivOverflow, coreruntime.TrapDivOverflow, true},
		{"integer overflow", coreruntime.TrapTruncOverflow, coreruntime.TrapTruncOverflow, true},
		{"integer overflow", coreruntime.TrapUnreachable, coreruntime.TrapUnreachable, false},
		{"invalid conversion to integer", coreruntime.TrapTruncOverflow, coreruntime.TrapTruncOverflow, true},
		{"out of bounds memory access", coreruntime.TrapDivOverflow, coreruntime.TrapLinMemOutOfBounds, true},
		{"new trap diagnostic", coreruntime.TrapDivOverflow, 0, false},
	}
	for _, tc := range cases {
		got, known := rv64PublicSpecTrapCode(tc.text, tc.overflow)
		if got != tc.want || known != tc.known {
			t.Fatalf("trap %q/%v: got %v/%v want %v/%v", tc.text, tc.overflow, got, known, tc.want, tc.known)
		}
	}
}
