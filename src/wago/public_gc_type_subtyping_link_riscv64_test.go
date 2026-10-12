//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

var rv64PublicGCTypeSubtypingLinkHex = []string{
	"0061736d01000000019780808000035000600001705001006000016301500101600001630203848080800003000102079080808000030266300000026631000102663200020a9c8080800003848080800000d0700b848080800000d0010b848080800000d0020b",
	"0061736d01000000019780808000035000600001705001006000016301500101600001630202ab8080800006014d0266300000014d0266310000014d0266310001014d0266320000014d0266320001014d0266320002",
	"0061736d01000000019780808000035000600001705001006000016301500101600001630202888080800001014d0266300001",
	"0061736d01000000019780808000035000600001705001006000016301500101600001630202888080800001014d0266300002",
	"0061736d01000000019780808000035000600001705001006000016301500101600001630202888080800001014d0266310002",
}

func rv64PublicGCTypeSubtypingLinkBytes(t testing.TB, variant int) []byte {
	t.Helper()
	data, err := hex.DecodeString(rv64PublicGCTypeSubtypingLinkHex[variant-1])
	wantLen := []int{103, 86, 51, 51, 51}[variant-1]
	wantSHA := [][sha256.Size]byte{
		{0x8e, 0x9b, 0xdb, 0xeb, 0x27, 0xa4, 0x96, 0x32, 0x8e, 0xb9, 0x52, 0x9e, 0x0e, 0xa6, 0x29, 0xd1, 0x4a, 0x01, 0x12, 0x4b, 0x65, 0x7c, 0x4e, 0xb0, 0xaa, 0xd7, 0x4a, 0x8b, 0xd0, 0xf4, 0x26, 0xdb},
		{0xea, 0x4d, 0x5a, 0xaf, 0x13, 0xa9, 0x74, 0x4b, 0xd3, 0x19, 0xa1, 0xb3, 0x3d, 0x1e, 0xe2, 0x30, 0x3c, 0xfa, 0xec, 0xc8, 0x4d, 0xae, 0x73, 0xa4, 0x17, 0x93, 0x51, 0xa6, 0xfb, 0x91, 0xa7, 0x60},
		{0x63, 0x4f, 0x7c, 0xaa, 0x3c, 0x4e, 0x26, 0xb7, 0x57, 0xfc, 0xa7, 0xa9, 0xa9, 0xf8, 0x36, 0x7f, 0x99, 0xe3, 0x33, 0x04, 0xc8, 0x7f, 0x7b, 0x2c, 0xf6, 0xec, 0x7d, 0x1e, 0x31, 0x56, 0x65, 0x35},
		{0xfe, 0x07, 0x22, 0x81, 0x54, 0xa2, 0x7a, 0x9d, 0xe4, 0x70, 0x2a, 0xfb, 0x12, 0x18, 0x77, 0x09, 0x53, 0x6e, 0x89, 0x44, 0x95, 0xe8, 0xfa, 0x2e, 0x34, 0xa7, 0x10, 0xe2, 0xdd, 0x7c, 0x0b, 0x88},
		{0x24, 0xce, 0x2b, 0x2e, 0xec, 0x63, 0x1e, 0xe2, 0x94, 0x6c, 0x64, 0x1e, 0x05, 0x45, 0xd0, 0x6d, 0xc1, 0x17, 0x9e, 0x2e, 0x9b, 0xa6, 0x46, 0xae, 0x59, 0xc5, 0xd3, 0x79, 0x74, 0x11, 0x16, 0x49},
	}[variant-1]
	if err != nil || len(data) != wantLen || sha256.Sum256(data) != wantSHA {
		t.Fatal("link source identity changed", variant, err)
	}
	return data
}

func rv64PublicGCTypeSubtypingLinkCompile(t *testing.T, variant int, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_LINK_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_LINK_ARTIFACT_MODE")
	path := filepath.Join(dir, []string{"provider.wago", "consumer.wago", "unlinkable-31.wago", "unlinkable-32.wago", "unlinkable-33.wago"}[variant-1])
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
	original, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingLinkBytes(t, variant))
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

func TestRV64PublicGCTypeSubtypingLinkExecutionAndArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			providerCompiled := rv64PublicGCTypeSubtypingLinkCompile(t, 1, artifact)
			consumerCompiled := rv64PublicGCTypeSubtypingLinkCompile(t, 2, artifact)
			for variant, c := range []*Compiled{providerCompiled.executionView(), consumerCompiled.executionView()} {
				metadata, err := marshalCompiledMetadata(c)
				if err != nil || len(metadata) != rv64GCTypeSubtypingLinkMetadataLens[variant] || sha256.Sum256(metadata) != rv64GCTypeSubtypingLinkMetadataDigests[variant] || rv64GCTypeSubtypingLinkProfileWithCode(c, false, true) != uint8(variant+1) || railshotValidateCompiled(c) != nil {
					providerCompiled.Close()
					consumerCompiled.Close()
					t.Fatalf("missing exact link witness %d: metadata=%d/%x err=%v profile=%d validation=%v", variant+1, len(metadata), sha256.Sum256(metadata), err, rv64GCTypeSubtypingLinkProfileWithCode(c, false, true), railshotValidateCompiled(c))
				}
			}
			cfg := rv64I31Config()
			cfg.railshotState.tailProducts = true
			rt := NewRuntime(WithRuntimeConfig(cfg))
			providerModule, err := rt.Module(providerCompiled)
			if err != nil {
				t.Fatal(err)
			}
			provider, err := rt.Instantiate(context.Background(), providerModule, WithImports(NewImports()))
			providerModule.Close()
			if err != nil {
				t.Fatal(err)
			}
			exports := make([]*InstanceExport, 3)
			for i, name := range []string{"f0", "f1", "f2"} {
				exports[i], err = provider.ExportedFunc(name)
				if err != nil {
					t.Fatal(err)
				}
				if got, invokeErr := provider.Invoke(name); invokeErr != nil || len(got) != 1 || got[0] != 0 {
					t.Fatalf("%s = %v, %v; want [0]", name, got, invokeErr)
				}
			}
			consumerModule, err := rt.Module(consumerCompiled)
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M.f0", exports[0], "M.f1", exports[1], "M.f2", exports[2])))
			consumerModule.Close()
			if err != nil {
				t.Fatal(err)
			}
			if len(provider.funcRefDescs) != 4*coreruntime.FuncRefDescBytes || len(consumer.funcRefDescs) != 7*coreruntime.FuncRefDescBytes {
				t.Fatal("link descriptor arenas are incomplete")
			}
			for importIndex, providerFunction := range []int{0, 1, 1, 2, 2, 2} {
				consumerOff := (importIndex + 1) * coreruntime.FuncRefDescBytes
				providerOff := (providerFunction + 1) * coreruntime.FuncRefDescBytes
				got := binary.LittleEndian.Uint64(consumer.funcRefDescs[consumerOff+coreruntime.TableEntryRefSlotOffset:])
				want := binary.LittleEndian.Uint64(provider.funcRefDescs[providerOff+coreruntime.TableEntryRefSlotOffset:])
				if got == 0 || got != want {
					t.Fatalf("consumer import %d identity = %#x, want provider function %d identity %#x", importIndex, got, providerFunction, want)
				}
			}
			if err = provider.Close(); err != nil {
				t.Fatal(err)
			}
			provider.lifeMu.Lock()
			refs, closed := provider.resourceRefs, provider.resourcesClosed
			provider.lifeMu.Unlock()
			if refs != 1 || closed {
				t.Fatalf("logically closed provider refs/closed = %d/%v, want 1/false", refs, closed)
			}
			if err = consumer.Close(); err != nil {
				t.Fatal(err)
			}
			provider.lifeMu.Lock()
			refs, closed = provider.resourceRefs, provider.resourcesClosed
			provider.lifeMu.Unlock()
			if refs != 0 || !closed {
				t.Fatalf("provider after consumer close refs/closed = %d/%v, want 0/true", refs, closed)
			}
			providerCompiled.Close()
			consumerCompiled.Close()
			rt.Close()
		})
	}
}

func TestRV64PublicGCTypeSubtypingLinkAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_LINK_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	provider := rv64PublicGCTypeSubtypingLinkCompile(t, 1, false)
	consumer := rv64PublicGCTypeSubtypingLinkCompile(t, 2, false)
	for name, mutate := range map[string]func(*Compiled){
		"provider-code": func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"provider-type": func(v *Compiled) { v.Types[1].Supers[0] ^= 1 },
		"provider-desc": func(v *Compiled) { v.GCTypeDescs[1].Super ^= 1 },
		"provider-product": func(v *Compiled) {
			cache := *v.codeCache
			v.codeCache = &cache
			v.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingLinkConsumer
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(provider.executionView())
			candidate.validateMemo = &validateMemo{}
			mutate(candidate)
			if rv64GCTypeSubtypingLink(candidate) || !errors.Is(railshotValidateCompiled(candidate), railshot.ErrUnsupported) {
				t.Fatal("hostile provider mutation admitted")
			}
		})
	}
	for name, mutate := range map[string]func(*Compiled){
		"consumer-import":    func(v *Compiled) { v.Imports[5] = "M.f1" },
		"consumer-signature": func(v *Compiled) { v.importFuncSigs[5].TypeIndex = 1 },
		"consumer-type":      func(v *Compiled) { v.Types[2].Supers[0] = 0 },
		"consumer-desc":      func(v *Compiled) { v.GCTypeDescs[2].Super = 0 },
		"consumer-product": func(v *Compiled) {
			cache := *v.codeCache
			v.codeCache = &cache
			v.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingLinkProvider
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(consumer.executionView())
			candidate.validateMemo = &validateMemo{}
			mutate(candidate)
			if rv64GCTypeSubtypingLink(candidate) || !errors.Is(railshotValidateCompiled(candidate), railshot.ErrUnsupported) {
				t.Fatal("hostile consumer mutation admitted")
			}
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
			candidate := cloneCompiledMetadata(pair.metadataOwner)
			candidate.code = slices.Clone(pair.codeOwner.code)
			candidate.Entry = slices.Clone(pair.codeOwner.Entry)
			candidate.InternalEntry = slices.Clone(pair.codeOwner.InternalEntry)
			candidate.validateMemo = &validateMemo{}
			if rv64GCTypeSubtypingLink(candidate) || !errors.Is(railshotValidateCompiled(candidate), railshot.ErrUnsupported) {
				t.Fatal("cross-paired link artifact admitted")
			}
		})
	}
	for variant := 3; variant <= 5; variant++ {
		unlinkable := rv64PublicGCTypeSubtypingLinkCompile(t, variant, false)
		if got := rv64GCTypeSubtypingLinkProfileWithCode(unlinkable.executionView(), false, true); got != uint8(variant) {
			unlinkable.Close()
			t.Fatalf("unlinkable .%d profile = %d, want %d", variant+28, got, variant)
		}
		candidate := cloneCompiledMetadata(unlinkable.executionView())
		candidate.importFuncSigs[0].TypeIndex ^= 1
		candidate.validateMemo = &validateMemo{}
		if rv64GCTypeSubtypingLink(candidate) || !errors.Is(railshotValidateCompiled(candidate), railshot.ErrUnsupported) {
			unlinkable.Close()
			t.Fatalf("hostile unlinkable .%d signature mutation admitted", variant+28)
		}
		unlinkable.Close()
	}
	provider.Close()
	consumer.Close()
}

func TestRV64PublicGCTypeSubtypingLinkMismatchAndPermission(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_LINK_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	providerCompiled := rv64PublicGCTypeSubtypingLinkCompile(t, 1, false)
	consumerCompiled := rv64PublicGCTypeSubtypingLinkCompile(t, 2, false)
	defer providerCompiled.Close()
	defer consumerCompiled.Close()
	plain := NewRuntime(WithRuntimeConfig(rv64I31Config()))
	mod, err := plain.Module(providerCompiled)
	if err == nil {
		_, err = plain.Instantiate(context.Background(), mod, WithImports(NewImports()))
		mod.Close()
	}
	plain.Close()
	if !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatal("link provider instantiated without private runtime permission", err)
	}
	cfg := rv64I31Config()
	cfg.railshotState.tailProducts = true
	rt := NewRuntime(WithRuntimeConfig(cfg))
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
	f0, _ := provider.ExportedFunc("f0")
	f1, _ := provider.ExportedFunc("f1")
	consumerModule, err := rt.Module(consumerCompiled)
	if err != nil {
		t.Fatal(err)
	}
	bad, linkErr := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M.f0", f0, "M.f1", f1, "M.f2", f1)))
	consumerModule.Close()
	if bad != nil || linkErr == nil || !strings.Contains(linkErr.Error(), "signature mismatch") {
		t.Fatal("later subtype mismatch was admitted", linkErr)
	}
}

func TestRV64PublicGCTypeSubtypingLinkExpectedUnlinkables(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			providerCompiled := rv64PublicGCTypeSubtypingLinkCompile(t, 1, artifact)
			defer providerCompiled.Close()
			cfg := rv64I31Config()
			cfg.railshotState.tailProducts = true
			rt := NewRuntime(WithRuntimeConfig(cfg))
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
			exports := make(map[string]*InstanceExport, 2)
			for _, name := range []string{"f0", "f1"} {
				exports[name], err = provider.ExportedFunc(name)
				if err != nil {
					t.Fatal(err)
				}
			}
			for variant := 3; variant <= 5; variant++ {
				consumerCompiled := rv64PublicGCTypeSubtypingLinkCompile(t, variant, artifact)
				view := consumerCompiled.executionView()
				metadata, metadataErr := marshalCompiledMetadata(view)
				if metadataErr != nil || len(metadata) != rv64GCTypeSubtypingLinkMetadataLens[variant-1] || sha256.Sum256(metadata) != rv64GCTypeSubtypingLinkMetadataDigests[variant-1] || rv64GCTypeSubtypingLinkProfileWithCode(view, false, true) != uint8(variant) || railshotValidateCompiled(view) != nil {
					consumerCompiled.Close()
					t.Fatalf("missing exact unlinkable .%d witness", variant+28)
				}
				consumerModule, moduleErr := rt.Module(consumerCompiled)
				if moduleErr != nil {
					consumerCompiled.Close()
					t.Fatal(moduleErr)
				}
				name := "f0"
				if variant == 5 {
					name = "f1"
				}
				bad, linkErr := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M."+name, exports[name])))
				consumerModule.Close()
				consumerCompiled.Close()
				if bad != nil || linkErr == nil || !strings.Contains(linkErr.Error(), "signature mismatch") {
					t.Fatalf("expected unlinkable .%d = %v, %v; want signature mismatch", variant+28, bad, linkErr)
				}
				provider.lifeMu.Lock()
				refs, closed := provider.resourceRefs, provider.resourcesClosed
				provider.lifeMu.Unlock()
				if refs != 0 || closed {
					t.Fatalf("unlinkable .%d retained provider refs/closed = %d/%v, want 0/false", variant+28, refs, closed)
				}
			}
		})
	}
}

func TestRV64PublicGCTypeSubtypingLinkCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_LINK_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	for variant := 1; variant <= 5; variant++ {
		c, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingLinkBytes(t, variant))
		if c != nil {
			c.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
			t.Fatal("compiler-free link path used source compilation", variant, err)
		}
	}
}
