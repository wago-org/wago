package wago

import (
	"fmt"
	"testing"
)

func BenchmarkCompileRegistryScaling(b *testing.B) {
	for _, count := range []int{0, 10, 1000, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			rt := NewRuntime()
			defer rt.Close()
			for i := 0; i < count; i++ {
				key := fmt.Sprintf("unused.f%d", i)
				fn := HostFunc(func(HostModule, []uint64, []uint64) {})
				rt.imports[key] = fn
				rt.importMeta[key] = &registeredImport{module: "unused", name: key, fn: fn, params: []ValType{ValI32}}
			}
			wasm := benchAddOneModule()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				mod, err := rt.Compile(wasm)
				if err != nil {
					b.Fatal(err)
				}
				if err := mod.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestRegistrySnapshotRemainsImmutable(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	oldMeta := &registeredImport{module: "env", name: "f", params: []ValType{ValI32}}
	rt.importMeta["env.f"] = oldMeta
	rt.imports["env.f"] = HostFunc(func(HostModule, []uint64, []uint64) {})
	rt.mu.Lock()
	old := rt.snapshotModuleBindingsLocked(rt.loadHooks())
	rt.writableImportsLocked()
	rt.importMeta["env.f"] = &registeredImport{module: "env", name: "f", params: []ValType{ValI64}}
	delete(rt.imports, "env.f")
	next := rt.snapshotModuleBindingsLocked(rt.loadHooks())
	rt.mu.Unlock()
	if old.importMeta["env.f"].params[0] != ValI32 || old.imports["env.f"] == nil {
		t.Fatal("published generation changed")
	}
	if next.importMeta["env.f"].params[0] != ValI64 || next.imports["env.f"] != nil {
		t.Fatal("new generation lost its paired update")
	}
	if n := testing.AllocsPerRun(100, func() {
		rt.mu.Lock()
		_ = rt.snapshotModuleBindingsLocked(rt.loadHooks())
		rt.mu.Unlock()
	}); n != 0 {
		t.Fatalf("warm snapshot allocates %g times", n)
	}
}

func TestPreparedCompileReleasesRegistryGeneration(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	for _, compile := range []bool{false, true} {
		p, err := rt.PrepareCompile(benchAddOneModule())
		if err != nil {
			t.Fatal(err)
		}
		if compile {
			mod, err := p.Compile()
			if err != nil {
				t.Fatal(err)
			}
			mod.Close()
		} else {
			p.Close()
		}
		if p.bindings.imports != nil || p.bindings.importMeta != nil {
			t.Fatal("consumed preparation retains old registry")
		}
	}
}
