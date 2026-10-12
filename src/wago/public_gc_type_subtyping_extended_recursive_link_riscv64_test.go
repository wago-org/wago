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

var rv64PublicGCTypeSubtypingExtendedRecursiveLinkHex = []string{
	"0061736d0100000001c780808000044e025000600001647050010060000164004e025000600001647050010260000164024e02500100600001647050010460000164044e0250010260000164705001066000016406038380808000020405078d80808000020367313100000367313200010a918080800002838080800000000b838080800000000b",
	"0061736d0100000001c780808000044e025000600001647050010060000164004e025000600001647050010260000164024e02500100600001647050010460000164044e025001026000016470500106600001640602c98080800008024d39036731310000024d39036731310002024d39036731320000024d39036731320002024d39036731310004024d39036731310006024d39036731320005024d39036731320007",
}

func rv64PublicGCTypeSubtypingExtendedRecursiveLinkBytes(t testing.TB, variant int) []byte {
	t.Helper()
	data, err := hex.DecodeString(rv64PublicGCTypeSubtypingExtendedRecursiveLinkHex[variant-1])
	wantLen := []int{136, 164}[variant-1]
	wantSHA := [][sha256.Size]byte{
		{0xba, 0x25, 0x1b, 0x03, 0x25, 0xc3, 0xf0, 0xc4, 0xaa, 0xb8, 0x0a, 0xea, 0xcf, 0x9f, 0xaf, 0x0d, 0xd9, 0x60, 0xe4, 0x8b, 0x48, 0xb6, 0xb7, 0xa4, 0x1d, 0x2f, 0x07, 0xe0, 0x3f, 0x37, 0xd3, 0x79},
		{0xb0, 0xe1, 0x4a, 0x30, 0x1b, 0xd4, 0x40, 0xe1, 0x55, 0x0b, 0xe3, 0x5f, 0x09, 0xd9, 0xb0, 0xec, 0xb0, 0x47, 0x6b, 0x09, 0xe4, 0x67, 0xb9, 0x65, 0xb0, 0x2b, 0x69, 0x35, 0xdc, 0xac, 0x0f, 0xd2},
	}[variant-1]
	if err != nil || len(data) != wantLen || sha256.Sum256(data) != wantSHA {
		t.Fatal("extended-recursive-link source identity changed", variant, err)
	}
	return data
}

func rv64PublicGCTypeSubtypingExtendedRecursiveLinkCompile(t *testing.T, variant int, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_EXTENDED_RECURSIVE_LINK_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_EXTENDED_RECURSIVE_LINK_ARTIFACT_MODE")
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
	original, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingExtendedRecursiveLinkBytes(t, variant))
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

func TestRV64PublicGCTypeSubtypingExtendedRecursiveLinkExecutionAndArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			providerCompiled := rv64PublicGCTypeSubtypingExtendedRecursiveLinkCompile(t, 1, artifact)
			defer providerCompiled.Close()
			consumerCompiled := rv64PublicGCTypeSubtypingExtendedRecursiveLinkCompile(t, 2, artifact)
			defer consumerCompiled.Close()
			for i, compiled := range []*Compiled{providerCompiled, consumerCompiled} {
				view := compiled.executionView()
				metadata, err := marshalCompiledMetadata(view)
				if err != nil || len(metadata) != rv64GCTypeSubtypingExtendedRecursiveLinkMetadataLens[i] || sha256.Sum256(metadata) != rv64GCTypeSubtypingExtendedRecursiveLinkMetadataDigests[i] || rv64GCTypeSubtypingExtendedRecursiveLinkProfileWithCode(view, false, true) != uint8(i+1) || railshotValidateCompiled(view) != nil {
					t.Fatalf("missing exact extended-recursive witness %d: metadata=%d/%x err=%v profile=%d validation=%v", i+1, len(metadata), sha256.Sum256(metadata), err, rv64GCTypeSubtypingExtendedRecursiveLinkProfileWithCode(view, false, true), railshotValidateCompiled(view))
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
				t.Fatal("extended-recursive provider collector/descriptor invariant")
			}
			identities := make([]uint64, 2)
			exports := make([]*InstanceExport, 2)
			for i, name := range []string{"g11", "g12"} {
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
			consumer, err := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M9.g11", exports[0], "M9.g12", exports[1])))
			consumerModule.Close()
			if err != nil {
				t.Fatal(err)
			}
			if consumer.gc != nil || len(consumer.funcRefDescs) != 9*coreruntime.FuncRefDescBytes {
				t.Fatal("extended-recursive consumer collector/descriptor invariant")
			}
			for importIndex, providerFunc := range []int{0, 0, 1, 1, 0, 0, 1, 1} {
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

func TestRV64PublicGCTypeSubtypingExtendedRecursiveLinkAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_EXTENDED_RECURSIVE_LINK_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	provider := rv64PublicGCTypeSubtypingExtendedRecursiveLinkCompile(t, 1, false)
	consumer := rv64PublicGCTypeSubtypingExtendedRecursiveLinkCompile(t, 2, false)
	defer provider.Close()
	defer consumer.Close()
	for name, mutate := range map[string]func(*Compiled){
		"provider-code": func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"provider-type": func(v *Compiled) { v.Types[6].Supers = nil },
		"provider-desc": func(v *Compiled) { v.GCTypeDescs[6].HasSuper = false },
		"provider-product": func(v *Compiled) {
			cache := *v.codeCache
			v.codeCache = &cache
			v.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingExtendedRecursiveLinkConsumer
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(provider.executionView())
			candidate.validateMemo = &validateMemo{}
			mutate(candidate)
			if rv64GCTypeSubtypingExtendedRecursiveLink(candidate) || !errors.Is(railshotValidateCompiled(candidate), railshot.ErrUnsupported) {
				t.Fatal("hostile extended-recursive provider admitted")
			}
		})
	}
	for name, mutate := range map[string]func(*Compiled){
		"consumer-import":    func(v *Compiled) { v.Imports[7] = "M8.g12" },
		"consumer-signature": func(v *Compiled) { v.importFuncSigs[7].TypeIndex = 6 },
		"consumer-type":      func(v *Compiled) { v.Types[6].Supers = nil },
		"consumer-desc":      func(v *Compiled) { v.GCTypeDescs[6].HasSuper = false },
		"consumer-product": func(v *Compiled) {
			cache := *v.codeCache
			v.codeCache = &cache
			v.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingExtendedRecursiveLinkProvider
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(consumer.executionView())
			candidate.validateMemo = &validateMemo{}
			mutate(candidate)
			if rv64GCTypeSubtypingExtendedRecursiveLink(candidate) || !errors.Is(railshotValidateCompiled(candidate), railshot.ErrUnsupported) {
				t.Fatal("hostile extended-recursive consumer admitted")
			}
		})
	}
	providerView, consumerView := provider.executionView(), consumer.executionView()
	for _, pair := range []struct{ metadataOwner, codeOwner *Compiled }{{consumerView, providerView}, {providerView, consumerView}} {
		candidate := cloneCompiledMetadata(pair.metadataOwner)
		candidate.code = slices.Clone(pair.codeOwner.code)
		candidate.Entry = slices.Clone(pair.codeOwner.Entry)
		candidate.InternalEntry = slices.Clone(pair.codeOwner.InternalEntry)
		candidate.validateMemo = &validateMemo{}
		if rv64GCTypeSubtypingExtendedRecursiveLink(candidate) || !errors.Is(railshotValidateCompiled(candidate), railshot.ErrUnsupported) {
			t.Fatal("cross-paired extended-recursive artifact admitted")
		}
	}
}

func TestRV64PublicGCTypeSubtypingExtendedRecursiveLinkMismatchAndPermission(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_EXTENDED_RECURSIVE_LINK_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	providerCompiled := rv64PublicGCTypeSubtypingExtendedRecursiveLinkCompile(t, 1, false)
	consumerCompiled := rv64PublicGCTypeSubtypingExtendedRecursiveLinkCompile(t, 2, false)
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
		t.Fatal("extended-recursive provider instantiated without private permission", err)
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
	valid, err := provider.ExportedFunc("g12")
	if err != nil {
		t.Fatal(err)
	}
	invalid := &InstanceExport{inst: provider, localIdx: len(provider.c.Entry)}
	consumerModule, err := rt.Module(consumerCompiled)
	if err != nil {
		t.Fatal(err)
	}
	bad, invalidErr := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M9.g11", invalid, "M9.g12", valid)))
	consumerModule.Close()
	if bad != nil || invalidErr == nil || !strings.Contains(invalidErr.Error(), "no retained framed entry") {
		t.Fatal("invalid extended-recursive export admitted", invalidErr)
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
	bad, hostErr := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M9.g11", host, "M9.g12", host)))
	consumerModule.Close()
	if bad != nil || hostErr == nil || !strings.Contains(hostErr.Error(), "nonscalar host signature") {
		t.Fatal("host extended-recursive import admitted", hostErr)
	}
	oldCompiled := rv64PublicGCTypeSubtypingDuplicateRecursiveLinkCompile(t, 1, false)
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
	old11, err := oldProvider.ExportedFunc("f11")
	if err != nil {
		t.Fatal(err)
	}
	old12, err := oldProvider.ExportedFunc("f12")
	if err != nil {
		t.Fatal(err)
	}
	consumerModule, err = rt.Module(consumerCompiled)
	if err != nil {
		t.Fatal(err)
	}
	bad, crossErr := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M9.g11", old11, "M9.g12", old12)))
	consumerModule.Close()
	if bad != nil || crossErr == nil || !strings.Contains(crossErr.Error(), "outside the exact gc/type-subtyping link product") {
		t.Fatal("cross-product extended-recursive import admitted", crossErr)
	}
}

func TestRV64PublicGCTypeSubtypingExtendedRecursiveLinkCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_EXTENDED_RECURSIVE_LINK_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	for variant := 1; variant <= 2; variant++ {
		c, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingExtendedRecursiveLinkBytes(t, variant))
		if c != nil {
			c.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
			t.Fatal("compiler-free extended-recursive path used source compilation", variant, err)
		}
	}
}
