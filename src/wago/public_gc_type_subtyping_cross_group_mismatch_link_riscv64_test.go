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

var rv64PublicGCTypeSubtypingCrossGroupMismatchLinkHex = []string{
	"0061736d01000000019b80808000024e0250006000005001006000004e025000600000500100600000038280808000010207858080800001016600000a8880808000018280808000000b",
	"0061736d01000000018e80808000014e02500060000050010060000002898080800001034d313001660000",
}

func rv64PublicGCTypeSubtypingCrossGroupMismatchLinkBytes(t testing.TB, variant int) []byte {
	t.Helper()
	data, err := hex.DecodeString(rv64PublicGCTypeSubtypingCrossGroupMismatchLinkHex[variant-1])
	wantLen := []int{74, 43}[variant-1]
	wantSHA := [][sha256.Size]byte{
		{0xdb, 0x07, 0xa8, 0xde, 0x50, 0x25, 0xcd, 0x21, 0x5a, 0x20, 0xa1, 0xe1, 0x18, 0x18, 0x3a, 0x22, 0x03, 0x54, 0xa1, 0xaa, 0x93, 0xea, 0x15, 0x4d, 0xaa, 0xd3, 0xf4, 0xcd, 0x75, 0xc0, 0xdf, 0xf9},
		{0x33, 0x62, 0xe5, 0xe8, 0x79, 0x44, 0xfe, 0x7f, 0x1a, 0x90, 0xcc, 0x31, 0xd6, 0xfd, 0x4b, 0xac, 0xf6, 0x43, 0x06, 0x73, 0xbc, 0x94, 0x19, 0x04, 0x2d, 0x1f, 0xc0, 0xe4, 0x87, 0xe8, 0x4a, 0x7b},
	}[variant-1]
	if err != nil || len(data) != wantLen || sha256.Sum256(data) != wantSHA {
		t.Fatal("cross-group-mismatch-link source identity changed", variant, err)
	}
	return data
}

func rv64PublicGCTypeSubtypingCrossGroupMismatchLinkCompile(t *testing.T, variant int, artifact bool) *Compiled {
	t.Helper()

	original, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingCrossGroupMismatchLinkBytes(t, variant))
	if err != nil {
		t.Fatal(err)
	}

	return original

}

func TestRV64PublicGCTypeSubtypingCrossGroupMismatchLinkMismatchAndPublicInstantiation(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_CROSS_GROUP_MISMATCH_LINK_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	providerCompiled := rv64PublicGCTypeSubtypingCrossGroupMismatchLinkCompile(t, 1, false)
	consumerCompiled := rv64PublicGCTypeSubtypingCrossGroupMismatchLinkCompile(t, 2, false)
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
		bad, linkErr := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M10.f", binding)))
		consumerModule.Close()
		if bad != nil {
			bad.Close()
			return errors.New("bad import instantiated")
		}
		return linkErr
	}
	invalid := &InstanceExport{inst: provider, localIdx: len(provider.c.Entry)}
	if err = try(invalid); err == nil || !strings.Contains(err.Error(), "no retained framed entry") {
		t.Fatal("invalid cross-group-mismatch-link export admitted", err)
	}
	rv64RequireSubtypePublicImport(t, rt, consumerCompiled, testImports("M10.f", slotHostFunc(func(HostModule, []uint64, []uint64) {})))
	projectionCompiled := rv64PublicGCTypeSubtypingStructProjectionLinkCompile(t, 1, false)
	defer projectionCompiled.Close()
	projectionModule, err := rt.Module(projectionCompiled)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := rt.Instantiate(context.Background(), projectionModule, WithImports(NewImports()))
	projectionModule.Close()
	if err != nil {
		t.Fatal(err)
	}
	defer projection.Close()
	projectionExport, err := projection.ExportedFunc("g")
	if err != nil {
		t.Fatal(err)
	}
	if err = try(projectionExport); err == nil || !strings.Contains(err.Error(), "signature mismatch") {
		t.Fatal("cross-product cross-group-mismatch-link import admitted", err)
	}
	provider.lifeMu.Lock()
	refs, closed := provider.resourceRefs, provider.resourcesClosed
	provider.lifeMu.Unlock()
	if refs != 0 || closed || exported == nil {
		t.Fatalf("rejected imports retained provider refs/closed=%d/%v export=%v", refs, closed, exported)
	}
}
