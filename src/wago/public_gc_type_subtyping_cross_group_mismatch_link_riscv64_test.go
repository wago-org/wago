//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
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
	dir, mode := os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_CROSS_GROUP_MISMATCH_LINK_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_CROSS_GROUP_MISMATCH_LINK_ARTIFACT_MODE")
	path := filepath.Join(dir, []string{"provider.wago", "consumer.wago"}[variant-1])
	if mode == "load" {
		if !artifact {
			t.Skip("compiler-free runner selected artifact case")
		}
		blob, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := LoadTrustedArtifact(blob)
		if err != nil {
			t.Fatal(err)
		}
		return loaded
	}
	original, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingCrossGroupMismatchLinkBytes(t, variant))
	if err != nil {
		t.Fatal(err)
	}
	if !artifact && mode != "prepare" {
		return original
	}
	blob, err := original.MarshalBinary()
	if err != nil {
		original.Close()
		t.Fatal(err)
	}
	if mode == "prepare" {
		if err = os.MkdirAll(dir, 0o700); err != nil {
			original.Close()
			t.Fatal(err)
		}
		if err = os.WriteFile(path, blob, 0o600); err != nil {
			original.Close()
			t.Fatal(err)
		}
	}
	if !artifact {
		return original
	}
	loaded, err := LoadTrustedArtifact(blob)
	original.Close()
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

func TestRV64PublicGCTypeSubtypingCrossGroupMismatchLinkExecutionAndArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			providerCompiled := rv64PublicGCTypeSubtypingCrossGroupMismatchLinkCompile(t, 1, artifact)
			defer providerCompiled.Close()
			consumerCompiled := rv64PublicGCTypeSubtypingCrossGroupMismatchLinkCompile(t, 2, artifact)
			defer consumerCompiled.Close()
			for _, compiled := range []*Compiled{providerCompiled, consumerCompiled} {
				view := compiled.executionView()
				if !rv64ArtifactContractValid(view) || view.validate() != nil {
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
			defer provider.Close()
			if provider.gc != nil {
				t.Fatal("cross-group-mismatch-link provider allocated a collector")
			}
			rv64RequireSubtypeStorage(t, provider)

			if got, invokeErr := provider.Invoke("f"); invokeErr != nil || len(got) != 0 {
				t.Fatalf("f = %v, %v; want []", got, invokeErr)
			}
			exported, err := provider.ExportedFunc("f")
			if err != nil {
				t.Fatal(err)
			}
			consumerModule, err := rt.Module(consumerCompiled)
			if err != nil {
				t.Fatal(err)
			}
			consumer, linkErr := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M10.f", exported)))
			consumerModule.Close()
			if consumer != nil || linkErr == nil || !strings.Contains(linkErr.Error(), "signature mismatch") {
				t.Fatalf("incompatible consumer link = %v, %v; want nil/signature mismatch", consumer, linkErr)
			}
			provider.lifeMu.Lock()
			refs, closed := provider.resourceRefs, provider.resourcesClosed
			provider.lifeMu.Unlock()
			if refs != 0 || closed {
				t.Fatalf("mismatched link retained provider refs/closed = %d/%v, want 0/false", refs, closed)
			}
			if _, err = marshalCompiled(consumerCompiled.executionView()); err != nil {
				t.Fatal("failed link mutated binding-independent consumer", err)
			}
		})
	}
}

func TestRV64PublicGCTypeSubtypingCrossGroupMismatchLinkAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_CROSS_GROUP_MISMATCH_LINK_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	provider := rv64PublicGCTypeSubtypingCrossGroupMismatchLinkCompile(t, 1, false)
	consumer := rv64PublicGCTypeSubtypingCrossGroupMismatchLinkCompile(t, 2, false)
	defer provider.Close()
	defer consumer.Close()
	for name, mutate := range map[string]func(*Compiled){
		"provider-code":               func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"provider-type":               func(v *Compiled) { v.Types[3].Supers = nil },
		"provider-desc":               func(v *Compiled) { v.GCTypeDescs[3].HasSuper = false },
		"provider-execution-contract": func(v *Compiled) { v.setRV64ExecutionContract(&rv64ExecutionContract{}) },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := rv64ReferenceStartCopy(provider.executionView())
			mutate(candidate)
			if err := candidate.validate(); err == nil {
				t.Fatal("hostile cross-group-mismatch-link provider mutation admitted")
			}
			rv64RequireRejectedArtifact(t, candidate)
		})
	}
	for name, mutate := range map[string]func(*Compiled){
		"consumer-import":             func(v *Compiled) { v.Imports[0] = "M9.f" },
		"consumer-signature":          func(v *Compiled) { v.importFuncSigs[0].TypeIndex = 1 },
		"consumer-type":               func(v *Compiled) { v.Types[1].Supers = nil },
		"consumer-desc":               func(v *Compiled) { v.GCTypeDescs[1].HasSuper = false },
		"consumer-execution-contract": func(v *Compiled) { v.setRV64ExecutionContract(&rv64ExecutionContract{}) },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := rv64ReferenceStartCopy(consumer.executionView())
			mutate(candidate)
			if err := candidate.validate(); err == nil {
				t.Fatal("hostile cross-group-mismatch-link consumer mutation admitted")
			}
			rv64RequireRejectedArtifact(t, candidate)
		})
	}
	providerView, consumerView := provider.executionView(), consumer.executionView()
	for _, pair := range []struct {
		name                     string
		metadataOwner, codeOwner *Compiled
	}{
		{"consumer-metadata-provider-image", consumerView, providerView},
		{"provider-metadata-consumer-image", providerView, consumerView},
	} {
		t.Run(pair.name, func(t *testing.T) {
			candidate := rv64ReferenceStartCopy(pair.metadataOwner)
			candidate.code = slices.Clone(pair.codeOwner.code)
			candidate.Entry = slices.Clone(pair.codeOwner.Entry)
			candidate.InternalEntry = slices.Clone(pair.codeOwner.InternalEntry)
			if err := candidate.validate(); err == nil {
				t.Fatal("cross-paired cross-group-mismatch-link artifact admitted")
			}
			rv64RequireRejectedArtifact(t, candidate)
		})
	}
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

func TestRV64PublicGCTypeSubtypingCrossGroupMismatchLinkCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_CROSS_GROUP_MISMATCH_LINK_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	for variant := 1; variant <= 2; variant++ {
		c, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingCrossGroupMismatchLinkBytes(t, variant))
		if c != nil {
			c.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
			t.Fatal("compiler-free cross-group-mismatch-link path used source compilation", variant, err)
		}
	}
}
