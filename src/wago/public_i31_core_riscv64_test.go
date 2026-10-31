//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"slices"
	"strings"
	"testing"
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

	c, err := rv64I31Config().Compile([]byte(rv64I31CoreSource))
	if err != nil {
		t.Fatal(err)
	}
	original := c
	t.Cleanup(func() { _ = original.Close() })

	return c
}

func rv64I31OfficialCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()

	original, err := rv64I31Config().Compile(stagedGCI31CoreBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = original.Close() })
	c := original

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

func TestRV64PublicI31OfficialCoreExecutionAndArtifact(t *testing.T) {
	data := stagedGCI31CoreBytes(t)
	wantSource := [sha256.Size]byte{0x4b, 0xdd, 0x4d, 0x0f, 0x18, 0x6a, 0x2f, 0xd6, 0x17, 0xb4, 0x1a, 0xd4, 0x94, 0x0e, 0x17, 0xf2, 0xc0, 0x41, 0x55, 0x14, 0xeb, 0xc6, 0x36, 0xa5, 0x6e, 0x41, 0x49, 0x6e, 0x8c, 0x39, 0x2a, 0xea}
	if len(data) != 252 || sha256.Sum256(data) != wantSource {
		t.Fatal("official i31 source identity changed")
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			c := rv64I31OfficialCompile(t, artifact)
			view := c.executionView()
			if !rv64I31OfficialCore(view) || !rv64I31Core(view) || railshotValidateCompiled(view) != nil || c.stagedGCI31Product() != stagedGCI31ProductCore || c.stagedFeatures() != CoreFeatureTypedFunctionReferences|CoreFeatureGC {
				t.Fatal("missing exact retained official i31 witness")
			}
			rt := NewRuntime(WithRuntimeConfig(rv64I31Config()))
			defer rt.Close()
			in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			if in.gc != nil {
				t.Fatal("non-allocating official i31 product provisioned a collector")
			}
			_ = c.Close() // the live instance owns an independent executable lease

			for _, sample := range []struct {
				in    int32
				wantU uint32
				wantS int32
			}{
				{0, 0, 0}, {100, 100, 100}, {-1, 0x7fffffff, -1},
				{0x3fffffff, 0x3fffffff, 0x3fffffff},
				{0x40000000, 0x40000000, -0x40000000},
				{int32(0x7fffffff), 0x7fffffff, -1},
				{-0x55555556, 0x2aaaaaaa, 0x2aaaaaaa},
				{-0x35555556, 0x4aaaaaaa, -0x35555556},
			} {
				got, err := in.Invoke("get_u", I32(sample.in))
				if err != nil || !slices.Equal(got, []uint64{uint64(sample.wantU)}) {
					t.Fatalf("get_u(%#x)=%v err=%v want=%#x", uint32(sample.in), got, err, sample.wantU)
				}
				got, err = in.Invoke("get_s", I32(sample.in))
				if err != nil || !slices.Equal(got, []uint64{uint64(uint32(sample.wantS))}) {
					t.Fatalf("get_s(%#x)=%v err=%v want=%#x", uint32(sample.in), got, err, uint32(sample.wantS))
				}
			}
			for _, input := range []uint64{0xcafe123400000001, 0xdeadbeefffffffff} {
				payload := uint32(input) & 0x7fffffff
				wantSigned := uint64(uint32(int32(payload<<1) >> 1))
				for export, want := range map[string]uint64{"get_u": uint64(payload), "get_s": wantSigned} {
					got, err := in.Invoke(export, input)
					if err != nil || !slices.Equal(got, []uint64{want}) {
						t.Fatalf("%s(dirty %#x)=%v err=%v want=%#x", export, input, got, err, want)
					}
				}
			}
			for _, name := range []string{"get_u-null", "get_s-null"} {
				if _, err := in.Invoke(name); err == nil || !strings.Contains(err.Error(), "null reference") {
					t.Fatalf("%s trap = %v", name, err)
				}
			}
			if got, err := in.Invoke("get_globals"); err != nil || !slices.Equal(got, []uint64{2, 3}) {
				t.Fatalf("initial globals=%v err=%v", got, err)
			}
			if _, err := in.Invoke("set_global", 0xcafe1234000004d2); err != nil {
				t.Fatal(err)
			}
			if got, err := in.Invoke("get_globals"); err != nil || !slices.Equal(got, []uint64{2, 1234}) {
				t.Fatalf("updated globals=%v err=%v", got, err)
			}
			raw, err := in.Invoke("new", I32(-1))
			if err != nil || !slices.Equal(raw, []uint64{uint64(uint32(0xffffffff))}) {
				t.Fatalf("raw new(-1)=%#v err=%v", raw, err)
			}
			if dirty, err := in.Invoke("new", 0xcafe123400000001); err != nil || !slices.Equal(dirty, []uint64{3}) {
				t.Fatalf("raw new(dirty)=%#v err=%v", dirty, err)
			}
			values, err := in.InvokeValues(context.Background(), "new", ValueI32(-1))
			if err != nil || len(values) != 1 || values[0].Type() != ValI31Ref || values[0].I31Ref().IsNull() || values[0].I31Ref().Signed() != -1 || values[0].I31Ref().Unsigned() != 0x7fffffff {
				t.Fatalf("typed new(-1)=%v err=%v", values, err)
			}
			if err := in.ReleaseGCRef(ValueOf(ValAnyRef, raw[0]).GCRef()); err == nil {
				t.Fatal("raw i31 immediate was accepted as an opaque GCRef token")
			}
		})
	}
}
