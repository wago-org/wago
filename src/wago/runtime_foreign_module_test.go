package wago

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/wago-org/wago/tests/wasmtest"
)

type namedNoopExtension string

func (e namedNoopExtension) Info() ExtensionInfo    { return ExtensionInfo{ID: string(e)} }
func (namedNoopExtension) Register(*Registry) error { return nil }

func TestRuntimeRejectsForeignModuleBeforePolicyHooksAndClosedState(t *testing.T) {
	rtA := NewRuntime()
	if err := rtA.Use(tripleExt{}); err != nil {
		t.Fatal(err)
	}
	modA := callsEnvF(t, rtA)
	compiled := modA.Compiled()
	defer compiled.Close()
	if err := rtA.Close(); err != nil {
		t.Fatal(err)
	}

	rtB := NewRuntime()
	if err := rtB.Use(tripleExt{}); err != nil {
		t.Fatal(err)
	}
	var beforeInstantiate int
	rtB.hooks.BeforeInstantiate(func(*InstantiateContext) error {
		beforeInstantiate++
		return nil
	})
	denySourceCapability := Policy{DeniedCapabilities: []Capability{CapMetricsWrite}}
	if _, err := rtB.Instantiate(context.Background(), modA, WithPolicy(denySourceCapability)); !errors.Is(err, ErrForeignModule) {
		t.Fatalf("foreign Instantiate error = %v, want ErrForeignModule before capability policy", err)
	}
	if beforeInstantiate != 0 {
		t.Fatalf("foreign module reached %d instantiate hooks", beforeInstantiate)
	}
	if err := rtB.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := rtB.Instantiate(context.Background(), modA); !errors.Is(err, ErrForeignModule) {
		t.Fatalf("foreign Instantiate on closed destination = %v, want ErrForeignModule", err)
	}

	rtC := NewRuntime()
	defer rtC.Close()
	modC, err := rtC.Module(compiled)
	if err != nil {
		t.Fatalf("explicit rebind: %v", err)
	}
	instance, err := rtC.Instantiate(context.Background(), modC, WithImports(Imports{"env.f": HostFunc(func(_ HostModule, p, r []uint64) { r[0] = p[0] })}))
	if err != nil {
		t.Fatalf("Instantiate rebound module: %v", err)
	}
	if err := instance.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeRejectsForeignModuleDuringConcurrentUse(t *testing.T) {
	source := NewRuntime()
	mod, err := source.Compile(wasmtest.Module())
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Compiled().Close()
	destination := NewRuntime()
	defer destination.Close()

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			<-start
			if err := destination.Use(namedNoopExtension(fmt.Sprintf("test.concurrent.%d", i))); err != nil {
				t.Errorf("Use %d: %v", i, err)
			}
		}(i)
		go func() {
			defer wg.Done()
			<-start
			if _, err := destination.Instantiate(context.Background(), mod); !errors.Is(err, ErrForeignModule) {
				t.Errorf("concurrent foreign Instantiate = %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
}
