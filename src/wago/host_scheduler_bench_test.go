//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"testing"
)

// This measures the existing whole-entry scheduler proof on the same prepared
// export/API. It is a bound on a possible source of savings, not a measurement
// of host-call overhead or evidence that host-return continuations are bounded.
func BenchmarkHostSchedulerPotential(b *testing.B) {
	for _, bounded := range []bool{false, true} {
		b.Run(fmt.Sprintf("bounded%t", bounded), func(b *testing.B) {
			c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithOptimization("prepared-bounded-entry", bounded), benchAddOneModule())
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			fn, err := in.PrepareFunction("f")
			if err != nil {
				b.Fatal(err)
			}
			if !fn.directIntFast || fn.directIntBounded != bounded {
				b.Fatal("scheduler control did not select requested path")
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				got, err := fn.Invoke1(41)
				if err != nil || len(got) != 1 || got[0] != 42 {
					b.Fatalf("invoke=%v, %v", got, err)
				}
			}
		})
	}
}

func TestHostImportsRemainOutsideBoundedSchedulerAdmission(t *testing.T) {
	for _, fixture := range []struct {
		name string
		data []byte
	}{
		{"single", benchReturningImportModule()},
		{"loop", hostRoundtripLoopModule(t, 0)},
		{"memory-loop", hostRoundtripLoopModule(t, 4)},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			c := MustCompile(fixture.data)
			defer c.Close()
			if c.directPreparedBoundedAt(0) {
				t.Fatal("host-capable function admitted without segment proof")
			}
		})
	}
}
