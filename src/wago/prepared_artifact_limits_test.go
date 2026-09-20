//go:build (linux || darwin || windows) && (amd64 || arm64)

package wago

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func preparedArtifactLimitSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{1, 0, 1}, []byte{1, 0, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("answer", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 42, 0x0b}))),
	)
}

func TestPreparedArtifactAdmissionLimits(t *testing.T) {
	if !SupportedFeatures().IsEnabled(CoreFeatureMultiMemory) {
		t.Skip("multi-memory is unsupported")
	}
	source := preparedArtifactLimitSource()
	base := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)
	compiled, err := Compile(base, source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	codeBytes := uint64(compiled.CodeSize())
	if codeBytes < 2 {
		t.Fatal("fixture has no native code")
	}
	artifact, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		cfg    *RuntimeConfig
		reject bool
	}{
		{"code-below", base.WithMaxNativeCodeBytes(codeBytes - 1), true},
		{"code-exact", base.WithMaxNativeCodeBytes(codeBytes), false},
		{"memory-below", base.WithMaxMemoriesPerModule(1), true},
		{"memory-exact", base.WithMaxMemoriesPerModule(2), false},
	} {
		for _, path := range []string{"compile", "module", "adopt", "prepared"} {
			t.Run(tc.name+"/"+path, func(t *testing.T) {
				rt := NewRuntime(WithRuntimeConfig(tc.cfg))
				defer rt.Close()
				errorsSeen, successesSeen := 0, 0
				rt.storeHooks(&hookRegistry{
					onCompileError: []func(ModuleCompileErrorEvent){func(event ModuleCompileErrorEvent) {
						errorsSeen++
						if event.Err == nil {
							t.Error("error observer received no error")
						}
					}},
					afterCompile: []func(ModuleCompiledEvent){func(ModuleCompiledEvent) { successesSeen++ }},
				})
				var decoded *Compiled
				var module *Module
				var err error
				if path == "compile" {
					module, err = rt.Compile(source)
				} else {
					decoded, err = LoadTrustedArtifact(artifact)
					if err != nil {
						t.Fatal(err)
					}
					defer decoded.Close()
					switch path {
					case "module":
						module, err = rt.Module(decoded)
					case "adopt":
						module, err = rt.AdoptModule(decoded)
					case "prepared":
						prepared, prepErr := rt.PrepareCompile(source)
						if prepErr != nil {
							t.Fatal(prepErr)
						}
						defer prepared.Close()
						module, err = prepared.Adopt(decoded)
					}
				}
				if module != nil {
					defer module.Close()
				}
				if tc.reject {
					if module != nil || err == nil {
						t.Fatalf("admission = %v, %v; want rejection", module, err)
					}
					// Source memory-count checks predate ResourceLimitError; artifact
					// admission paths must retain the structured error contract.
					if path != "compile" && !errors.Is(err, ErrResourceLimit) {
						t.Fatalf("error = %v, want resource limit", err)
					}
					if errorsSeen != 1 || successesSeen != 0 {
						t.Fatalf("observers = errors %d, successes %d", errorsSeen, successesSeen)
					}
					if decoded != nil {
						closed := decoded.checkOpen() != nil
						if closed != (path != "module") {
							t.Fatalf("artifact closed = %v on %s rejection", closed, path)
						}
					}
				} else {
					if err != nil || module == nil {
						t.Fatalf("exact-limit admission = %v, %v", module, err)
					}
					if errorsSeen != 0 || successesSeen != 1 {
						t.Fatalf("observers = errors %d, successes %d", errorsSeen, successesSeen)
					}
				}
			})
		}
	}
}

func TestPreparedArtifactAdmissionUsesCapturedConfig(t *testing.T) {
	source := benchAddOneModule()
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	codeBytes := uint64(compiled.CodeSize())
	artifact, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	for _, strictPreparation := range []bool{false, true} {
		cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)
		if strictPreparation {
			cfg = cfg.WithMaxNativeCodeBytes(codeBytes - 1)
		}
		rt := NewRuntime(WithRuntimeConfig(cfg))
		prepared, err := rt.PrepareCompile(source)
		if err != nil {
			t.Fatal(err)
		}
		// Replace the runtime's private pointer to model a later generation.
		// The preparation must continue to use its admitted snapshot.
		rt.mu.Lock()
		if strictPreparation {
			rt.cfg = cfg.WithMaxNativeCodeBytes(0)
		} else {
			rt.cfg = cfg.WithMaxNativeCodeBytes(codeBytes - 1)
		}
		rt.mu.Unlock()
		decoded, err := LoadTrustedArtifact(artifact)
		if err != nil {
			t.Fatal(err)
		}
		module, err := prepared.Adopt(decoded)
		if module != nil {
			module.Close()
		}
		prepared.Close()
		rt.Close()
		if strictPreparation && !errors.Is(err, ErrResourceLimit) || !strictPreparation && err != nil {
			t.Fatalf("strict preparation %v: error = %v", strictPreparation, err)
		}
	}
}

func BenchmarkPreparedArtifactAdmission(b *testing.B) {
	source := benchAddOneModule()
	cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)
	compiled, err := Compile(cfg, source)
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	artifact, err := compiled.MarshalBinary()
	if err != nil {
		b.Fatal(err)
	}
	for _, path := range []string{"module", "prepared"} {
		b.Run(path, func(b *testing.B) {
			rt := NewRuntime(WithRuntimeConfig(cfg))
			defer rt.Close()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				decoded, err := LoadTrustedArtifact(artifact)
				if err != nil {
					b.Fatal(err)
				}
				var module *Module
				if path == "module" {
					module, err = rt.AdoptModule(decoded)
				} else {
					prepared, prepErr := rt.PrepareCompile(source)
					if prepErr != nil {
						decoded.Close()
						b.Fatal(prepErr)
					}
					module, err = prepared.Adopt(decoded)
				}
				if err != nil {
					b.Fatal(err)
				}
				if err := module.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
