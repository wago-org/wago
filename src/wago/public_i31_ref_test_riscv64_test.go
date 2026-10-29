//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"
	"testing"
)

func rv64I31OfficialRefTestCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()

	original, err := rv64I31Config().Compile(stagedGCI31RefTestGateBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = original.Close() })
	c := original

	return c
}

func TestRV64PublicI31OfficialRefTestExecutionAndArtifact(t *testing.T) {
	data := stagedGCI31RefTestGateBytes(t)
	wantSource := [sha256.Size]byte{0x15, 0xae, 0x51, 0xeb, 0x55, 0x7d, 0xb9, 0x16, 0x94, 0xfc, 0x8c, 0xb2, 0xe2, 0xca, 0x14, 0x87, 0x92, 0xee, 0xc4, 0xae, 0x2b, 0x52, 0x4c, 0x61, 0x0e, 0xdf, 0x80, 0x05, 0xa0, 0x48, 0x37, 0xec}
	if len(data) != 255 || sha256.Sum256(data) != wantSource {
		t.Fatal("official i31 ref.test source identity changed")
	}
	for _, artifact := range []bool{false} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			c := rv64I31OfficialRefTestCompile(t, artifact)
			view := c.executionView()
			wantProduct, wantStaged := stagedGCI31ProductRefTest, CoreFeatureTypedFunctionReferences|CoreFeatureGC
			if artifact {
				wantProduct, wantStaged = stagedGCI31ProductCore, CoreFeatureGC
			}
			if !rv64I31OfficialRefTest(view) || !rv64I31Core(view) || railshotValidateCompiled(view) != nil || c.stagedGCI31Product() != wantProduct || c.stagedFeatures() != wantStaged {
				t.Fatal("missing exact retained official i31 ref.test witness")
			}
			rt := NewRuntime(WithRuntimeConfig(rv64I31Config()))
			defer rt.Close()
			in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			defer in.Close()
			if in.gc != nil {
				t.Fatal("non-allocating official i31 ref.test provisioned a collector")
			}
			invoke := func(name string, want uint64, args ...uint64) {
				t.Helper()
				got, err := in.Invoke(name, args...)
				if err != nil || !slices.Equal(got, []uint64{want}) {
					t.Fatalf("%s%v=%v err=%v want=[%d]", name, args, got, err, want)
				}
			}
			invoke("null-data", 1)
			invoke("null-i31", 1)
			invoke("i31", 2, 0xdeadbeef8000002a)
			invoke("any", 2, 0xcafebabe00000007)
			invoke("eq", 2, 0xfeedface00000007)
			invoke("struct", 0, 0xffffffff00000007)
			invoke("array", 0, 0x1234567800000007)
			_ = c.Close() // A live instance retains its executable lease.
			invoke("i31", 2, 0xaaaaaaaa80000000)
		})
	}
}
