//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// These are the remaining assert_invalid binaries generated without debug
// names by WABT 1.0.41 from the Release 3 core tests at revision
// 9d36019973201a19f9c9ebb0f10828b2fe2374aa.
func TestRV64Core3NullabilityPinnedInvalidModules(t *testing.T) {
	tests := []struct {
		name, encoded, hash, detail string
	}{
		{
			name:    "br_on_null.3",
			encoded: "0061736d01000000010f03600000600163000170600170017003030201020a1202040020000b0b00d0002000d5001a10000b",
			hash:    "4c986e71dd2b81c992c1ec0274b5276889e9047304772690e71052c4b80c1fd1",
			detail:  "funcref is not ref null type 0",
		},
		{
			name:    "br_on_non_null.3",
			encoded: "0061736d0100000001100360000060016300017060017002707003030201020a1302040020000b0c00d0002000d600100020000b",
			hash:    "fa4a0cca126ce9d6b0edafc81a2bebc51cb1b2c2214bc89f9c07c1e52453929b",
			detail:  "funcref is not ref null type 0",
		},
		{
			name:    "ref_as_non_null.1",
			encoded: "0061736d01000000010d036000017f600164000060000003030201020a0f0206002000d41a0b0600d00010000b",
			hash:    "7d3cc5ca0d509b2366f97c78c002a645999839ad5f09a58ffb93e1d1a2fdb05a",
			detail:  "ref null type 0 is not ref type 0",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source, err := hex.DecodeString(tc.encoded)
			if err != nil {
				t.Fatal(err)
			}
			if got := fmt.Sprintf("%x", sha256.Sum256(source)); got != tc.hash {
				t.Fatalf("pinned source hash = %s, want %s", got, tc.hash)
			}
			m, err := wasm.DecodeModule(source)
			if err != nil || m == nil {
				t.Fatalf("pinned assert_invalid must decode: module=%v err=%v", m, err)
			}
			assertRV64Core3NullabilityTypeMismatch(t, wasm.ValidateModule(m), tc.detail)
			if os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
				// Invalid sources can never create an artifact for the compiler-free
				// runtime. The architecture-neutral decoder and validator checks above
				// remain meaningful in that mode.
				return
			}

			// A validation failure cannot produce a Compiled value, executable
			// publication, or a serialized-artifact path. Repeat both public
			// compile boundaries under a one-byte native-code quota to ensure the
			// type error still wins before code generation and its resource checks.
			cfg := rv64NativeTailConfig().WithMaxNativeCodeBytes(1)
			for range 16 {
				compiled, err := cfg.Compile(source)
				if compiled != nil {
					_ = compiled.Close()
					t.Fatal("invalid module produced a compiled value and artifact path")
				}
				assertRV64Core3NullabilityTypeMismatch(t, err, tc.detail)
			}

			rt := NewRuntime(WithRuntimeConfig(cfg))
			for range 16 {
				module, err := rt.Compile(source)
				if module != nil {
					_ = module.Close()
					t.Fatal("invalid module reached runtime publication")
				}
				assertRV64Core3NullabilityTypeMismatch(t, err, tc.detail)
			}
			rt.mu.Lock()
			clean := rt.activeOperations == 0 && rt.compileOperations == 0 &&
				rt.moduleCloseOperations == 0 && len(rt.instances) == 0 &&
				len(rt.instanceReservations) == 0 && rt.directInstanceCount == 0 &&
				rt.nativeMemoryMappings == 0 && rt.nativeMemoryMappingsPeak == 0
			rt.mu.Unlock()
			if !clean {
				t.Fatal("repeated validation rejection retained runtime publication state")
			}
			if err := rt.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func assertRV64Core3NullabilityTypeMismatch(t *testing.T, err error, detail string) {
	t.Helper()
	var validation *wasm.ValidationError
	if !errors.As(err, &validation) || validation.Code != wasm.ErrTypeMismatch ||
		validation.Func != 1 || validation.Detail != detail {
		t.Fatalf("validation rejection = %#v (%v), want function 1 type mismatch %q", validation, err, detail)
	}
}
