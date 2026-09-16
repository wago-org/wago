//go:build (linux || darwin || windows) && (amd64 || arm64)

package wago

import (
	"testing"
)

func TestInstantiateCloseAllocationBudget(t *testing.T) {
	if !requireStandardGoTestRuntime(t) {
		return
	}
	c := MustCompile(multiValueControlCallModule())
	defer c.Close()

	var lifecycleErr error
	allocs := testing.AllocsPerRun(1000, func() {
		in, err := Instantiate(c, InstantiateOptions{})
		if err != nil {
			lifecycleErr = err
			return
		}
		lifecycleErr = in.Close()
	})
	if lifecycleErr != nil {
		t.Fatalf("Instantiate/Close: %v", lifecycleErr)
	}
	if allocs > 6 {
		t.Fatalf("Instantiate/Close allocations = %.0f, want <= 6", allocs)
	}
}

func TestInstantiateHostImportAllocationBudget(t *testing.T) {
	if !requireStandardGoTestRuntime(t) {
		return
	}
	c := MustCompile(voidImportCallModule())
	defer c.Close()
	imports := testImports("env.f", slotHostFunc(func(HostModule, []uint64, []uint64) {}))
	warm, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	if err := warm.Close(); err != nil {
		t.Fatal(err)
	}

	var lifecycleErr error
	allocs := testing.AllocsPerRun(1000, func() {
		in, err := Instantiate(c, InstantiateOptions{Imports: imports})
		if err != nil {
			lifecycleErr = err
			return
		}
		lifecycleErr = in.Close()
	})
	if lifecycleErr != nil {
		t.Fatalf("Instantiate/Close: %v", lifecycleErr)
	}
	// The public Imports snapshot introduced on main adds four allocations on
	// Go 1.22 (five on newer Go releases). Keep a small portability margin while
	// still guarding the reduction from main's 32 allocations on Go 1.22.
	if allocs > 20 {
		t.Fatalf("Instantiate/Close host-import allocations = %.0f, want <= 20", allocs)
	}
}
