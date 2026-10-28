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

var rv64PublicGCTypeSubtypingExtendedProjectionLinkHex = []string{
	"0061736d0100000001c380808000044e02500060000050005f016400004e02500060000050005f016402004e025001026000005001035f056400006402006400006402006404004e025001046000005f00038280808000010607858080800001016800000a8880808000018280808000000b",
	"0061736d0100000001c380808000044e02500060000050005f016400004e02500060000050005f016402004e025001006000005001015f056400006400006402006402006404004e025001046000005f00028f8080800002024d3701680000024d3701680004",
}

func rv64PublicGCTypeSubtypingExtendedProjectionLinkBytes(t testing.TB, variant int) []byte {
	t.Helper()
	data, err := hex.DecodeString(rv64PublicGCTypeSubtypingExtendedProjectionLinkHex[variant-1])
	wantLen := []int{114, 102}[variant-1]
	wantSHA := [][sha256.Size]byte{
		{0x4b, 0x62, 0x73, 0x8d, 0x11, 0x27, 0x0b, 0x2a, 0xc5, 0xb4, 0x3e, 0x5d, 0xe7, 0xc8, 0x10, 0x5d, 0xda, 0xc4, 0x49, 0xd9, 0xf2, 0x3d, 0xe0, 0x9c, 0xe4, 0x41, 0x60, 0x99, 0x6f, 0x30, 0x2d, 0x62},
		{0x59, 0x92, 0xb9, 0x26, 0xa2, 0xf2, 0xed, 0x28, 0xc4, 0xe6, 0xd5, 0x14, 0x9d, 0x97, 0xff, 0x07, 0x5c, 0x0e, 0x6f, 0xdb, 0xcc, 0x9f, 0xb8, 0xec, 0x81, 0x94, 0xfa, 0x96, 0x27, 0x14, 0x05, 0xdb},
	}[variant-1]
	if err != nil || len(data) != wantLen || sha256.Sum256(data) != wantSHA {
		t.Fatal("extended-projection-link source identity changed", variant, err)
	}
	return data
}

func rv64PublicGCTypeSubtypingExtendedProjectionLinkCompile(t *testing.T, variant int, artifact bool) *Compiled {
	t.Helper()

	original, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingExtendedProjectionLinkBytes(t, variant))
	if err != nil {
		t.Fatal(err)
	}

	return original

}

func TestRV64PublicGCTypeSubtypingExtendedProjectionLinkExecutionAndArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			providerCompiled := rv64PublicGCTypeSubtypingExtendedProjectionLinkCompile(t, 1, artifact)
			defer providerCompiled.Close()
			consumerCompiled := rv64PublicGCTypeSubtypingExtendedProjectionLinkCompile(t, 2, artifact)
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

			if got, invokeErr := provider.Invoke("h"); invokeErr != nil || len(got) != 0 {
				t.Fatalf("h = %v, %v; want []", got, invokeErr)
			}
			exported, err := provider.ExportedFunc("h")
			if err != nil {
				t.Fatal(err)
			}
			consumerModule, err := rt.Module(consumerCompiled)
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M7.h", exported)))
			consumerModule.Close()
			if err != nil {
				t.Fatal(err)
			}
			rv64RequireSubtypeStorage(t, consumer)
			rv64RequireSubtypeNativeImports(t, consumer, provider, 0, 0)
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

func TestRV64PublicGCTypeSubtypingExtendedProjectionLinkMismatchAndPublicRuntime(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_EXTENDED_PROJECTION_LINK_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	providerCompiled := rv64PublicGCTypeSubtypingExtendedProjectionLinkCompile(t, 1, false)
	consumerCompiled := rv64PublicGCTypeSubtypingExtendedProjectionLinkCompile(t, 2, false)
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
	invalid := &InstanceExport{inst: provider, localIdx: len(provider.c.Entry)}
	consumerModule, err := rt.Module(consumerCompiled)
	if err != nil {
		t.Fatal(err)
	}
	bad, invalidErr := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M7.h", invalid)))
	consumerModule.Close()
	if bad != nil || invalidErr == nil || !strings.Contains(invalidErr.Error(), "no retained framed entry") {
		t.Fatal("invalid extended-projection export admitted", invalidErr)
	}
	provider.lifeMu.Lock()
	refs, closed := provider.resourceRefs, provider.resourcesClosed
	provider.lifeMu.Unlock()
	if refs != 0 || closed {
		t.Fatalf("invalid link retained provider refs/closed=%d/%v", refs, closed)
	}
	consumerModule, err = rt.Module(consumerCompiled)
	if err != nil {
		t.Fatal(err)
	}
	bad, hostErr := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M7.h", slotHostFunc(func(HostModule, []uint64, []uint64) {}))))
	consumerModule.Close()
	if bad == nil || hostErr != nil {
		t.Fatal("public subtype host import", hostErr)
	}
	if err := bad.Close(); err != nil {
		t.Fatal(err)
	}
	oldCompiled := rv64PublicGCTypeSubtypingStructProjectionLinkCompile(t, 1, false)
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
	oldExport, err := oldProvider.ExportedFunc("g")
	if err != nil {
		t.Fatal(err)
	}
	consumerModule, err = rt.Module(consumerCompiled)
	if err != nil {
		t.Fatal(err)
	}
	bad, crossErr := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M7.h", oldExport)))
	consumerModule.Close()
	if bad == nil || crossErr != nil {
		t.Fatal("compatible projection import", crossErr)
	}
	rv64RequireSubtypeNativeImports(t, bad, oldProvider, 0, 0)
	if err := bad.Close(); err != nil {
		t.Fatal(err)
	}
}
