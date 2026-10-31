//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

var rv64PublicGCTypeSubtypingDuplicateRecursiveLinkHex = []string{
	"0061736d0100000001a380808000024e025000600001647050010060000164004e02500060000164705001026000016402038380808000020001078d80808000020366313100000366313200010a918080800002838080800000000b838080800000000b",
	"0061736d0100000001a380808000024e025000600001647050010060000164004e0250006000016470500102600001640202a58080800004024d38036631310000024d38036631310002024d38036631320001024d38036631320003",
}

func rv64PublicGCTypeSubtypingDuplicateRecursiveLinkBytes(t testing.TB, variant int) []byte {
	t.Helper()
	data, err := hex.DecodeString(rv64PublicGCTypeSubtypingDuplicateRecursiveLinkHex[variant-1])
	wantLen := []int{100, 92}[variant-1]
	wantSHA := [][sha256.Size]byte{
		{0xce, 0xe5, 0x3b, 0x6e, 0x42, 0x09, 0x32, 0xfa, 0xec, 0x8b, 0xf1, 0x66, 0xa6, 0xae, 0x79, 0xcf, 0xab, 0x88, 0xf7, 0xca, 0x89, 0x0c, 0xd3, 0x98, 0x51, 0xd3, 0xf7, 0x0b, 0x93, 0x24, 0x71, 0xaa},
		{0x30, 0xff, 0x7e, 0x3b, 0xef, 0xab, 0x74, 0x05, 0xc6, 0x35, 0x54, 0x52, 0x6e, 0xcf, 0x2e, 0x61, 0xfc, 0x9e, 0xcf, 0x2b, 0x65, 0xe1, 0x75, 0x41, 0x4d, 0xb0, 0x7a, 0xa7, 0x74, 0xe6, 0xa5, 0x40},
	}[variant-1]
	if err != nil || len(data) != wantLen || sha256.Sum256(data) != wantSHA {
		t.Fatal("duplicate-recursive-link source identity changed", variant, err)
	}
	return data
}

func rv64PublicGCTypeSubtypingDuplicateRecursiveLinkCompile(t *testing.T, variant int, artifact bool) *Compiled {
	t.Helper()

	original, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingDuplicateRecursiveLinkBytes(t, variant))
	if err != nil {
		t.Fatal(err)
	}

	return original

}

func TestRV64PublicGCTypeSubtypingDuplicateRecursiveLinkExecutionAndArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			providerCompiled := rv64PublicGCTypeSubtypingDuplicateRecursiveLinkCompile(t, 1, artifact)
			defer providerCompiled.Close()
			consumerCompiled := rv64PublicGCTypeSubtypingDuplicateRecursiveLinkCompile(t, 2, artifact)
			defer consumerCompiled.Close()
			for _, compiled := range []*Compiled{providerCompiled, consumerCompiled} {
				view := compiled.executionView()
				if !rv64SourceContractValid(view) || view.validate() != nil {
					t.Fatal("invalid public subtype link execution contract")
				}
			}
			rt := NewRuntime()
			defer rt.Close()
			providerModule, err := rt.Module(providerCompiled)
			if err != nil {
				t.Fatal(err)
			}
			provider, err := rt.Instantiate(context.Background(), providerModule, WithImports(NewImports()))
			providerModule.Close()
			if err != nil {
				t.Fatal(err)
			}
			rv64RequireSubtypeStorage(t, provider)
			exports := make([]*InstanceExport, 2)
			for i, name := range []string{"f11", "f12"} {
				if got, invokeErr := provider.Invoke(name); invokeErr == nil || len(got) != 0 {
					t.Fatalf("%s = %v, %v; want unreachable trap", name, got, invokeErr)
				}
				exports[i], err = provider.ExportedFunc(name)
				if err != nil {
					t.Fatal(err)
				}
			}

			consumerModule, err := rt.Module(consumerCompiled)
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M8.f11", exports[0], "M8.f12", exports[1])))
			consumerModule.Close()
			if err != nil {
				t.Fatal(err)
			}
			rv64RequireSubtypeStorage(t, consumer)
			rv64RequireSubtypeNativeImports(t, consumer, provider, 0, 0, 1, 1)
			provider.lifeMu.Lock()
			refs, closed := provider.resourceRefs, provider.resourcesClosed
			provider.lifeMu.Unlock()
			if refs != 1 || closed {
				t.Fatalf("duplicate imports refs/closed=%d/%v want 1/false", refs, closed)
			}
			if err = provider.Close(); err != nil {
				t.Fatal(err)
			}
			if err = consumer.Close(); err != nil {
				t.Fatal(err)
			}
			provider.lifeMu.Lock()
			refs, closed = provider.resourceRefs, provider.resourcesClosed
			provider.lifeMu.Unlock()
			if refs != 0 || !closed {
				t.Fatalf("released provider refs/closed=%d/%v", refs, closed)
			}
		})
	}
}

func TestRV64PublicGCTypeSubtypingDuplicateRecursiveLinkMismatchAndPublicRuntime(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_DUPLICATE_RECURSIVE_LINK_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	providerCompiled := rv64PublicGCTypeSubtypingDuplicateRecursiveLinkCompile(t, 1, false)
	consumerCompiled := rv64PublicGCTypeSubtypingDuplicateRecursiveLinkCompile(t, 2, false)
	defer providerCompiled.Close()
	defer consumerCompiled.Close()
	plain := NewRuntime()
	mod, err := plain.Module(providerCompiled)
	if err == nil {
		_, err = plain.Instantiate(context.Background(), mod, WithImports(NewImports()))
		mod.Close()
	}
	plain.Close()
	if err != nil {
		t.Fatal("public subtype provider instantiation", err)
	}
	rt := NewRuntime()
	defer rt.Close()
	providerModule, err := rt.Module(providerCompiled)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := rt.Instantiate(context.Background(), providerModule, WithImports(NewImports()))
	providerModule.Close()
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	valid, err := provider.ExportedFunc("f12")
	if err != nil {
		t.Fatal(err)
	}
	invalid := &InstanceExport{inst: provider, localIdx: len(provider.c.Entry)}
	consumerModule, err := rt.Module(consumerCompiled)
	if err != nil {
		t.Fatal(err)
	}
	bad, invalidErr := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M8.f11", invalid, "M8.f12", valid)))
	consumerModule.Close()
	if bad != nil || invalidErr == nil || !strings.Contains(invalidErr.Error(), "no retained framed entry") {
		t.Fatal("invalid duplicate-recursive export admitted", invalidErr)
	}
	provider.lifeMu.Lock()
	refs, closed := provider.resourceRefs, provider.resourcesClosed
	provider.lifeMu.Unlock()
	if refs != 0 || closed {
		t.Fatalf("invalid link retained provider refs/closed=%d/%v", refs, closed)
	}
	host := slotHostFunc(func(HostModule, []uint64, []uint64) {})
	consumerModule, err = rt.Module(consumerCompiled)
	if err != nil {
		t.Fatal(err)
	}
	bad, hostErr := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M8.f11", host, "M8.f12", host)))
	consumerModule.Close()
	if bad == nil || hostErr != nil {
		t.Fatal("public subtype host import", hostErr)
	}
	if err := bad.Close(); err != nil {
		t.Fatal(err)
	}
	oldCompiled := rv64PublicGCTypeSubtypingExtendedProjectionLinkCompile(t, 1, false)
	defer oldCompiled.Close()
	oldModule, err := rt.Module(oldCompiled)
	if err != nil {
		t.Fatal(err)
	}
	oldProvider, err := rt.Instantiate(context.Background(), oldModule, WithImports(NewImports()))
	oldModule.Close()
	if err != nil {
		t.Fatal(err)
	}
	defer oldProvider.Close()
	oldExport, err := oldProvider.ExportedFunc("h")
	if err != nil {
		t.Fatal(err)
	}
	consumerModule, err = rt.Module(consumerCompiled)
	if err != nil {
		t.Fatal(err)
	}
	bad, crossErr := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M8.f11", oldExport, "M8.f12", oldExport)))
	consumerModule.Close()
	if bad != nil || crossErr == nil || !strings.Contains(crossErr.Error(), "signature mismatch") {
		t.Fatal("cross-product duplicate-recursive import admitted", crossErr)
	}
}
