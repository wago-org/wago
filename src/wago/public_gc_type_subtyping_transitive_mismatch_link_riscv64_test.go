//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
)

var rv64PublicGCTypeSubtypingTransitiveMismatchLinkHex = []string{
	"0061736d0100000001a880808000034e0250006000005001006000004e0250006000005001006000004e025000600000500102600000038280808000010407858080800001016600000a8880808000018280808000000b",
	"0061736d01000000019b80808000024e0250006000005001006000004e02500060000050010060000002898080800001034d313101660002",
}

func rv64PublicGCTypeSubtypingTransitiveMismatchLinkBytes(t testing.TB, variant int) []byte {
	t.Helper()
	data, err := hex.DecodeString(rv64PublicGCTypeSubtypingTransitiveMismatchLinkHex[variant-1])
	wantLen := []int{87, 56}[variant-1]
	wantSHA := [][sha256.Size]byte{
		{0xaf, 0x07, 0xe7, 0xf6, 0x58, 0x6d, 0xb5, 0x7d, 0x5a, 0x92, 0x7c, 0x9c, 0x7f, 0x02, 0x5b, 0x12, 0x16, 0x57, 0xf0, 0x42, 0x37, 0xdc, 0xee, 0x03, 0x44, 0xe7, 0xdb, 0x26, 0x37, 0x98, 0xcf, 0xea},
		{0x9e, 0x70, 0x3d, 0xc0, 0xad, 0xdc, 0x7e, 0xc3, 0x5b, 0xc2, 0x5d, 0x91, 0x90, 0x31, 0x31, 0xc8, 0x15, 0xc5, 0x12, 0x73, 0x6e, 0xda, 0x1a, 0xe0, 0xf1, 0x67, 0x8e, 0xf8, 0xa7, 0xa4, 0x01, 0x44},
	}[variant-1]
	if err != nil || len(data) != wantLen || sha256.Sum256(data) != wantSHA {
		t.Fatal("transitive-mismatch-link source identity changed", variant, err)
	}
	return data
}

func rv64PublicGCTypeSubtypingTransitiveMismatchLinkCompile(t *testing.T, variant int, artifact bool) *Compiled {
	t.Helper()

	original, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingTransitiveMismatchLinkBytes(t, variant))
	if err != nil {
		t.Fatal(err)
	}

	return original

}

func TestRV64PublicGCTypeSubtypingTransitiveMismatchLinkMismatchAndPublicInstantiation(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_TRANSITIVE_MISMATCH_LINK_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	providerCompiled := rv64PublicGCTypeSubtypingTransitiveMismatchLinkCompile(t, 1, false)
	consumerCompiled := rv64PublicGCTypeSubtypingTransitiveMismatchLinkCompile(t, 2, false)
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
		t.Fatal("public provider instantiation", err)
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
	exported, err := provider.ExportedFunc("f")
	if err != nil {
		t.Fatal(err)
	}
	try := func(binding any) error {
		consumerModule, moduleErr := rt.Module(consumerCompiled)
		if moduleErr != nil {
			return moduleErr
		}
		bad, linkErr := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M11.f", binding)))
		consumerModule.Close()
		if bad != nil {
			bad.Close()
			return errors.New("bad import instantiated")
		}
		return linkErr
	}
	invalid := &InstanceExport{inst: provider, localIdx: len(provider.c.Entry)}
	if err = try(invalid); err == nil || !strings.Contains(err.Error(), "no retained framed entry") {
		t.Fatal("invalid transitive-mismatch-link export admitted", err)
	}
	rv64RequireSubtypePublicImport(t, rt, consumerCompiled, testImports("M11.f", slotHostFunc(func(HostModule, []uint64, []uint64) {})))
	crossGroupCompiled := rv64PublicGCTypeSubtypingCrossGroupMismatchLinkCompile(t, 1, false)
	defer crossGroupCompiled.Close()
	crossGroupModule, err := rt.Module(crossGroupCompiled)
	if err != nil {
		t.Fatal(err)
	}
	crossGroup, err := rt.Instantiate(context.Background(), crossGroupModule, WithImports(NewImports()))
	crossGroupModule.Close()
	if err != nil {
		t.Fatal(err)
	}
	defer crossGroup.Close()
	crossGroupExport, err := crossGroup.ExportedFunc("f")
	if err != nil {
		t.Fatal(err)
	}
	rv64RequireSubtypePublicImport(t, rt, consumerCompiled, testImports("M11.f", crossGroupExport))
	provider.lifeMu.Lock()
	refs, closed := provider.resourceRefs, provider.resourcesClosed
	provider.lifeMu.Unlock()
	if refs != 0 || closed || exported == nil {
		t.Fatalf("rejected imports retained provider refs/closed=%d/%v export=%v", refs, closed, exported)
	}
}
