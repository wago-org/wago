//go:build (linux || darwin || windows) && (amd64 || arm64)

package wago

import "testing"

func TestScalarSyncHostCallUsesOneScopedHandleAllocation(t *testing.T) {
	compiled := MustCompile(benchReturningImportModule())
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{
		"env.f": HostFunc(func(_ HostModule, params, results []uint64) {
			results[0] = params[0] + 1
		}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("g", I32(1)); err != nil || len(got) != 1 || got[0] != 2 {
		t.Fatalf("warm host call = %v, %v", got, err)
	}
	allocs := testing.AllocsPerRun(100, func() {
		if _, err := instance.Invoke("g", I32(1)); err != nil {
			panic(err)
		}
	})
	if allocs > 1 {
		t.Fatalf("scalar synchronous host-call allocations = %.1f, want at most 1", allocs)
	}
}
