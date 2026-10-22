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

var rv64PublicGCTypeSubtypingStructMismatchLinkHex = []string{
	"0061736d0100000001a380808000034e0250006000005f016400004e0250006000005f016400004e025001026000005f00038280808000010407858080800001016700000a8880808000018280808000000b",
	"0061736d01000000019780808000024e0250006000005f016400004e025001006000005f0002888080800001024d3501670002",
}

func rv64PublicGCTypeSubtypingStructMismatchLinkBytes(t testing.TB, variant int) []byte {
	t.Helper()
	data, err := hex.DecodeString(rv64PublicGCTypeSubtypingStructMismatchLinkHex[variant-1])
	wantLen := []int{82, 51}[variant-1]
	wantSHA := [][sha256.Size]byte{
		{0x04, 0x94, 0xd7, 0xc9, 0x5b, 0x50, 0xe1, 0x51, 0xac, 0x8e, 0x0f, 0x9e, 0xb8, 0xa1, 0xc9, 0x35, 0xa0, 0x16, 0xdb, 0x45, 0xb1, 0x96, 0x93, 0x78, 0xed, 0x95, 0xd4, 0x03, 0x69, 0xfd, 0xa0, 0x62},
		{0xbb, 0x59, 0x8c, 0xc8, 0x9f, 0x2d, 0x73, 0x72, 0x01, 0x90, 0xe6, 0xc7, 0xe1, 0x15, 0xbe, 0xc1, 0x04, 0x01, 0x3b, 0xf8, 0xeb, 0xea, 0xd4, 0xc4, 0x17, 0xd1, 0x7e, 0x70, 0x15, 0x98, 0xc7, 0xa1},
	}[variant-1]
	if err != nil || len(data) != wantLen || sha256.Sum256(data) != wantSHA {
		t.Fatal("struct-mismatch-link source identity changed", variant, err)
	}
	return data
}

func rv64PublicGCTypeSubtypingStructMismatchLinkCompile(t *testing.T, variant int, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_STRUCT_MISMATCH_LINK_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_STRUCT_MISMATCH_LINK_ARTIFACT_MODE")
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
	original, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingStructMismatchLinkBytes(t, variant))
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

func TestRV64PublicGCTypeSubtypingStructMismatchLinkExecutionAndArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			providerCompiled := rv64PublicGCTypeSubtypingStructMismatchLinkCompile(t, 1, artifact)
			defer providerCompiled.Close()
			consumerCompiled := rv64PublicGCTypeSubtypingStructMismatchLinkCompile(t, 2, artifact)
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
				t.Fatal("struct-mismatch-link provider allocated a collector")
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
			consumer, linkErr := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M5.g", exported)))
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

func TestRV64PublicGCTypeSubtypingStructMismatchLinkAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_STRUCT_MISMATCH_LINK_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	provider := rv64PublicGCTypeSubtypingStructMismatchLinkCompile(t, 1, false)
	consumer := rv64PublicGCTypeSubtypingStructMismatchLinkCompile(t, 2, false)
	defer provider.Close()
	defer consumer.Close()
	for name, mutate := range map[string]func(*Compiled){
		"provider-code":               func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"provider-type":               func(v *Compiled) { v.Types[4].Supers = nil },
		"provider-desc":               func(v *Compiled) { v.GCTypeDescs[4].HasSuper = false },
		"provider-execution-contract": func(v *Compiled) { v.setRV64ExecutionContract(&rv64ExecutionContract{}) },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := rv64ReferenceStartCopy(provider.executionView())
			mutate(candidate)
			if err := candidate.validate(); err == nil {
				t.Fatal("hostile struct-mismatch-link provider mutation admitted")
			}
			rv64RequireRejectedArtifact(t, candidate)
		})
	}
	for name, mutate := range map[string]func(*Compiled){
		"consumer-import":             func(v *Compiled) { v.Imports[0] = "M4.g" },
		"consumer-signature":          func(v *Compiled) { v.importFuncSigs[0].TypeIndex = 0 },
		"consumer-type":               func(v *Compiled) { v.Types[2].Supers = nil },
		"consumer-desc":               func(v *Compiled) { v.GCTypeDescs[2].HasSuper = false },
		"consumer-execution-contract": func(v *Compiled) { v.setRV64ExecutionContract(&rv64ExecutionContract{}) },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := rv64ReferenceStartCopy(consumer.executionView())
			mutate(candidate)
			if err := candidate.validate(); err == nil {
				t.Fatal("hostile struct-mismatch-link consumer mutation admitted")
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
				t.Fatal("cross-paired struct-mismatch-link artifact admitted")
			}
			rv64RequireRejectedArtifact(t, candidate)
		})
	}
}

func TestRV64PublicGCTypeSubtypingStructMismatchLinkMismatchAndPublicRuntime(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_STRUCT_MISMATCH_LINK_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	providerCompiled := rv64PublicGCTypeSubtypingStructMismatchLinkCompile(t, 1, false)
	consumerCompiled := rv64PublicGCTypeSubtypingStructMismatchLinkCompile(t, 2, false)
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
	exported, err := provider.ExportedFunc("g")
	if err != nil {
		t.Fatal(err)
	}
	try := func(binding any) error {
		consumerModule, moduleErr := rt.Module(consumerCompiled)
		if moduleErr != nil {
			return moduleErr
		}
		bad, linkErr := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M5.g", binding)))
		consumerModule.Close()
		if bad != nil {
			bad.Close()
			return errors.New("bad import instantiated")
		}
		return linkErr
	}
	invalid := &InstanceExport{inst: provider, localIdx: len(provider.c.Entry)}
	if err = try(invalid); err == nil || !strings.Contains(err.Error(), "no retained framed entry") {
		t.Fatal("invalid struct-mismatch-link export admitted", err)
	}
	rv64RequireSubtypePublicImport(t, rt, consumerCompiled, testImports("M5.g", slotHostFunc(func(HostModule, []uint64, []uint64) {})))
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
		t.Fatal("cross-product struct-mismatch-link import admitted", err)
	}
	provider.lifeMu.Lock()
	refs, closed := provider.resourceRefs, provider.resourcesClosed
	provider.lifeMu.Unlock()
	if refs != 0 || closed || exported == nil {
		t.Fatalf("rejected imports retained provider refs/closed=%d/%v export=%v", refs, closed, exported)
	}
}

func TestRV64PublicGCTypeSubtypingStructMismatchLinkCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_STRUCT_MISMATCH_LINK_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	for variant := 1; variant <= 2; variant++ {
		c, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingStructMismatchLinkBytes(t, variant))
		if c != nil {
			c.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
			t.Fatal("compiler-free struct-mismatch-link path used source compilation", variant, err)
		}
	}
}
