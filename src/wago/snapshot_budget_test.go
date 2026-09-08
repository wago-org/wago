package wago

import (
	"errors"
	"testing"
)

func TestSnapshotBudgetCountsAliasedDestinationCopies(t *testing.T) {
	shared := make([]byte, 1024)
	c := &Compiled{Data: make([]DataInit, 64)}
	for i := range c.Data {
		c.Data[i].Bytes = shared
	}
	if _, err := c.freezeExecution(50000); err == nil || !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("snapshot budget: %v", err)
	}
	if c.loadValidateMemo().executionView() != nil {
		t.Fatal("over-budget snapshot was cloned")
	}
	if _, err := c.freezeExecution(256000); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.executionView().Data[0].Bytes == nil {
		t.Fatal("valid retry lost data")
	}
	shared[0] = 42
	if c.executionView().Data[0].Bytes[0] != 0 {
		t.Fatal("snapshot retained public mutable bytes")
	}
}

func TestSnapshotBudgetChecksWarmCallerPolicy(t *testing.T) {
	c := MustCompile(benchAddOneModule())
	defer c.Close()
	size := c.loadValidateMemo().snapshotBytes
	if size == 0 {
		t.Fatal("snapshot size not recorded")
	}
	if _, err := Instantiate(c, InstantiateOptions{MaxCompiledMetadataBytes: size - 1}); err == nil || !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("warm policy: %v", err)
	}
	in, err := Instantiate(c, InstantiateOptions{MaxCompiledMetadataBytes: size})
	if err != nil {
		t.Fatal(err)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithMaxCompiledMetadataBytes(size - 1)))
	defer rt.Close()
	if _, err := rt.Module(c); err == nil || !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("runtime policy: %v", err)
	}
}

func TestSnapshotPreflightNoAllocation(t *testing.T) {
	c := MustCompile(benchAddOneModule())
	defer c.Close()
	if n := testing.AllocsPerRun(100, func() {
		if _, err := snapshotMetadataBytes(c, 0); err != nil {
			t.Fatal(err)
		}
	}); n != 0 {
		t.Fatalf("preflight allocates %g times", n)
	}
}

func BenchmarkCompiledSnapshotPreflight(b *testing.B) {
	c := benchMustCompile(b, benchAddOneModule())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := snapshotMetadataBytes(c, 0); err != nil {
			b.Fatal(err)
		}
	}
}
