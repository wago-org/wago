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

var rv64PublicGCTypeSubtypingDuplicateRecursiveLinkHex = []string{
	"0061736d0100000001a380808000024e025000600001647050010060000164004e02500060000164705001026000016402038380808000020001078d80808000020366313100000366313200010a918080800002838080800000000b838080800000000b",
	"0061736d0100000001a380808000024e025000600001647050010060000164004e0250006000016470500102600001640202a58080800004024d38036631310000024d38036631310002024d38036631320001024d38036631320003",
}

func rv64PublicGCTypeSubtypingDuplicateRecursiveLinkBytes(t testing.TB, variant int) []byte {
	t.Helper()
	data, err := hex.DecodeString(rv64PublicGCTypeSubtypingDuplicateRecursiveLinkHex[variant-1])
	wantLen := []int{100, 92}[variant-1]
	wantSHA := [][sha256.Size]byte{
		{0xce, 0xe5, 0x3b, 0x6e, 0x42, 0x09, 0x32, 0xfa, 0xec, 0x8b, 0xf1, 0x66, 0xa6, 0xae, 0x79, 0xcf, 0xab, 0x88, 0xf7, 0xca, 0x89, 0x0c, 0xd3, 0x98, 0x51, 0xd3, 0xf7, 0x0b, 0x93, 0x24, 0x71, 0xaa},
		{0x30, 0xff, 0x7e, 0x3b, 0xef, 0xab, 0x74, 0x05, 0xc6, 0x35, 0x54, 0x52, 0x6e, 0xcf, 0x2e, 0x61, 0xfc, 0x9e, 0xcf, 0x2b, 0x65, 0xe1, 0x75, 0x41, 0x4d, 0xb0, 0x7a, 0xa7, 0x74, 0xe6, 0xa5, 0x40},
	}[variant-1]
	if err != nil || len(data) != wantLen || sha256.Sum256(data) != wantSHA {
		t.Fatal("duplicate-recursive-link source identity changed", variant, err)
	}
	return data
}

func rv64PublicGCTypeSubtypingDuplicateRecursiveLinkCompile(t *testing.T, variant int, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_DUPLICATE_RECURSIVE_LINK_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_DUPLICATE_RECURSIVE_LINK_ARTIFACT_MODE")
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
	original, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingDuplicateRecursiveLinkBytes(t, variant))
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

func TestRV64PublicGCTypeSubtypingDuplicateRecursiveLinkExecutionAndArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			providerCompiled := rv64PublicGCTypeSubtypingDuplicateRecursiveLinkCompile(t, 1, artifact)
			defer providerCompiled.Close()
			consumerCompiled := rv64PublicGCTypeSubtypingDuplicateRecursiveLinkCompile(t, 2, artifact)
			defer consumerCompiled.Close()
			for i, compiled := range []*Compiled{providerCompiled, consumerCompiled} {
				view := compiled.executionView()
				metadata, err := marshalCompiledMetadata(view)
				if err != nil || len(metadata) != rv64GCTypeSubtypingDuplicateRecursiveLinkMetadataLens[i] || sha256.Sum256(metadata) != rv64GCTypeSubtypingDuplicateRecursiveLinkMetadataDigests[i] || rv64GCTypeSubtypingDuplicateRecursiveLinkProfileWithCode(view, false, true) != uint8(i+1) || railshotValidateCompiled(view) != nil {
					t.Fatalf("missing exact duplicate-recursive witness %d: metadata=%d/%x err=%v profile=%d validation=%v", i+1, len(metadata), sha256.Sum256(metadata), err, rv64GCTypeSubtypingDuplicateRecursiveLinkProfileWithCode(view, false, true), railshotValidateCompiled(view))
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
			if provider.gc != nil || len(provider.funcRefDescs) != 3*coreruntime.FuncRefDescBytes {
				t.Fatal("duplicate-recursive provider collector/descriptor invariant")
			}
			identities := make([]uint64, 2)
			exports := make([]*InstanceExport, 2)
			for i, name := range []string{"f11", "f12"} {
				off := (i + 1) * coreruntime.FuncRefDescBytes
				identities[i] = binary.LittleEndian.Uint64(provider.funcRefDescs[off+coreruntime.TableEntryRefSlotOffset:])
				if want := uint64(uintptr(unsafe.Pointer(&provider.funcRefDescs[off]))); identities[i] != want {
					t.Fatalf("provider %s identity=%#x want self=%#x", name, identities[i], want)
				}
				if got, invokeErr := provider.Invoke(name); invokeErr == nil || len(got) != 0 {
					t.Fatalf("%s = %v, %v; want unreachable trap", name, got, invokeErr)
				}
				exports[i], err = provider.ExportedFunc(name)
				if err != nil {
					t.Fatal(err)
				}
			}
			if identities[0] == identities[1] {
				t.Fatal("provider functions share canonical identity")
			}
			consumerModule, err := rt.Module(consumerCompiled)
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M8.f11", exports[0], "M8.f12", exports[1])))
			consumerModule.Close()
			if err != nil {
				t.Fatal(err)
			}
			if consumer.gc != nil || len(consumer.funcRefDescs) != 5*coreruntime.FuncRefDescBytes {
				t.Fatal("duplicate-recursive consumer collector/descriptor invariant")
			}
			for importIndex, providerFunc := range []int{0, 0, 1, 1} {
				off := (importIndex + 1) * coreruntime.FuncRefDescBytes
				if got := binary.LittleEndian.Uint64(consumer.funcRefDescs[off+coreruntime.TableEntryRefSlotOffset:]); got != identities[providerFunc] {
					t.Fatalf("consumer import %d identity=%#x want %#x", importIndex, got, identities[providerFunc])
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

func TestRV64PublicGCTypeSubtypingDuplicateRecursiveLinkAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_DUPLICATE_RECURSIVE_LINK_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	provider := rv64PublicGCTypeSubtypingDuplicateRecursiveLinkCompile(t, 1, false)
	consumer := rv64PublicGCTypeSubtypingDuplicateRecursiveLinkCompile(t, 2, false)
	defer provider.Close()
	defer consumer.Close()
	for name, mutate := range map[string]func(*Compiled){
		"provider-code": func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"provider-type": func(v *Compiled) { v.Types[3].Supers = nil },
		"provider-desc": func(v *Compiled) { v.GCTypeDescs[3].HasSuper = false },
		"provider-product": func(v *Compiled) {
			cache := *v.codeCache
			v.codeCache = &cache
			v.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingDuplicateRecursiveLinkConsumer
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(provider.executionView())
			candidate.validateMemo = &validateMemo{}
			mutate(candidate)
			if rv64GCTypeSubtypingDuplicateRecursiveLink(candidate) || !errors.Is(railshotValidateCompiled(candidate), railshot.ErrUnsupported) {
				t.Fatal("hostile duplicate-recursive provider admitted")
			}
		})
	}
	for name, mutate := range map[string]func(*Compiled){
		"consumer-import":    func(v *Compiled) { v.Imports[3] = "M7.f12" },
		"consumer-signature": func(v *Compiled) { v.importFuncSigs[3].TypeIndex = 2 },
		"consumer-type":      func(v *Compiled) { v.Types[3].Supers = nil },
		"consumer-desc":      func(v *Compiled) { v.GCTypeDescs[3].HasSuper = false },
		"consumer-product": func(v *Compiled) {
			cache := *v.codeCache
			v.codeCache = &cache
			v.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingDuplicateRecursiveLinkProvider
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(consumer.executionView())
			candidate.validateMemo = &validateMemo{}
			mutate(candidate)
			if rv64GCTypeSubtypingDuplicateRecursiveLink(candidate) || !errors.Is(railshotValidateCompiled(candidate), railshot.ErrUnsupported) {
				t.Fatal("hostile duplicate-recursive consumer admitted")
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
		if rv64GCTypeSubtypingDuplicateRecursiveLink(candidate) || !errors.Is(railshotValidateCompiled(candidate), railshot.ErrUnsupported) {
			t.Fatal("cross-paired duplicate-recursive artifact admitted")
		}
	}
}

func TestRV64PublicGCTypeSubtypingDuplicateRecursiveLinkMismatchAndPermission(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_DUPLICATE_RECURSIVE_LINK_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	providerCompiled := rv64PublicGCTypeSubtypingDuplicateRecursiveLinkCompile(t, 1, false)
	consumerCompiled := rv64PublicGCTypeSubtypingDuplicateRecursiveLinkCompile(t, 2, false)
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
		t.Fatal("duplicate-recursive provider instantiated without private permission", err)
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
	valid, err := provider.ExportedFunc("f12")
	if err != nil {
		t.Fatal(err)
	}
	invalid := &InstanceExport{inst: provider, localIdx: len(provider.c.Entry)}
	consumerModule, err := rt.Module(consumerCompiled)
	if err != nil {
		t.Fatal(err)
	}
	bad, invalidErr := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M8.f11", invalid, "M8.f12", valid)))
	consumerModule.Close()
	if bad != nil || invalidErr == nil || !strings.Contains(invalidErr.Error(), "no retained framed entry") {
		t.Fatal("invalid duplicate-recursive export admitted", invalidErr)
	}
	provider.lifeMu.Lock()
	refs, closed := provider.resourceRefs, provider.resourcesClosed
	provider.lifeMu.Unlock()
	if refs != 0 || closed {
		t.Fatalf("invalid link retained provider refs/closed=%d/%v", refs, closed)
	}
	host := slotHostFunc(func(HostModule, []uint64, []uint64) {})
	consumerModule, err = rt.Module(consumerCompiled)
	if err != nil {
		t.Fatal(err)
	}
	bad, hostErr := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M8.f11", host, "M8.f12", host)))
	consumerModule.Close()
	if bad != nil || hostErr == nil || !strings.Contains(hostErr.Error(), "nonscalar host signature") {
		t.Fatal("host duplicate-recursive import admitted", hostErr)
	}
	oldCompiled := rv64PublicGCTypeSubtypingExtendedProjectionLinkCompile(t, 1, false)
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
	oldExport, err := oldProvider.ExportedFunc("h")
	if err != nil {
		t.Fatal(err)
	}
	consumerModule, err = rt.Module(consumerCompiled)
	if err != nil {
		t.Fatal(err)
	}
	bad, crossErr := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M8.f11", oldExport, "M8.f12", oldExport)))
	consumerModule.Close()
	if bad != nil || crossErr == nil || !strings.Contains(crossErr.Error(), "outside the exact gc/type-subtyping link product") {
		t.Fatal("cross-product duplicate-recursive import admitted", crossErr)
	}
}

func TestRV64PublicGCTypeSubtypingDuplicateRecursiveLinkCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_DUPLICATE_RECURSIVE_LINK_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	for variant := 1; variant <= 2; variant++ {
		c, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingDuplicateRecursiveLinkBytes(t, variant))
		if c != nil {
			c.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
			t.Fatal("compiler-free duplicate-recursive path used source compilation", variant, err)
		}
	}
}
