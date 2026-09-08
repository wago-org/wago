package wago

import (
	"context"
	"fmt"
	"testing"
)

func BenchmarkCompileRegistryScaling(b *testing.B) {
	for _, count := range []int{0, 10, 1000, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			rt := benchmarkRegisteredRuntime(b, count)
			defer rt.Close()
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

func benchmarkRegistrySet(b *testing.B, count int) PluginSet {
	definition := testDefinition("example.com/bench/registry")
	definition.Authorities = []AuthorityRequest{{Name: AuthorityHostImportDefine, Mode: AuthorityRequired, Reason: "benchmark registered imports", Scope: AuthorityScope{Modules: []string{"unused"}}}}
	return testSet(b, PluginProvider{Definition: definition, New: func() Plugin {
		return pluginFunc(func(reg *Registrar) error {
			imports, err := reg.HostImports()
			if err != nil {
				return err
			}
			module, err := imports.Module("unused")
			if err != nil {
				return err
			}
			fn := HostFunc(func(HostModule, []uint64, []uint64) {})
			for i := 0; i < count; i++ {
				module.Func(fmt.Sprintf("f%d", i), fn).Params(ValI32)
			}
			return nil
		})
	}})
}
func benchmarkRegisteredRuntime(b *testing.B, count int) *Runtime {
	b.Helper()
	rt := NewRuntime()
	if err := rt.LoadPlugins(context.Background(), benchmarkRegistrySet(b, count)); err != nil {
		rt.Close()
		b.Fatal(err)
	}
	return rt
}
func BenchmarkRegisterImports(b *testing.B) {
	for _, count := range []int{0, 10, 1000, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			set := benchmarkRegistrySet(b, count)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rt := NewRuntime()
				if err := rt.LoadPlugins(context.Background(), set); err != nil {
					b.Fatal(err)
				}
				if err := rt.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
