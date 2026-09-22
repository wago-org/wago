//go:build linux && amd64

package wago

import (
	"context"
	"testing"
)

func TestIdleMemoryPolicyImmutableAndInherited(t *testing.T) {
	base := NewRuntimeConfig()
	low := base.WithNativeStackBytes(1 << 20).WithIdleMemoryReclamation(true)
	if base.IdleMemoryReclamation() || !low.IdleMemoryReclamation() || low.NativeStackBytes() != 1<<20 {
		t.Fatal("immutable configuration changed")
	}
	c, err := Compile(low, boundedLargeFrameRecursionModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !c.idleMemoryReclamation() || c.nativeStackBytes() != 1<<20 {
		t.Fatal("compiled policy damaged stack capacity")
	}
	in, err := Instantiate(c, InstantiateOptions{forceSyncHost: true})
	if err != nil {
		t.Fatal(err)
	}
	if !in.eng.IdleMemoryReclamation() || in.eng.StackBytes() != 1<<20 {
		t.Fatal("direct instance lost policy")
	}
	outer := in.eng
	restore, err := in.prepareHostReentryState()
	if err != nil {
		t.Fatal(err)
	}
	if in.eng == outer || !in.eng.IdleMemoryReclamation() {
		t.Fatal("nested Engine lost policy")
	}
	restore()
	if in.eng != outer {
		t.Fatal("outer Engine not restored")
	}
	if _, err = in.Invoke("recurse", 4); err != nil {
		t.Fatal(err)
	}
	if err = in.Close(); err != nil {
		t.Fatal(err)
	}
	// The receiving Runtime owns the execution policy, even for a foreign Compiled.
	rt := NewRuntime(WithRuntimeConfig(base))
	defer rt.Close()
	mod, err := rt.Module(c)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	instance, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if instance.eng.IdleMemoryReclamation() || instance.eng.StackBytes() != DefaultNativeStackBytes {
		t.Fatal("Runtime did not override compile preference")
	}
}
