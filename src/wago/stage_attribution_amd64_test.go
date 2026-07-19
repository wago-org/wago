//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type core3StageFixture struct {
	name        string
	path        string
	env         string
	core3       bool
	instantiate bool
	linkCold    bool
	initExport  string
	execExport  string
	execArgs    []uint64
}

func core3StageFixtures() []core3StageFixture {
	root := filepath.Clean("../..")
	corpus := filepath.Join(root, "bench", "corpus")
	return []core3StageFixture{
		{name: "tiny", path: filepath.Join(corpus, "tiny.wasm"), instantiate: true, execExport: "add", execArgs: []uint64{I32(7), I32(5)}},
		{name: "json-as", path: filepath.Join(corpus, "json-as.wasm"), instantiate: true, initExport: "_initialize", execExport: "serializeN", execArgs: []uint64{I32(200)}},
		{name: "wasm3", path: filepath.Join(corpus, "wasm3.wasm")},
		{name: "sqlite3", path: filepath.Join(corpus, "sqlite3.wasm")},
		{name: "ruby", path: filepath.Join(corpus, "ruby.wasm")},
		{name: "esbuild", path: filepath.Join(corpus, "esbuild.wasm")},
		{name: "moonbit-json", env: "WAGO_MOONBIT_JSON_SMOKE_WASM", core3: true, instantiate: true, execExport: "run", execArgs: []uint64{I32(1)}},
		{name: "starshine", env: "WAGO_STARSHINE_SMOKE_WASM", core3: true, instantiate: true, linkCold: true},
	}
}

func loadCore3StageFixture(tb testing.TB, fixture core3StageFixture) []byte {
	tb.Helper()
	path := fixture.path
	if fixture.env != "" {
		path = os.Getenv(fixture.env)
		if path == "" {
			tb.Skipf("set %s to benchmark %s", fixture.env, fixture.name)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		tb.Fatalf("read %s: %v", fixture.name, err)
	}
	return data
}

func core3StageConfig(fixture core3StageFixture) *RuntimeConfig {
	cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithFunctionWorkers(1)
	if fixture.core3 {
		cfg = cfg.WithCoreFeatures(CoreFeaturesV3)
	}
	return cfg
}

func reportCore3StageShape(b *testing.B, data []byte, m *wasm.Module) {
	bodyBytes := 0
	flatTypes := 0
	for i := range m.Code {
		bodyBytes += len(m.Code[i].BodyBytes)
	}
	for i := range m.Types {
		flatTypes += len(m.Types[i].SubTypes)
	}
	b.ReportMetric(float64(len(data)), "wasm-B")
	b.ReportMetric(float64(bodyBytes), "body-B")
	b.ReportMetric(float64(len(m.Code)), "funcs")
	b.ReportMetric(float64(flatTypes), "types")
}

func decodeValidateCore3Stage(b *testing.B, fixture core3StageFixture, data []byte) (*wasm.Module, *RuntimeConfig, frontend.Features) {
	b.Helper()
	cfg := core3StageConfig(fixture)
	features := cfg.frontendFeatures()
	m, err := wasm.DecodeModule(data)
	if err != nil {
		b.Fatal(err)
	}
	validationFeatures := wasm.ValidationFeatures{
		CompactImports: features.MultiMemory,
		MultiMemory:    features.MultiMemory,
		GCConstExpr:    features.GCStructProducts || features.GCArrayProducts || features.GCI31Products,
	}
	if err := wasm.ValidateModuleWithFeaturesAndWorkers(m, validationFeatures, 1); err != nil {
		b.Fatal(err)
	}
	return m, cfg, features
}

var (
	core3StageFeatureSink   CoreFeatures
	core3StageElemStateSink int
	core3StageDataStateSink int
	core3StageIntSink       int
	core3StageFactsSink     *frontend.ModuleFacts
	core3StageTypesSink     []DefinedTypeDescriptor
	core3StageCompiledSink  *Compiled
)

// BenchmarkCore3FrontendStages attributes the repeated frontend/type/codegen
// passes without adding tracing branches to production. Run cold stages with
// -benchtime=1x -count=N; prerequisite decode/validation is timer-paused.
func BenchmarkCore3FrontendStages(b *testing.B) {
	for _, fixture := range core3StageFixtures() {
		fixture := fixture
		b.Run(fixture.name, func(b *testing.B) {
			data := loadCore3StageFixture(b, fixture)
			b.Run("required-features", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					b.StopTimer()
					m, err := wasm.DecodeModule(data)
					if err != nil {
						b.Fatal(err)
					}
					reportCore3StageShape(b, data, m)
					b.StartTimer()
					core3StageFeatureSink = moduleRequiredFeatures(m)
				}
			})
			b.Run("segment-state-counts", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					b.StopTimer()
					m, err := wasm.DecodeModule(data)
					if err != nil {
						b.Fatal(err)
					}
					reportCore3StageShape(b, data, m)
					b.StartTimer()
					core3StageElemStateSink, core3StageDataStateSink = moduleSegmentStateCounts(m)
				}
			})
			b.Run("gc-descriptors", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					b.StopTimer()
					m, _, _ := decodeValidateCore3Stage(b, fixture, data)
					reportCore3StageShape(b, data, m)
					b.StartTimer()
					descs, err := frontend.BuildGCTypeDescs(m)
					if err != nil {
						b.Fatal(err)
					}
					core3StageIntSink = len(descs)
				}
			})
			b.Run("public-type-descriptors", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					b.StopTimer()
					m, _, _ := decodeValidateCore3Stage(b, fixture, data)
					reportCore3StageShape(b, data, m)
					b.StartTimer()
					types, err := typeDescriptorsFromWasm(m)
					if err != nil {
						b.Fatal(err)
					}
					core3StageTypesSink = types
				}
			})
			b.Run("structural-type-keys", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					b.StopTimer()
					m, _, _ := decodeValidateCore3Stage(b, fixture, data)
					reportCore3StageShape(b, data, m)
					b.StartTimer()
					flat := uint32(0)
					var keyMix uint64
					for gi := range m.Types {
						for si := range m.Types[gi].SubTypes {
							if m.Types[gi].SubTypes[si].Comp.Kind == wasm.CompFunc {
								key, ok := m.StructuralTypeKeyChecked(flat)
								if !ok {
									b.Fatalf("structural key %d failed", flat)
								}
								keyMix ^= key
							}
							flat++
						}
					}
					core3StageIntSink = int(keyMix)
				}
			})
			b.Run("module-facts", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					b.StopTimer()
					m, _, _ := decodeValidateCore3Stage(b, fixture, data)
					reportCore3StageShape(b, data, m)
					b.StartTimer()
					facts, err := frontend.AnalyzeModuleFacts(m)
					if err != nil {
						b.Fatal(err)
					}
					core3StageFactsSink = facts
				}
			})
			b.Run("frontend-admission", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					b.StopTimer()
					m, _, features := decodeValidateCore3Stage(b, fixture, data)
					facts, err := frontend.AnalyzeModuleFacts(m)
					if err != nil {
						b.Fatal(err)
					}
					reportCore3StageShape(b, data, m)
					b.StartTimer()
					if err := frontend.RejectUnsupportedWithFeaturesAndFacts(m, features, facts); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("native-lowering", func(b *testing.B) {
				b.ReportAllocs()
				codeBytes := 0
				for i := 0; i < b.N; i++ {
					b.StopTimer()
					m, cfg, _ := decodeValidateCore3Stage(b, fixture, data)
					arrayProduct, _ := stagedGCArrayOpcodeProduct(m)
					pressureAt, pressure := compileMemoryPressure(len(data))
					reportCore3StageShape(b, data, m)
					b.StartTimer()
					cm, err := railshotCompileModuleWith(m, railshotCompileOptions{
						Workers:           1,
						ElideBoundsChecks: cfg.boundsChecks == BoundsChecksSignalsBased,
						NoBoundsFacts:     cfg.noDeferBounds,
						SyncHostCalls:     true,
						GCStructHelpers:   moduleUsesGenericGCStructHelpers(m),
						GCArrayHelpers:    arrayProduct.requiresHelpers(),
						Interruptible:     true,
						MemoryPressureAt:  pressureAt,
						MemoryPressure:    pressure,
					})
					if err != nil {
						b.Fatal(err)
					}
					codeBytes = len(cm.Code)
					core3StageIntSink = codeBytes
				}
				b.ReportMetric(float64(codeBytes), "code-B")
			})
			b.Run("full-compile", func(b *testing.B) {
				cfg := core3StageConfig(fixture)
				b.ReportAllocs()
				b.SetBytes(int64(len(data)))
				for i := 0; i < b.N; i++ {
					compiled, err := Compile(cfg, data)
					if err != nil {
						b.Fatal(err)
					}
					core3StageCompiledSink = compiled
					if err := compiled.Close(); err != nil {
						b.Fatal(err)
					}
				}
			})
			if fixture.linkCold {
				b.Run("bind-cold", func(b *testing.B) {
					cfg := core3StageConfig(fixture)
					b.ReportAllocs()
					for i := 0; i < b.N; i++ {
						b.StopTimer()
						compiled, err := Compile(cfg, data)
						if err != nil {
							b.Fatal(err)
						}
						imports := make(Imports, len(compiled.Imports))
						for _, key := range compiled.Imports {
							imports[key] = HostFunc(func(HostModule, []uint64, []uint64) {})
						}
						b.StartTimer()
						instance, err := Instantiate(compiled, InstantiateOptions{Imports: imports})
						if err == nil {
							err = instance.Close()
						}
						b.StopTimer()
						_ = compiled.Close()
						if err != nil {
							b.Fatal(err)
						}
						b.StartTimer()
					}
				})
			}
			if fixture.instantiate {
				b.Run("instantiate-start", func(b *testing.B) {
					cfg := core3StageConfig(fixture)
					compiled, err := Compile(cfg, data)
					if err != nil {
						b.Fatal(err)
					}
					defer compiled.Close()
					imports := make(Imports, len(compiled.Imports))
					for _, key := range compiled.Imports {
						imports[key] = HostFunc(func(HostModule, []uint64, []uint64) {})
					}
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						instance, err := Instantiate(compiled, InstantiateOptions{Imports: imports})
						if err != nil {
							b.Fatal(err)
						}
						if err := instance.Close(); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
			if fixture.execExport != "" {
				b.Run("warmed-exec", func(b *testing.B) {
					compiled, err := Compile(core3StageConfig(fixture), data)
					if err != nil {
						b.Fatal(err)
					}
					defer compiled.Close()
					imports := make(Imports, len(compiled.Imports))
					for _, key := range compiled.Imports {
						imports[key] = HostFunc(func(HostModule, []uint64, []uint64) {})
					}
					instance, err := Instantiate(compiled, InstantiateOptions{Imports: imports})
					if err != nil {
						b.Fatal(err)
					}
					defer instance.Close()
					if fixture.initExport != "" {
						if _, err := instance.Invoke(fixture.initExport); err != nil {
							b.Fatal(err)
						}
					}
					fn, err := instance.PrepareFunction(fixture.execExport)
					if err != nil {
						b.Fatal(err)
					}
					if _, err := fn.Invoke(fixture.execArgs...); err != nil {
						b.Fatal(err)
					}
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						result, err := fn.Invoke(fixture.execArgs...)
						if err != nil {
							b.Fatal(err)
						}
						benchResultSink = result
					}
				})
			}
		})
	}
}
