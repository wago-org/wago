//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	corergc "github.com/wago-org/wago/src/core/runtime/gc"
)

const rv64I31CoreSource = "\x00\x61\x73\x6d\x01\x00\x00\x00" +
	"\x01\x06\x01\x60\x01\x7f\x01\x7f" +
	"\x03\x03\x02\x00\x00" +
	"\x07\x15\x02\x06signed\x00\x00\x08unsigned\x00\x01" +
	"\x0a\x13\x02\x08\x00\x20\x00\xfb\x1c\xfb\x1d\x0b" +
	"\x08\x00\x20\x00\xfb\x1c\xfb\x1e\x0b"

func rv64I31Config() *RuntimeConfig {
	cfg := compatibilityDefaultConfig().WithCoreFeatures(platformCoreFeatures() | CoreFeatureGC)
	cfg.railshotState.i31Products = true
	return cfg
}

func rv64I31Compile(t *testing.T, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_I31_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_I31_ARTIFACT_MODE")
	path := filepath.Join(dir, "i31-core.wago")
	if mode == "load" {
		if !artifact {
			t.Skip("compiler-free runner selected artifact case")
		}
		blob, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		c, err := LoadTrustedArtifact(blob)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
		return c
	}
	c, err := rv64I31Config().Compile([]byte(rv64I31CoreSource))
	if err != nil {
		t.Fatal(err)
	}
	original := c
	t.Cleanup(func() { _ = original.Close() })
	if artifact || mode == "prepare" {
		blob, err := c.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if mode == "prepare" {
			if err = os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, blob, 0600); err != nil {
				t.Fatal(err)
			}
		}
		if artifact {
			loaded, loadErr := LoadTrustedArtifact(blob)
			err = loadErr
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = loaded.Close() })
			c = loaded
		}
	}
	return c
}

func TestRV64PublicI31CoreExecutionAndArtifact(t *testing.T) {
	wantSource := [sha256.Size]byte{0x91, 0xa5, 0x75, 0x41, 0xb3, 0x27, 0x61, 0xe0, 0xa6, 0x76, 0xb9, 0xf6, 0xf4, 0x79, 0x18, 0xf4, 0xcb, 0xfb, 0x40, 0x2e, 0xa1, 0x4f, 0x65, 0x84, 0xa4, 0x70, 0xa7, 0x39, 0x2b, 0x21, 0x21, 0x21}
	if len(rv64I31CoreSource) != 65 || sha256.Sum256([]byte(rv64I31CoreSource)) != wantSource {
		t.Fatal("i31 source identity changed")
	}
	inputs := []uint64{0, 1, 0x3fffffff, 0x40000000, 0x7fffffff, 0x80000000, 0xffffffff, 0xcafe123400000001, 0xdeadbeefffffffff}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			c := rv64I31Compile(t, artifact)
			if !rv64I31Core(c.executionView()) || c.stagedGCI31Product() != stagedGCI31ProductCore || c.stagedFeatures()&CoreFeatureGC == 0 {
				v := c.executionView()
				t.Logf("i31 diagnostic required=%v structural=%v staged=%v product=%v code=%d/%x entries=%v/%v funcs=%+v types=%+v descs=%+v exports=%v context=%t regDisabled=%t tableABI=%d dir=%+v", v.requiredFeatures, compiledStructuralRequiredFeatures(v), v.stagedFeatures(), v.stagedGCI31Product(), len(v.code), sha256.Sum256(v.code), v.Entry, v.InternalEntry, v.Funcs, v.Types, v.GCTypeDescs, v.Exports, v.nativeContextTrapABI, v.registerABIDisabled, v.nativeTable64ABI, v.memoryDir)
				t.Fatal("missing exact retained i31 witness")
			}
			rt := NewRuntime(WithRuntimeConfig(rv64I31Config()))
			defer rt.Close()
			in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			if in.gc != nil {
				t.Fatal("non-allocating i31 product provisioned a collector")
			}
			c.Close() // the live instance must retain its own executable lease
			for _, input := range inputs {
				payload := uint32(input) & 0x7fffffff
				wantSigned := uint64(uint32(int32(payload<<1) >> 1))
				wantUnsigned := uint64(payload)
				for export, want := range map[string]uint64{"signed": wantSigned, "unsigned": wantUnsigned} {
					got, err := in.Invoke(export, input)
					if err != nil || !slices.Equal(got, []uint64{want}) {
						t.Fatalf("%s(%#x) = %#x, want %#x: %v", export, input, got, want, err)
					}
				}
			}
		})
	}
}

func TestRV64PublicI31CoreAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_I31_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	source := []byte(rv64I31CoreSource)
	for name, mutate := range map[string]func([]byte){
		"empty-table-section": func(v []byte) {},
		"signed-op":           func(v []byte) { v[54] = 0x1e },
		"unsigned-op":         func(v []byte) { v[63] = 0x1d },
		"export":              func(v []byte) { v[27] ^= 1 },
	} {
		candidate := slices.Clone(source)
		if name == "empty-table-section" {
			candidate = slices.Concat(candidate[:21], []byte{4, 1, 0}, candidate[21:])
		} else {
			mutate(candidate)
		}
		c, err := rv64I31Config().Compile(candidate)
		if c != nil {
			c.Close()
		}
		if err == nil || !errors.Is(err, railshot.ErrUnsupported) {
			t.Fatal("near-miss source admitted", name, err)
		}
	}
	c := rv64I31Compile(t, false)
	base := c.executionView()
	for name, mutate := range map[string]func(*Compiled){
		"code": func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"joint-entry": func(v *Compiled) {
			v.Entry = slices.Clone(v.Entry)
			v.InternalEntry = slices.Clone(v.InternalEntry)
			v.Entry[1], v.InternalEntry[1] = 352, 352
		},
		"missing-feature": func(v *Compiled) { v.requiredFeatures &^= CoreFeatureGC },
		"extra-feature":   func(v *Compiled) { v.requiredFeatures |= CoreFeatureTailCall },
		"export": func(v *Compiled) {
			v.Exports = map[string]int{"signed": 0, "unsigned": 0}
		},
		"descriptor": func(v *Compiled) {
			v.GCTypeDescs = slices.Clone(v.GCTypeDescs)
			v.GCTypeDescs[0].Kind = corergc.KindStruct
		},
		"type-key": func(v *Compiled) {
			v.FuncTypeID = slices.Clone(v.FuncTypeID)
			v.FuncTypeID[1] ^= 1
		},
		"context-header": func(v *Compiled) { v.needsFuncRefContextHeader = false },
		"table-context":  func(v *Compiled) { v.nativeTableContextABI = false },
	} {
		copy := *base
		mutate(&copy)
		if rv64I31Core(&copy) || railshotValidateCompiled(&copy) == nil {
			t.Fatal("hostile retained mutation admitted", name)
		}
		if blob, err := marshalCompiled(&copy); err == nil {
			loaded, loadErr := LoadTrustedArtifact(blob)
			if loaded != nil {
				normalized := name == "context-header" && loadErr == nil && rv64I31Core(loaded.executionView())
				loaded.Close()
				if normalized {
					continue // The loader derives this header from the authoritative context ABI.
				}
			}
			if loadErr == nil {
				t.Fatal("hostile artifact mutation admitted", name)
			}
		}
	}

	plain := compatibilityDefaultConfig().WithCoreFeatures(platformCoreFeatures() | CoreFeatureGC)
	if err := plain.Validate(); err == nil {
		t.Fatal("GC feature escaped the private i31 qualification gate")
	}
	if err := rv64I31Config().Validate(); err != nil {
		t.Fatal("qualified i31 config rejected", err)
	}
}
