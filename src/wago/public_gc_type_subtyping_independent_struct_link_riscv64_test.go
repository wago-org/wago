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
	"unsafe"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
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
	dir, mode := os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_INDEPENDENT_STRUCT_LINK_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_INDEPENDENT_STRUCT_LINK_ARTIFACT_MODE")
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
	original, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingIndependentStructLinkBytes(t, variant))
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

func TestRV64PublicGCTypeSubtypingIndependentStructLinkExecutionAndArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			providerCompiled := rv64PublicGCTypeSubtypingIndependentStructLinkCompile(t, 1, artifact)
			defer providerCompiled.Close()
			consumerCompiled := rv64PublicGCTypeSubtypingIndependentStructLinkCompile(t, 2, artifact)
			defer consumerCompiled.Close()
			for i, compiled := range []*Compiled{providerCompiled, consumerCompiled} {
				view := compiled.executionView()
				metadata, err := marshalCompiledMetadata(view)
				if err != nil || len(metadata) != rv64GCTypeSubtypingIndependentStructLinkMetadataLens[i] || sha256.Sum256(metadata) != rv64GCTypeSubtypingIndependentStructLinkMetadataDigests[i] || rv64GCTypeSubtypingIndependentStructLinkProfileWithCode(view, false, true) != uint8(i+1) || railshotValidateCompiled(view) != nil {
					t.Fatalf("missing exact independent-struct-link witness %d: metadata=%d/%x err=%v profile=%d validation=%v", i+1, len(metadata), sha256.Sum256(metadata), err, rv64GCTypeSubtypingIndependentStructLinkProfileWithCode(view, false, true), railshotValidateCompiled(view))
				}
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
			if provider.gc != nil || len(provider.funcRefDescs) != 2*coreruntime.FuncRefDescBytes {
				t.Fatal("independent-struct provider collector/descriptor invariant")
			}
			providerOff := coreruntime.FuncRefDescBytes
			providerIdentity := binary.LittleEndian.Uint64(provider.funcRefDescs[providerOff+coreruntime.TableEntryRefSlotOffset:])
			if providerIdentity != uint64(uintptr(unsafe.Pointer(&provider.funcRefDescs[providerOff]))) {
				t.Fatal("provider lacks canonical descriptor identity")
			}
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
			if consumer.gc != nil || len(consumer.funcRefDescs) != 2*coreruntime.FuncRefDescBytes {
				t.Fatal("independent-struct consumer collector/descriptor invariant")
			}
			consumerOff := coreruntime.FuncRefDescBytes
			if got := binary.LittleEndian.Uint64(consumer.funcRefDescs[consumerOff+coreruntime.TableEntryRefSlotOffset:]); got != providerIdentity {
				t.Fatalf("consumer identity=%#x want provider %#x", got, providerIdentity)
			}
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

func TestRV64PublicGCTypeSubtypingIndependentStructLinkAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_INDEPENDENT_STRUCT_LINK_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	provider := rv64PublicGCTypeSubtypingIndependentStructLinkCompile(t, 1, false)
	consumer := rv64PublicGCTypeSubtypingIndependentStructLinkCompile(t, 2, false)
	defer provider.Close()
	defer consumer.Close()
	for name, mutate := range map[string]func(*Compiled){
		"provider-code": func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"provider-type": func(v *Compiled) { v.Types[4].Supers = nil },
		"provider-desc": func(v *Compiled) { v.GCTypeDescs[4].HasSuper = false },
		"provider-product": func(v *Compiled) {
			cache := *v.codeCache
			v.codeCache = &cache
			v.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingIndependentStructLinkConsumer
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(provider.executionView())
			candidate.validateMemo = &validateMemo{}
			mutate(candidate)
			if rv64GCTypeSubtypingIndependentStructLink(candidate) || !errors.Is(railshotValidateCompiled(candidate), railshot.ErrUnsupported) {
				t.Fatal("hostile independent-struct provider admitted")
			}
		})
	}
	for name, mutate := range map[string]func(*Compiled){
		"consumer-import":    func(v *Compiled) { v.Imports[0] = "M5.g" },
		"consumer-signature": func(v *Compiled) { v.importFuncSigs[0].TypeIndex = 2 },
		"consumer-type":      func(v *Compiled) { v.Types[0].Supers = []uint32{2} },
		"consumer-desc":      func(v *Compiled) { v.GCTypeDescs[0].HasSuper = true },
		"consumer-product": func(v *Compiled) {
			cache := *v.codeCache
			v.codeCache = &cache
			v.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingIndependentStructLinkProvider
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(consumer.executionView())
			candidate.validateMemo = &validateMemo{}
			mutate(candidate)
			if rv64GCTypeSubtypingIndependentStructLink(candidate) || !errors.Is(railshotValidateCompiled(candidate), railshot.ErrUnsupported) {
				t.Fatal("hostile independent-struct consumer admitted")
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
			if rv64GCTypeSubtypingIndependentStructLink(candidate) || !errors.Is(railshotValidateCompiled(candidate), railshot.ErrUnsupported) {
				t.Fatal("cross-paired independent-struct artifact admitted")
			}
		})
	}
}

func TestRV64PublicGCTypeSubtypingIndependentStructLinkMismatchAndPermission(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_INDEPENDENT_STRUCT_LINK_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	providerCompiled := rv64PublicGCTypeSubtypingIndependentStructLinkCompile(t, 1, false)
	consumerCompiled := rv64PublicGCTypeSubtypingIndependentStructLinkCompile(t, 2, false)
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
		t.Fatal("independent-struct provider instantiated without private permission", err)
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
	if bad != nil || hostErr == nil || !strings.Contains(hostErr.Error(), "exact gc/type-subtyping link provider") {
		t.Fatal("host independent-struct import admitted", hostErr)
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
	if bad != nil || crossErr == nil || !strings.Contains(crossErr.Error(), "outside the exact gc/type-subtyping link product") {
		t.Fatal("cross-product independent-struct import admitted", crossErr)
	}
}

func TestRV64PublicGCTypeSubtypingIndependentStructLinkCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_INDEPENDENT_STRUCT_LINK_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	for variant := 1; variant <= 2; variant++ {
		c, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingIndependentStructLinkBytes(t, variant))
		if c != nil {
			c.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
			t.Fatal("compiler-free independent-struct-link path used source compilation", variant, err)
		}
	}
}
