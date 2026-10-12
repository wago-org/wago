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

var rv64PublicGCTypeSubtypingStructLinkHex = []string{
	"0061736d01000000019780808000024e0250006000005f016400004e025001006000005f00038280808000010207858080800001016700000a8880808000018280808000000b",
	"0061736d01000000019780808000024e0250006000005f016400004e025001006000005f0002888080800001024d3301670002",
}

func rv64PublicGCTypeSubtypingStructLinkBytes(t testing.TB, variant int) []byte {
	t.Helper()
	data, err := hex.DecodeString(rv64PublicGCTypeSubtypingStructLinkHex[variant-1])
	wantLen := []int{70, 51}[variant-1]
	wantSHA := [][sha256.Size]byte{
		{0xac, 0x63, 0x80, 0x2e, 0x38, 0x27, 0xe3, 0x33, 0x89, 0xd9, 0x2f, 0xf8, 0xa8, 0xbd, 0x25, 0xb6, 0x23, 0x1f, 0x1d, 0xde, 0x96, 0xba, 0xb5, 0xcb, 0x77, 0xa0, 0xe1, 0xd0, 0x94, 0xf8, 0x0e, 0x6f},
		{0x5f, 0x09, 0x09, 0x89, 0xed, 0xc6, 0x24, 0x37, 0xb5, 0x6b, 0x36, 0xc6, 0x9a, 0x31, 0x6c, 0xdc, 0xfd, 0xde, 0xc4, 0xa6, 0x3d, 0x45, 0x1b, 0xd9, 0x44, 0x3a, 0xd5, 0x9d, 0xa7, 0x5a, 0xf0, 0xa3},
	}[variant-1]
	if err != nil || len(data) != wantLen || sha256.Sum256(data) != wantSHA {
		t.Fatal("struct-link source identity changed", variant, err)
	}
	return data
}

func rv64PublicGCTypeSubtypingStructLinkCompile(t *testing.T, variant int, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_STRUCT_LINK_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_STRUCT_LINK_ARTIFACT_MODE")
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
	original, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingStructLinkBytes(t, variant))
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

func TestRV64PublicGCTypeSubtypingStructLinkExecutionAndArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			providerCompiled := rv64PublicGCTypeSubtypingStructLinkCompile(t, 1, artifact)
			defer providerCompiled.Close()
			consumerCompiled := rv64PublicGCTypeSubtypingStructLinkCompile(t, 2, artifact)
			defer consumerCompiled.Close()
			for i, compiled := range []*Compiled{providerCompiled, consumerCompiled} {
				view := compiled.executionView()
				metadata, err := marshalCompiledMetadata(view)
				if err != nil || len(metadata) != rv64GCTypeSubtypingStructLinkMetadataLens[i] || sha256.Sum256(metadata) != rv64GCTypeSubtypingStructLinkMetadataDigests[i] || rv64GCTypeSubtypingStructLinkProfileWithCode(view, false, true) != uint8(i+1) || railshotValidateCompiled(view) != nil {
					t.Fatalf("missing exact struct-link witness %d: metadata=%d/%x err=%v profile=%d validation=%v required=%#x staged=%#x native=%v/%v/%v imports=%d funcs=%d types=%d descs=%d", i+1, len(metadata), sha256.Sum256(metadata), err, rv64GCTypeSubtypingStructLinkProfileWithCode(view, false, true), railshotValidateCompiled(view), view.requiredFeatures, view.stagedFeatures(), view.nativeReferenceFunctionABI, view.nativeReferenceCallABI, view.nativeReferenceControlABI, view.NumImports, len(view.Funcs), len(view.Types), len(view.GCTypeDescs))
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
			if provider.gc != nil {
				t.Fatal("struct-link provider allocated a collector")
			}
			if got, want := len(provider.funcRefDescs), 2*coreruntime.FuncRefDescBytes; got != want {
				t.Fatalf("provider descriptor arena = %d, want %d", got, want)
			}
			providerOff := coreruntime.FuncRefDescBytes
			providerIdentity := binary.LittleEndian.Uint64(provider.funcRefDescs[providerOff+coreruntime.TableEntryRefSlotOffset:])
			if providerIdentity != uint64(uintptr(unsafe.Pointer(&provider.funcRefDescs[providerOff]))) {
				t.Fatalf("provider identity = %#x, want canonical descriptor address", providerIdentity)
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
			consumer, err := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M3.g", exported)))
			consumerModule.Close()
			if err != nil {
				t.Fatal(err)
			}
			if consumer.gc != nil {
				t.Fatal("struct-link consumer allocated a collector")
			}
			if got, want := len(consumer.funcRefDescs), 2*coreruntime.FuncRefDescBytes; got != want {
				t.Fatalf("consumer descriptor arena = %d, want %d", got, want)
			}
			consumerOff := coreruntime.FuncRefDescBytes
			if got := binary.LittleEndian.Uint64(consumer.funcRefDescs[consumerOff+coreruntime.TableEntryRefSlotOffset:]); got != providerIdentity {
				t.Fatalf("consumer identity = %#x, want provider canonical %#x", got, providerIdentity)
			}
			provider.lifeMu.Lock()
			refs, closed := provider.resourceRefs, provider.resourcesClosed
			provider.lifeMu.Unlock()
			if refs != 1 || closed {
				t.Fatalf("linked provider refs/closed = %d/%v, want 1/false", refs, closed)
			}
			if err = provider.Close(); err != nil {
				t.Fatal(err)
			}
			provider.lifeMu.Lock()
			refs, closed = provider.resourceRefs, provider.resourcesClosed
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
				t.Fatalf("released provider refs/closed = %d/%v, want 0/true", refs, closed)
			}
		})
	}
}

func TestRV64PublicGCTypeSubtypingStructLinkAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_STRUCT_LINK_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	provider := rv64PublicGCTypeSubtypingStructLinkCompile(t, 1, false)
	consumer := rv64PublicGCTypeSubtypingStructLinkCompile(t, 2, false)
	defer provider.Close()
	defer consumer.Close()
	for name, mutate := range map[string]func(*Compiled){
		"provider-code": func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"provider-type": func(v *Compiled) { v.Types[2].Supers = nil },
		"provider-desc": func(v *Compiled) { v.GCTypeDescs[2].HasSuper = false },
		"provider-product": func(v *Compiled) {
			cache := *v.codeCache
			v.codeCache = &cache
			v.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingStructLinkConsumer
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(provider.executionView())
			candidate.validateMemo = &validateMemo{}
			mutate(candidate)
			if rv64GCTypeSubtypingStructLink(candidate) || !errors.Is(railshotValidateCompiled(candidate), railshot.ErrUnsupported) {
				t.Fatal("hostile struct-link provider mutation admitted")
			}
		})
	}
	for name, mutate := range map[string]func(*Compiled){
		"consumer-import":    func(v *Compiled) { v.Imports[0] = "M4.g" },
		"consumer-signature": func(v *Compiled) { v.importFuncSigs[0].TypeIndex = 0 },
		"consumer-type":      func(v *Compiled) { v.Types[2].Supers = nil },
		"consumer-desc":      func(v *Compiled) { v.GCTypeDescs[2].HasSuper = false },
		"consumer-product": func(v *Compiled) {
			cache := *v.codeCache
			v.codeCache = &cache
			v.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingStructLinkProvider
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(consumer.executionView())
			candidate.validateMemo = &validateMemo{}
			mutate(candidate)
			if rv64GCTypeSubtypingStructLink(candidate) || !errors.Is(railshotValidateCompiled(candidate), railshot.ErrUnsupported) {
				t.Fatal("hostile struct-link consumer mutation admitted")
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
			if rv64GCTypeSubtypingStructLink(candidate) || !errors.Is(railshotValidateCompiled(candidate), railshot.ErrUnsupported) {
				t.Fatal("cross-paired struct-link artifact admitted")
			}
		})
	}
}

func TestRV64PublicGCTypeSubtypingStructLinkMismatchAndPermission(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_STRUCT_LINK_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	providerCompiled := rv64PublicGCTypeSubtypingStructLinkCompile(t, 1, false)
	consumerCompiled := rv64PublicGCTypeSubtypingStructLinkCompile(t, 2, false)
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
		t.Fatal("struct-link provider instantiated without private runtime permission", err)
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
	exported, err := provider.ExportedFunc("g")
	if err != nil {
		t.Fatal(err)
	}
	invalid := &InstanceExport{inst: provider, localIdx: len(provider.c.Entry)}
	consumerModule, err := rt.Module(consumerCompiled)
	if err != nil {
		t.Fatal(err)
	}
	bad, invalidErr := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M3.g", invalid)))
	consumerModule.Close()
	if bad != nil || invalidErr == nil || !strings.Contains(invalidErr.Error(), "no retained framed entry") {
		t.Fatal("invalid struct-link export admitted", invalidErr)
	}
	provider.lifeMu.Lock()
	refs, closed := provider.resourceRefs, provider.resourcesClosed
	provider.lifeMu.Unlock()
	if refs != 0 || closed {
		t.Fatalf("invalid link retained provider refs/closed = %d/%v, want 0/false", refs, closed)
	}
	consumerModule, err = rt.Module(consumerCompiled)
	if err != nil {
		t.Fatal(err)
	}
	host := slotHostFunc(func(HostModule, []uint64, []uint64) {})
	bad, hostErr := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M3.g", host)))
	consumerModule.Close()
	if bad != nil || hostErr == nil || !strings.Contains(hostErr.Error(), "exact gc/type-subtyping link provider") {
		t.Fatal("host struct link admitted", hostErr)
	}
	if exported == nil {
		t.Fatal("provider export missing")
	}
	oldProviderCompiled := rv64PublicGCTypeSubtypingFinalityLinkCompile(t, 1, false)
	defer oldProviderCompiled.Close()
	oldModule, err := rt.Module(oldProviderCompiled)
	if err != nil {
		t.Fatal(err)
	}
	oldProvider, err := rt.Instantiate(context.Background(), oldModule, WithImports(NewImports()))
	oldModule.Close()
	if err != nil {
		t.Fatal(err)
	}
	defer oldProvider.Close()
	oldExport, err := oldProvider.ExportedFunc("f1")
	if err != nil {
		t.Fatal(err)
	}
	consumerModule, err = rt.Module(consumerCompiled)
	if err != nil {
		t.Fatal(err)
	}
	bad, crossErr := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M3.g", oldExport)))
	consumerModule.Close()
	if bad != nil || crossErr == nil || !strings.Contains(crossErr.Error(), "outside the exact gc/type-subtyping link product") {
		t.Fatal("cross-product struct link admitted", crossErr)
	}
	oldProvider.lifeMu.Lock()
	refs, closed = oldProvider.resourceRefs, oldProvider.resourcesClosed
	oldProvider.lifeMu.Unlock()
	if refs != 0 || closed {
		t.Fatalf("cross-product rejection retained old provider refs/closed = %d/%v, want 0/false", refs, closed)
	}
}

func TestRV64PublicGCTypeSubtypingStructLinkCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_STRUCT_LINK_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	for variant := 1; variant <= 2; variant++ {
		c, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingStructLinkBytes(t, variant))
		if c != nil {
			c.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
			t.Fatal("compiler-free struct-link path used source compilation", variant, err)
		}
	}
}
