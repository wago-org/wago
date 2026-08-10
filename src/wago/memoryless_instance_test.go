package wago

import (
	"context"
	"testing"
)

func TestMemorylessInstanceDoesNotExposeRuntimeScratchMemory(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	in, err := rt.Instantiate(context.Background(), mustCompileWat(rt, t, `(module (func (export "f")))`))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got := in.Memory(); got != nil {
		t.Fatalf("Memory() = %p, want nil for a memoryless module", got)
	}
}
