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

var rv64PublicGCTypeSubtypingIndependentStructLinkHex = []string{
	"0061736d0100000001a380808000034e0250006000005f016400004e0250006000005f016402004e025001006000005f00038280808000010407858080800001016700000a8880808000018280808000000b",
	"0061736d0100000001a380808000034e0250006000005f016400004e0250006000005f016402004e025001006000005f0002888080800001024d3601670000",
}

func rv64PublicGCTypeSubtypingIndependentStructLinkBytes(t testing.TB, variant int) []byte {
	t.Helper()
	data, err := hex.DecodeString(rv64PublicGCTypeSubtypingIndependentStructLinkHex[variant-1])
	wantLen := []int{82, 63}[variant-1]
	wantSHA := [][sha256.Size]byte{
		{0x7c, 0x8a, 0xf0, 0x76, 0x5c, 0x2e, 0x2d, 0x43, 0xa0, 0x7e, 0x7a, 0x6a, 0x75, 0xa8, 0x5d, 0x39, 0x65, 0x31, 0x82, 0x7c, 0x1b, 0x2c, 0xb4, 0x40, 0x2a, 0x24, 0x27, 0x73, 0x08, 0x78, 0x1d, 0xff},
		{0xa5, 0x93, 0xd0, 0xdb, 0x0e, 0x5f, 0x17, 0x3a, 0xaa, 0xc2, 0xd6, 0x00, 0x7b, 0x84, 0xa4, 0xb2, 0x68, 0xd7, 0xad, 0x20, 0x47, 0xa4, 0xe8, 0xcd, 0x8f, 0xe3, 0xa2, 0x75, 0xef, 0x9b, 0x08, 0x20},
	}[variant-1]
	if err != nil || len(data) != wantLen || sha256.Sum256(data) != wantSHA {
		t.Fatal("independent-struct-link source identity changed", variant, err)
	}
	return data
}

func rv64PublicGCTypeSubtypingIndependentStructLinkCompile(t *testing.T, variant int, artifact bool) *Compiled {
	t.Helper()

	original, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingIndependentStructLinkBytes(t, variant))
	if err != nil {
		t.Fatal(err)
	}

	return original

}

func TestRV64PublicGCTypeSubtypingIndependentStructLinkExecutionAndArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			providerCompiled := rv64PublicGCTypeSubtypingIndependentStructLinkCompile(t, 1, artifact)
			defer providerCompiled.Close()
			consumerCompiled := rv64PublicGCTypeSubtypingIndependentStructLinkCompile(t, 2, artifact)
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

			if got, invokeErr := provider.Invoke("g"); invokeErr != nil || len(got) != 0 {
				t.Fatalf("g = %v, %v; want []", got, invokeErr)
			}
			exported, err := provider.ExportedFunc("g")
			if err != nil {
				t.Fatal(err)
			}
			consumerModule, err := rt.Module(consumerCompiled)
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M6.g", exported)))
			consumerModule.Close()
			if err != nil {
				t.Fatal(err)
			}
			rv64RequireSubtypeStorage(t, consumer)
			rv64RequireSubtypeNativeImports(t, consumer, provider, 0)
			provider.lifeMu.Lock()
			refs, closed := provider.resourceRefs, provider.resourcesClosed
			provider.lifeMu.Unlock()
			if refs != 1 || closed {
				t.Fatalf("linked provider refs/closed=%d/%v", refs, closed)
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

func TestRV64PublicGCTypeSubtypingIndependentStructLinkMismatchAndPublicRuntime(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_INDEPENDENT_STRUCT_LINK_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	providerCompiled := rv64PublicGCTypeSubtypingIndependentStructLinkCompile(t, 1, false)
	consumerCompiled := rv64PublicGCTypeSubtypingIndependentStructLinkCompile(t, 2, false)
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
	bad, invalidErr := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M6.g", invalid)))
	consumerModule.Close()
	if bad != nil || invalidErr == nil || !strings.Contains(invalidErr.Error(), "no retained framed entry") {
		t.Fatal("invalid independent-struct export admitted", invalidErr)
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
	bad, hostErr := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M6.g", slotHostFunc(func(HostModule, []uint64, []uint64) {}))))
	consumerModule.Close()
	if bad == nil || hostErr != nil {
		t.Fatal("public subtype host import", hostErr)
	}
	if err := bad.Close(); err != nil {
		t.Fatal(err)
	}
	oldCompiled := rv64PublicGCTypeSubtypingStructMismatchLinkCompile(t, 1, false)
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
	bad, crossErr := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M6.g", oldExport)))
	consumerModule.Close()
	if bad != nil || crossErr == nil || !strings.Contains(crossErr.Error(), "signature mismatch") {
		t.Fatal("cross-product independent-struct import admitted", crossErr)
	}
}
