//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"testing"
)

func TestRV64Core3RefIsNullExecution(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "retained", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64Core3RefIsNullExecutionRetainedFixture(t)
			if artifact {
				blob, err := compiled.MarshalBinary()
				if err != nil {
					t.Fatal(err)
				}
				compiled, err = LoadTrustedArtifact(blob)
				if err != nil {
					t.Fatal(err)
				}
			}
			defer compiled.Close()

			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			in := rv64NativeTokenInstantiate(t, rt, compiled, NewImports())

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
		})
	}
}
