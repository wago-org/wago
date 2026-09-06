package wago

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/tests/wasmtest"
)

const artifactManyFunctionCount = 65536

func artifactManyFunctions(t testing.TB) []byte {
	t.Helper()
	types := append(wasmtest.ULEB(artifactManyFunctionCount), bytes.Repeat([]byte{0}, artifactManyFunctionCount)...)
	bodies := append(wasmtest.ULEB(artifactManyFunctionCount), bytes.Repeat(wasmtest.Code([]byte{0x0b}), artifactManyFunctionCount)...)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, types),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("last", 0, artifactManyFunctionCount-1))),
		wasmtest.Section(10, bodies),
	)
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	artifact, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

func TestArtifactManyLocalFunctionsRoundTrip(t *testing.T) {
	artifact := artifactManyFunctions(t)
	for _, mode := range []string{"ReadFrom", "UnmarshalBinary"} {
		t.Run(mode, func(t *testing.T) {
			var loaded Compiled
			defer loaded.Close()
			var err error
			if mode == "ReadFrom" {
				_, err = loaded.ReadFrom(bytes.NewReader(artifact))
			} else {
				err = loaded.UnmarshalBinary(artifact)
			}
			if err != nil {
				t.Fatalf("load %d-byte artifact with default limits: %v", len(artifact), err)
			}
			if len(loaded.Funcs) != artifactManyFunctionCount {
				t.Fatalf("decoded %d functions", len(loaded.Funcs))
			}
			instance, err := Instantiate(&loaded)
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			if result, err := instance.Invoke("last"); err != nil || len(result) != 0 {
				t.Fatalf("last function = %v, %v", result, err)
			}
		})
	}
}

func BenchmarkArtifactManyLocalFunctionsDecode(b *testing.B) {
	artifact := artifactManyFunctions(b)
	b.ReportAllocs()
	b.SetBytes(int64(len(artifact)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var loaded Compiled
		if _, err := loaded.ReadFrom(bytes.NewReader(artifact)); err != nil {
			b.Fatal(err)
		}
		if err := loaded.Close(); err != nil {
			b.Fatal(err)
		}
	}
}
