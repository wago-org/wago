//go:build windows && amd64 && wago_guardpage && !tinygo

package wago

import (
	"context"
	"testing"
)

func TestWindowsTypedI64GuardPageUsesParkedFallback(t *testing.T) {
	c, err := Compile(goHostSegmentConfig().WithBoundsChecks(BoundsChecksSignalsBased), cancelBoundedTypedI64Single)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	imports := NewImports()
	imports.HostFunc("env", "step", func(v int64) int64 { return v + 1 }).Params(ValI64).Results(ValI64)
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if in.eng.PreparedScalarHost() != nil {
		t.Fatal("signals-based bounds checks admitted live Windows owner")
	}
	if got, err := in.InvokeContext(context.Background(), "run", 41); err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("parked fallback: %v, %v", got, err)
	}
}
