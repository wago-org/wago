//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"sync/atomic"
	"testing"
)

func TestPreparedScalarHostReentryBuffers(t *testing.T) {
	c, err := Compile(goHostSegmentConfig(), hostYieldLoopModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var counter atomic.Uint64
	imports := NewImports()
	imports.HostFunc("env", "step", func(v int32) int32 { counter.Add(1); return v + 1 }).Params(ValI32).Results(ValI32)
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	outer := in.eng
	prepared := outer.PreparedScalarHost()
	if prepared == nil {
		t.Skip("prepared scalar bridge unavailable")
	}
	func() {
		restore, err := in.prepareHostReentryState()
		if err != nil {
			t.Fatal(err)
		}
		defer restore()
		if in.eng == outer || in.eng.PreparedScalarHost() != nil {
			t.Fatal("reentry retained outer engine bridge")
		}
		got, err := in.Invoke("run", 3, 1)
		if err != nil || len(got) != 1 || got[0] != 3 {
			t.Fatalf("nested call: %v, %v", got, err)
		}
	}()
	if in.eng != outer || outer.PreparedScalarHost() != prepared {
		t.Fatal("outer engine bridge was not restored")
	}
	got, err := in.Invoke("run", 5, 1)
	if err != nil || len(got) != 1 || got[0] != 5 || counter.Load() != 8 {
		t.Fatalf("restored call: %v, %v, callbacks=%d", got, err, counter.Load())
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if outer.PreparedScalarHost() != nil {
		t.Fatal("closed instance retained stale arena views")
	}
}
