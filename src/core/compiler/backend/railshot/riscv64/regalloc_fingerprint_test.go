//go:build linux && !tinygo

package riscv64

import (
	"crypto/sha256"
	"testing"
)

func TestRegallocCheckEmissionFingerprint(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"shared", binaryModule(pressureFixture())},
		{"integer", binaryModule(numericFixture(false, 0x6e, true))},
		{"recursive", binaryModule(sumCallFixture(0))},
		{"memory", stateModule([]fixtureFunc{memoryFixture(0x28, 0), growFixture()}, false, 1, 2, true, nil, nil)},
	}
	for _, tc := range multivalueCases() {
		switch tc.name {
		case "indexed-if-params", "loop-parameter-result-counts", "256-arguments-and-results":
			cases = append(cases, struct {
				name string
				data []byte
			}{tc.name, binaryModule(tc.funcs...)})
		}
	}
	for _, tc := range cases {
		for _, production := range []bool{false, true} {
			for _, force := range []bool{false, true} {
				c, err := compileOptions(tc.data, scalarRegisters[:], CompileOptions{ForceFallback: force, Interruptible: production, production: production})
				if err != nil {
					t.Fatal(tc.name, err)
				}
				t.Logf("REGALLOC_CODE %s production=%v force=%v bytes=%d sha256=%x", tc.name, production, force, len(c.code), sha256.Sum256(c.code))
			}
		}
	}
}

func BenchmarkRegallocCompileBounded(b *testing.B) {
	data := binaryModule(pressureFixture(), sumCallFixture(1))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Compile(data); err != nil {
			b.Fatal(err)
		}
	}
}
