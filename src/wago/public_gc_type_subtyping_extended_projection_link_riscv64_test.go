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
	dir, mode := os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_EXTENDED_PROJECTION_LINK_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_EXTENDED_PROJECTION_LINK_ARTIFACT_MODE")
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
	original, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingExtendedProjectionLinkBytes(t, variant))
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

func TestRV64PublicGCTypeSubtypingExtendedProjectionLinkExecutionAndArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			providerCompiled := rv64PublicGCTypeSubtypingExtendedProjectionLinkCompile(t, 1, artifact)
			defer providerCompiled.Close()
			consumerCompiled := rv64PublicGCTypeSubtypingExtendedProjectionLinkCompile(t, 2, artifact)
			defer consumerCompiled.Close()
			for i, compiled := range []*Compiled{providerCompiled, consumerCompiled} {
				view := compiled.executionView()
				metadata, err := marshalCompiledMetadata(view)
				if err != nil || len(metadata) != rv64GCTypeSubtypingExtendedProjectionLinkMetadataLens[i] || sha256.Sum256(metadata) != rv64GCTypeSubtypingExtendedProjectionLinkMetadataDigests[i] || rv64GCTypeSubtypingExtendedProjectionLinkProfileWithCode(view, false, true) != uint8(i+1) || railshotValidateCompiled(view) != nil {
					t.Fatalf("missing exact extended-projection witness %d: metadata=%d/%x err=%v profile=%d validation=%v", i+1, len(metadata), sha256.Sum256(metadata), err, rv64GCTypeSubtypingExtendedProjectionLinkProfileWithCode(view, false, true), railshotValidateCompiled(view))
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
				t.Fatal("extended-projection provider collector/descriptor invariant")
			}
			providerOff := coreruntime.FuncRefDescBytes
			providerIdentity := binary.LittleEndian.Uint64(provider.funcRefDescs[providerOff+coreruntime.TableEntryRefSlotOffset:])
			if providerIdentity != uint64(uintptr(unsafe.Pointer(&provider.funcRefDescs[providerOff]))) {
				t.Fatal("provider lacks canonical descriptor identity")
			}
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
			if consumer.gc != nil || len(consumer.funcRefDescs) != 3*coreruntime.FuncRefDescBytes {
				t.Fatal("extended-projection consumer collector/descriptor invariant")
			}
			for importIndex := 0; importIndex < 2; importIndex++ {
				off := (importIndex + 1) * coreruntime.FuncRefDescBytes
				if got := binary.LittleEndian.Uint64(consumer.funcRefDescs[off+coreruntime.TableEntryRefSlotOffset:]); got != providerIdentity {
					t.Fatalf("consumer import %d identity=%#x want %#x", importIndex, got, providerIdentity)
				}
			}
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

func TestRV64PublicGCTypeSubtypingExtendedProjectionLinkAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_EXTENDED_PROJECTION_LINK_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	provider := rv64PublicGCTypeSubtypingExtendedProjectionLinkCompile(t, 1, false)
	consumer := rv64PublicGCTypeSubtypingExtendedProjectionLinkCompile(t, 2, false)
	defer provider.Close()
	defer consumer.Close()
	for name, mutate := range map[string]func(*Compiled){
		"provider-code": func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"provider-type": func(v *Compiled) { v.Types[6].Supers = nil },
		"provider-desc": func(v *Compiled) { v.GCTypeDescs[6].HasSuper = false },
		"provider-product": func(v *Compiled) {
			cache := *v.codeCache
			v.codeCache = &cache
			v.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingExtendedProjectionLinkConsumer
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(provider.executionView())
			candidate.validateMemo = &validateMemo{}
			mutate(candidate)
			if rv64GCTypeSubtypingExtendedProjectionLink(candidate) || !errors.Is(railshotValidateCompiled(candidate), railshot.ErrUnsupported) {
				t.Fatal("hostile extended-projection provider admitted")
			}
		})
	}
	for name, mutate := range map[string]func(*Compiled){
		"consumer-import":    func(v *Compiled) { v.Imports[1] = "M6.h" },
		"consumer-signature": func(v *Compiled) { v.importFuncSigs[1].TypeIndex = 2 },
		"consumer-type":      func(v *Compiled) { v.Types[6].Supers = nil },
		"consumer-desc":      func(v *Compiled) { v.GCTypeDescs[6].HasSuper = false },
		"consumer-product": func(v *Compiled) {
			cache := *v.codeCache
			v.codeCache = &cache
			v.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingExtendedProjectionLinkProvider
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(consumer.executionView())
			candidate.validateMemo = &validateMemo{}
			mutate(candidate)
			if rv64GCTypeSubtypingExtendedProjectionLink(candidate) || !errors.Is(railshotValidateCompiled(candidate), railshot.ErrUnsupported) {
				t.Fatal("hostile extended-projection consumer admitted")
			}
		})
	}
	providerView, consumerView := provider.executionView(), consumer.executionView()
	for _, pair := range []struct {
		metadataOwner, codeOwner *Compiled
	}{{consumerView, providerView}, {providerView, consumerView}} {
		candidate := cloneCompiledMetadata(pair.metadataOwner)
		candidate.code = slices.Clone(pair.codeOwner.code)
		candidate.Entry = slices.Clone(pair.codeOwner.Entry)
		candidate.InternalEntry = slices.Clone(pair.codeOwner.InternalEntry)
		candidate.validateMemo = &validateMemo{}
		if rv64GCTypeSubtypingExtendedProjectionLink(candidate) || !errors.Is(railshotValidateCompiled(candidate), railshot.ErrUnsupported) {
			t.Fatal("cross-paired extended-projection artifact admitted")
		}
	}
}

func TestRV64PublicGCTypeSubtypingExtendedProjectionLinkMismatchAndPermission(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_EXTENDED_PROJECTION_LINK_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	providerCompiled := rv64PublicGCTypeSubtypingExtendedProjectionLinkCompile(t, 1, false)
	consumerCompiled := rv64PublicGCTypeSubtypingExtendedProjectionLinkCompile(t, 2, false)
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
		t.Fatal("extended-projection provider instantiated without private permission", err)
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
	if bad != nil || hostErr == nil || !strings.Contains(hostErr.Error(), "exact gc/type-subtyping link provider") {
		t.Fatal("host extended-projection import admitted", hostErr)
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
	if bad != nil || crossErr == nil || !strings.Contains(crossErr.Error(), "outside the exact gc/type-subtyping link product") {
		t.Fatal("cross-product extended-projection import admitted", crossErr)
	}
}

func TestRV64PublicGCTypeSubtypingExtendedProjectionLinkCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_EXTENDED_PROJECTION_LINK_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	for variant := 1; variant <= 2; variant++ {
		c, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingExtendedProjectionLinkBytes(t, variant))
		if c != nil {
			c.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
			t.Fatal("compiler-free extended-projection path used source compilation", variant, err)
		}
	}
}
