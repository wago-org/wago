//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"testing"
)

func TestRV64Core3RefIsNullExecution(t *testing.T) {
	for _, artifact := range []bool{false} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64Core3RefIsNullExecutionCompile(t, artifact)
			t.Cleanup(func() { _ = compiled.Close() })
			view := compiled.executionView()
			if !rv64SourceContractValid(view) || railshotValidateCompiled(view) != nil {
				t.Fatal("source compiler lost the general ref_is_null contract", railshotValidateCompiled(view))
			}
			cache := compiled.loadCodeCache()
			if cache == nil || !artifact && (cache.base != 0 || len(cache.mem) != 0 || cache.sealed || cache.flags&compiledCacheWritableCode != 0) || artifact && (cache.base == 0 || len(cache.mem) < len(view.code) || cache.sealed || cache.flags&compiledCacheWritableCode == 0 || cache.refs != 0) {
				compiled.Close()
				t.Fatalf("ref_is_null unpublished mapping state: artifact=%t base=%#x mem=%d sealed=%t writable=%t refs=%d", artifact, cache.base, len(cache.mem), cache.sealed, cache.flags&compiledCacheWritableCode != 0, cache.refs)
			}

			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			t.Cleanup(func() { _ = rt.Close() })
			in := rv64NativeTokenInstantiate(t, rt, compiled, NewImports())
			if in.gc == nil || in.refStore != rt.refStore || rt.refStore.gcDomainForCollector(in.gc) == nil || cache.base == 0 || len(cache.mem) < len(view.code) || !cache.sealed || (cache.flags&compiledCacheWritableCode != 0) != artifact || cache.refs == 0 {
				in.Close()
				compiled.Close()
				rt.Close()
				t.Fatalf("ref_is_null publication state: gc=%p base=%#x mem=%d code=%d sealed=%t writable=%t refs=%d", in.gc, cache.base, len(cache.mem), len(view.code), cache.sealed, cache.flags&compiledCacheWritableCode != 0, cache.refs)
			}

			invokeI32 := func(export string, args ...uint64) int32 {
				t.Helper()
				got, err := in.Invoke(export, args...)
				if err != nil || len(got) != 1 {
					t.Fatalf("%s%v = %v, %v", export, args, got, err)
				}
				return AsI32(got[0])
			}

			if got := invokeI32("funcref", 0); got != 1 {
				t.Fatal("ref.is_null null funcref =", got)
			}
			if got := invokeI32("externref", 0); got != 1 {
				t.Fatal("ref.is_null null externref =", got)
			}
			if got := invokeI32("ref-null"); got != 1 {
				t.Fatal("ref.null indexed =", got)
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, in)
			for export, want := range map[string][2]int32{
				"funcref-elem":   {1, 0},
				"externref-elem": {1, 1},
				"ref-elem":       {1, 0},
			} {
				for index := range 2 {
					if got := invokeI32(export, uint64(index)); got != want[index] {
						t.Fatalf("%s(%d) = %d, want %d", export, index, got, want[index])
					}
				}
			}

			ref, err := rt.NewExternRef("core3")
			if err != nil {
				t.Fatal(err)
			}
			token := ValueExternRef(ref).Bits()
			if got, err := in.Invoke("externref", token^0x9e3779b97f4a7c15); err == nil || got != nil {
				t.Fatal("forged externref token was admitted", got, err)
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, in)
			if got := invokeI32("externref", token); got != 0 {
				t.Fatal("ref.is_null non-null externref =", got)
			}
			if got, err := in.Invoke("externref-elem", 2); err == nil || got != nil {
				t.Fatal("out-of-bounds compact table read succeeded", got, err)
			}
			if got := invokeI32("externref-elem", 1); got != 1 {
				t.Fatal("table state changed after bounds trap", got)
			}
			if got, err := in.Invoke("init", token); err != nil || len(got) != 0 {
				t.Fatal("init externref", got, err)
			}
			if got := invokeI32("externref-elem", 1); got != 0 {
				t.Fatal("stored externref ref.is_null =", got)
			}
			if value, ok := rt.ExternRefValue(ref); !ok || value != "core3" {
				t.Fatal("stored externref token changed", value, ok)
			}
			if got, err := in.Invoke("deinit"); err != nil || len(got) != 0 {
				t.Fatal("deinit references", got, err)
			}
			if err := in.CollectGC(); err != nil {
				t.Fatal("collect deinitialized references", err)
			}
			for _, export := range []string{"funcref-elem", "externref-elem", "ref-elem"} {
				if got := invokeI32(export, 1); got != 1 {
					t.Fatalf("%s(1) after deinit = %d", export, got)
				}
			}
			if !rt.ReleaseExternRef(ref) {
				t.Fatal("deinitialized externref did not release")
			}
			if got, err := in.Invoke("externref", token); err == nil || got != nil {
				t.Fatal("released externref token was admitted", got, err)
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, in)
			if got, err := in.InvokeValues(context.Background(), "externref", ValueExternRef(NullExternRef())); err != nil || len(got) != 1 || got[0].I32() != 1 {
				t.Fatal("typed null externref", got, err)
			}
			var invokeErr error
			if allocs := testing.AllocsPerRun(1000, func() { _, invokeErr = in.Invoke("ref-null") }); invokeErr != nil || allocs != 0 {
				t.Fatalf("ref_is_null steady state = %v, allocs=%v", invokeErr, allocs)
			}
			if err := compiled.Close(); err != nil {
				t.Fatal(err)
			}
			if cache.base == 0 || !cache.sealed || cache.refs == 0 {
				t.Fatal("live ref_is_null instance lost its executable lease")
			}
			if err := in.Close(); err != nil {
				t.Fatal(err)
			}
			if err := rt.Close(); err != nil {
				t.Fatal(err)
			}
			if cache.base != 0 || len(cache.mem) != 0 || cache.refs != 0 || !cache.closed {
				t.Fatalf("ref_is_null executable mapping survived final close: base=%#x mem=%d refs=%d closed=%t", cache.base, len(cache.mem), cache.refs, cache.closed)
			}
		})
	}
}
