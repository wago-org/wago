//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

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

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

var rv64PublicGCTypeSubtypingFinalityLinkHex = []string{
	"0061736d01000000018980808000025000600000600000038380808000020001078b8080800002026631000002663200010a8f80808000028280808000000b8280808000000b",
	"0061736d0100000001898080800002500060000060000002898080800001024d320266310001",
	"0061736d0100000001898080800002500060000060000002898080800001024d320266320000",
}

func rv64PublicGCTypeSubtypingFinalityLinkBytes(t testing.TB, variant int) []byte {
	t.Helper()
	data, err := hex.DecodeString(rv64PublicGCTypeSubtypingFinalityLinkHex[variant-1])
	wantLen := []int{70, 38, 38}[variant-1]
	wantSHA := [][sha256.Size]byte{
		{0xdc, 0xf5, 0x44, 0x59, 0xe9, 0xf3, 0x90, 0x87, 0xc6, 0x97, 0xc9, 0xd9, 0xed, 0xc0, 0x95, 0x5a, 0xab, 0xc0, 0x2e, 0xb2, 0x8e, 0x40, 0xb6, 0x5c, 0x84, 0x29, 0x1c, 0xbe, 0x19, 0x4a, 0x95, 0x62},
		{0xea, 0x96, 0x0d, 0xde, 0xc4, 0xf2, 0x4c, 0x95, 0x2d, 0x26, 0xee, 0x7a, 0x56, 0x73, 0x09, 0xa4, 0x1c, 0x58, 0x95, 0xcf, 0x84, 0x69, 0x0c, 0xa1, 0x20, 0xd4, 0x57, 0x7b, 0xb4, 0xc2, 0x6e, 0x08},
		{0x7f, 0xc4, 0x3b, 0xbb, 0xff, 0x42, 0xca, 0x92, 0x3d, 0xb1, 0x60, 0x4d, 0x03, 0x39, 0xca, 0xdd, 0x21, 0x45, 0x8f, 0x56, 0x71, 0xea, 0x79, 0x62, 0xd0, 0x31, 0x78, 0x6e, 0x93, 0x51, 0x79, 0x96},
	}[variant-1]
	if err != nil || len(data) != wantLen || sha256.Sum256(data) != wantSHA {
		t.Fatal("finality-link source identity changed", variant, err)
	}
	return data
}

func rv64PublicGCTypeSubtypingFinalityLinkCompile(t *testing.T, variant int, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_FINALITY_LINK_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_FINALITY_LINK_ARTIFACT_MODE")
	path := filepath.Join(dir, []string{"provider.wago", "unlinkable-35.wago", "unlinkable-36.wago"}[variant-1])
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
	original, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingFinalityLinkBytes(t, variant))
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

func TestRV64PublicGCTypeSubtypingFinalityLinkExpectedUnlinkables(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := make([]*Compiled, 3)
			for i := range compiled {
				compiled[i] = rv64PublicGCTypeSubtypingFinalityLinkCompile(t, i+1, artifact)
				defer compiled[i].Close()
				view := compiled[i].executionView()
				metadata, err := marshalCompiledMetadata(view)
				if err != nil || len(metadata) != rv64GCTypeSubtypingFinalityLinkMetadataLens[i] || sha256.Sum256(metadata) != rv64GCTypeSubtypingFinalityLinkMetadataDigests[i] || rv64GCTypeSubtypingFinalityLinkProfileWithCode(view, false, true) != uint8(i+1) || railshotValidateCompiled(view) != nil {
					t.Fatalf("missing exact finality-link witness %d: metadata=%d/%x err=%v profile=%d validation=%v", i+1, len(metadata), sha256.Sum256(metadata), err, rv64GCTypeSubtypingFinalityLinkProfileWithCode(view, false, true), railshotValidateCompiled(view))
				}
			}
			cfg := rv64I31Config()
			cfg.railshotState.tailProducts = true
			rt := NewRuntime(WithRuntimeConfig(cfg))
			defer rt.Close()
			providerModule, err := rt.Module(compiled[0])
			if err != nil {
				t.Fatal(err)
			}
			provider, err := rt.Instantiate(context.Background(), providerModule, WithImports(NewImports()))
			providerModule.Close()
			if err != nil {
				t.Fatal(err)
			}
			defer provider.Close()
			if len(provider.funcRefDescs) != 3*coreruntime.FuncRefDescBytes {
				t.Fatalf("provider descriptor arena = %d, want %d", len(provider.funcRefDescs), 3*coreruntime.FuncRefDescBytes)
			}
			exports := make(map[string]*InstanceExport, 2)
			for _, name := range []string{"f1", "f2"} {
				if got, invokeErr := provider.Invoke(name); invokeErr != nil || len(got) != 0 {
					t.Fatalf("%s = %v, %v; want []", name, got, invokeErr)
				}
				exports[name], err = provider.ExportedFunc(name)
				if err != nil {
					t.Fatal(err)
				}
			}
			for variant := 2; variant <= 3; variant++ {
				name := []string{"f1", "f2"}[variant-2]
				consumerModule, moduleErr := rt.Module(compiled[variant-1])
				if moduleErr != nil {
					t.Fatal(moduleErr)
				}
				bad, linkErr := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M2."+name, exports[name])))
				consumerModule.Close()
				if bad != nil || linkErr == nil || !strings.Contains(linkErr.Error(), "signature mismatch") {
					t.Fatalf("expected unlinkable .%d = %v, %v; want signature mismatch", variant+33, bad, linkErr)
				}
				provider.lifeMu.Lock()
				refs, closed := provider.resourceRefs, provider.resourcesClosed
				provider.lifeMu.Unlock()
				if refs != 0 || closed {
					t.Fatalf("unlinkable .%d retained provider refs/closed = %d/%v, want 0/false", variant+33, refs, closed)
				}
			}
			if err = provider.Close(); err != nil {
				t.Fatal(err)
			}
			provider.lifeMu.Lock()
			refs, closed := provider.resourceRefs, provider.resourcesClosed
			provider.lifeMu.Unlock()
			if refs != 0 || !closed {
				t.Fatalf("provider final refs/closed = %d/%v, want 0/true", refs, closed)
			}
		})
	}
}

func TestRV64PublicGCTypeSubtypingFinalityLinkAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_FINALITY_LINK_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	provider := rv64PublicGCTypeSubtypingFinalityLinkCompile(t, 1, false)
	consumer := rv64PublicGCTypeSubtypingFinalityLinkCompile(t, 2, false)
	defer provider.Close()
	defer consumer.Close()
	for name, mutate := range map[string]func(*Compiled){
		"provider-code": func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"provider-type": func(v *Compiled) { v.Types[1].Final = false },
		"provider-desc": func(v *Compiled) { v.GCTypeDescs[1].Final = false },
		"provider-product": func(v *Compiled) {
			cache := *v.codeCache
			v.codeCache = &cache
			v.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingFinalityLinkConsumer
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(provider.executionView())
			candidate.validateMemo = &validateMemo{}
			mutate(candidate)
			if rv64GCTypeSubtypingFinalityLink(candidate) || !errors.Is(railshotValidateCompiled(candidate), railshot.ErrUnsupported) {
				t.Fatal("hostile finality-link provider mutation admitted")
			}
		})
	}
	for name, mutate := range map[string]func(*Compiled){
		"consumer-import":    func(v *Compiled) { v.Imports[0] = "M2.f2" },
		"consumer-signature": func(v *Compiled) { v.importFuncSigs[0].TypeIndex = 0 },
		"consumer-type":      func(v *Compiled) { v.Types[1].Final = false },
		"consumer-desc":      func(v *Compiled) { v.GCTypeDescs[1].Final = false },
		"consumer-product": func(v *Compiled) {
			cache := *v.codeCache
			v.codeCache = &cache
			v.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingFinalityLinkProvider
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(consumer.executionView())
			candidate.validateMemo = &validateMemo{}
			mutate(candidate)
			if rv64GCTypeSubtypingFinalityLink(candidate) || !errors.Is(railshotValidateCompiled(candidate), railshot.ErrUnsupported) {
				t.Fatal("hostile finality-link consumer mutation admitted")
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
			if rv64GCTypeSubtypingFinalityLink(candidate) || !errors.Is(railshotValidateCompiled(candidate), railshot.ErrUnsupported) {
				t.Fatal("cross-paired finality-link artifact admitted")
			}
		})
	}
}

func TestRV64PublicGCTypeSubtypingFinalityLinkMismatchAndPermission(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_FINALITY_LINK_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	providerCompiled := rv64PublicGCTypeSubtypingFinalityLinkCompile(t, 1, false)
	consumerCompiled := rv64PublicGCTypeSubtypingFinalityLinkCompile(t, 2, false)
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
		t.Fatal("finality-link provider instantiated without private runtime permission", err)
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
	f1, err := provider.ExportedFunc("f1")
	if err != nil {
		t.Fatal(err)
	}
	consumerModule, err := rt.Module(consumerCompiled)
	if err != nil {
		t.Fatal(err)
	}
	host := slotHostFunc(func(HostModule, []uint64, []uint64) {})
	bad, hostErr := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M2.f1", host)))
	consumerModule.Close()
	if bad != nil || hostErr == nil || !strings.Contains(hostErr.Error(), "exact gc/type-subtyping link provider") {
		t.Fatal("host finality link admitted", hostErr)
	}
	if f1 == nil {
		t.Fatal("provider export missing")
	}
	oldProviderCompiled := rv64PublicGCTypeSubtypingLinkCompile(t, 1, false)
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
	oldExport, err := oldProvider.ExportedFunc("f0")
	if err != nil {
		t.Fatal(err)
	}
	consumerModule, err = rt.Module(consumerCompiled)
	if err != nil {
		t.Fatal(err)
	}
	bad, crossErr := rt.Instantiate(context.Background(), consumerModule, WithImports(testImports("M2.f1", oldExport)))
	consumerModule.Close()
	if bad != nil || crossErr == nil || !strings.Contains(crossErr.Error(), "outside the exact gc/type-subtyping link product") {
		t.Fatal("cross-product finality link admitted", crossErr)
	}
	oldProvider.lifeMu.Lock()
	refs, closed := oldProvider.resourceRefs, oldProvider.resourcesClosed
	oldProvider.lifeMu.Unlock()
	if refs != 0 || closed {
		t.Fatalf("cross-product rejection retained old provider refs/closed = %d/%v, want 0/false", refs, closed)
	}
}

func TestRV64PublicGCTypeSubtypingFinalityLinkCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_FINALITY_LINK_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	for variant := 1; variant <= 3; variant++ {
		c, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingFinalityLinkBytes(t, variant))
		if c != nil {
			c.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
			t.Fatal("compiler-free finality-link path used source compilation", variant, err)
		}
	}
}
