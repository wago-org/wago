package wasm

import (
	"os"
	"path/filepath"
	"testing"
)

type validationStageFixture struct {
	name string
	path string
	env  string
}

func validationStageFixtures() []validationStageFixture {
	root := filepath.Clean("../../../..")
	corpus := filepath.Join(root, "bench", "corpus")
	return []validationStageFixture{
		{name: "tiny", path: filepath.Join(corpus, "tiny.wasm")},
		{name: "json-as", path: filepath.Join(corpus, "json-as.wasm")},
		{name: "wasm3", path: filepath.Join(corpus, "wasm3.wasm")},
		{name: "sqlite3", path: filepath.Join(corpus, "sqlite3.wasm")},
		{name: "ruby", path: filepath.Join(corpus, "ruby.wasm")},
		{name: "esbuild", path: filepath.Join(corpus, "esbuild.wasm")},
		{name: "moonbit-json", env: "WAGO_MOONBIT_JSON_SMOKE_WASM"},
		{name: "starshine", env: "WAGO_STARSHINE_SMOKE_WASM"},
	}
}

func loadValidationStageFixture(tb testing.TB, fixture validationStageFixture) []byte {
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

func reportValidationStageShape(b *testing.B, m *Module) {
	bodyBytes := 0
	for i := range m.Code {
		bodyBytes += len(m.Code[i].BodyBytes)
	}
	b.ReportMetric(float64(bodyBytes), "body-B")
	b.ReportMetric(float64(len(m.Code)), "funcs")
	b.ReportMetric(float64(m.flattenedTypeCount()), "types")
}

// BenchmarkCore3ValidationStages splits the real byte-backed validator at its
// declaration/function boundary. Run with -benchtime=1x -count=N for cold-stage
// samples: setup decode and the prerequisite declaration pass are timer-paused.
func BenchmarkCore3ValidationStages(b *testing.B) {
	for _, fixture := range validationStageFixtures() {
		fixture := fixture
		b.Run(fixture.name, func(b *testing.B) {
			data := loadValidationStageFixture(b, fixture)
			b.Run("module-declarations", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					b.StopTimer()
					m, err := DecodeModule(data)
					if err != nil {
						b.Fatal(err)
					}
					reportValidationStageShape(b, m)
					v := &moduleValidator{m: m, funcIndex: -1}
					b.StartTimer()
					if err := v.validateModule(); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("function-bodies", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					b.StopTimer()
					m, err := DecodeModule(data)
					if err != nil {
						b.Fatal(err)
					}
					reportValidationStageShape(b, m)
					v := &moduleValidator{m: m, funcIndex: -1}
					if err := v.validateModule(); err != nil {
						b.Fatal(err)
					}
					b.StartTimer()
					if err := v.validateFunctions(1); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}
