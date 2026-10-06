//go:build linux && amd64 && !tinygo

package amd64

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestSignedImmediateStoreSelection(t *testing.T) {
	form := os.Getenv("WAGO_SIGNED_IMM64_FORM")
	if form == "" {
		form = "qword"
	}
	for _, guard := range []bool{false, true} {
		for _, profile := range []shared.AMD64Features{0, shared.AMD64KnownFeatures} {
			for _, value := range []int64{0, -1, math.MinInt32, math.MaxInt32, math.MinInt32 - 1, math.MaxInt32 + 1, 0xffffffff, 0x1234567887654321} {
				for _, size := range []int{1, 2, 4, 8} {
					t.Run(fmt.Sprintf("guard=%t/features=%x/value=%x/size=%d", guard, profile, uint64(value), size), func(t *testing.T) {
						m, err := wasm.DecodeModule(wasmtest.SignedImmediateStore(value, size, false, false, false, 7))
						if err != nil {
							t.Fatal(err)
						}
						if err := wasm.ValidateModule(m); err != nil {
							t.Fatal(err)
						}
						cm, err := CompileModuleWith(m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: profile, ElideBoundsChecks: guard})
						if err != nil {
							t.Fatal(err)
						}
						defer cm.CodeImage.Close()
						listing := disasm(t, cm.Code)
						rx := regexp.MustCompile(`mov\s+(qword|dword|word|byte) ptr \[rbx\+[^\]]*\],`)
						matches := rx.FindAllStringSubmatch(listing, -1)
						width, want := map[int]string{1: "byte", 2: "word", 4: "dword", 8: "dword"}[size], 1
						if size == 8 {
							want = 2
							if form == "qword" && (guard || value >= math.MinInt32 && value <= math.MaxInt32) {
								width, want = "qword", 1
							}
						}
						if len(matches) != want {
							t.Fatalf("stores=%d want=%d\n%s", len(matches), want, listing)
						}
						for _, match := range matches {
							if match[1] != width {
								t.Fatalf("store width=%s want=%s\n%s", match[1], width, listing)
							}
						}
						if profile == 0 {
							assertScalarBaseline(t, cm.Code)
						}
					})
				}
			}
		}
	}
}

func TestSignedImmediateStoreLoopCode(t *testing.T) {
	for _, pressure := range []bool{false, true} {
		for _, value := range []int64{-1, math.MinInt32, 0x80000000} {
			m, err := wasm.DecodeModule(wasmtest.SignedImmediateStore(value, 8, true, pressure, false, 0))
			if err != nil {
				t.Fatal(err)
			}
			var stats ModuleStats
			opts := CompileOptions{ElideBoundsChecks: os.Getenv("WAGO_STORE_IMM_GUARD") == "1"}
			if diagnosticsEnabled {
				opts.Stats = &stats
			}
			cm, err := CompileModuleWith(m, opts)
			if err != nil {
				t.Fatal(err)
			}
			if dir := os.Getenv("WAGO_STORE_IMM_DUMP"); dir != "" {
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				name := fmt.Sprintf("value-%x-pressure-%v", uint64(value), pressure)
				if err := os.WriteFile(filepath.Join(dir, name+".asm"), []byte(disasm(t, cm.Code)), 0644); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, name+".bin"), cm.Code, 0644); err != nil {
					t.Fatal(err)
				}
				counts := make([]map[string]int, 0, len(stats.Funcs))
				for _, f := range stats.Funcs {
					counts = append(counts, map[string]int{"spills": f.Spills, "reloads": f.Reloads})
				}
				data, err := json.MarshalIndent(counts, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, name+".stats.json"), data, 0644); err != nil {
					t.Fatal(err)
				}
			}
			cm.CodeImage.Close()
		}
	}
}

// A constant need not occupy a register while the address tree needs scratch
// registers. Keep the seven live guest values resident across this store loop.
func TestSignedImmediateStoreGuardPressure(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, value := range []int64{0x80000000, math.MinInt32 - 1, 0x1234567887654321} {
		for _, profile := range []shared.AMD64Features{0, shared.AMD64KnownFeatures} {
			t.Run(fmt.Sprintf("value=%x/features=%x", uint64(value), profile), func(t *testing.T) {
				m, err := wasm.DecodeModule(wasmtest.SignedImmediateStore(value, 8, true, true, false, 0))
				if err != nil {
					t.Fatal(err)
				}
				var stats ModuleStats
				cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, ElideBoundsChecks: true, AMD64FeaturesSet: true, AMD64Features: profile})
				if err != nil {
					t.Fatal(err)
				}
				defer cm.CodeImage.Close()
				if len(stats.Funcs) != 1 {
					t.Fatalf("function count=%d, want 1", len(stats.Funcs))
				}
				if got := stats.Funcs[0].Spills; got != 0 {
					t.Fatalf("guarded constant-store loop spilled %d live values, want 0", got)
				}
			})
		}
	}
}
