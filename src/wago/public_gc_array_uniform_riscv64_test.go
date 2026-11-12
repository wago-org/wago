//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"slices"
	"testing"
)

func rv64GCArrayUniformCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()

	original, err := rv64I31Config().Compile(stagedGCArrayNumericUniformBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = original.Close() })
	c := original

	return c
}

func TestRV64PublicGCArrayUniformExecutionAndArtifact(t *testing.T) {
	data := stagedGCArrayNumericUniformBytes(t)
	wantSource := [sha256.Size]byte{0x5b, 0x42, 0x1e, 0x8a, 0x65, 0xc9, 0x97, 0xbe, 0x7b, 0x20, 0xbd, 0x18, 0x8c, 0x5a, 0x1f, 0x1b, 0x16, 0x77, 0x16, 0xb9, 0x3a, 0xbb, 0x55, 0xb4, 0x6a, 0xf8, 0xf4, 0x59, 0x59, 0xc4, 0x51, 0x5a}
	if len(data) != 127 || sha256.Sum256(data) != wantSource {
		t.Fatal("gc array uniform source identity changed")
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64GCArrayUniformCompile(t, artifact)
			view := compiled.executionView()
			validationErr := railshotValidateCompiled(view)
			if !rv64SourceContractValid(view) || validationErr != nil {
				t.Fatalf("missing exact retained gc array uniform witness: validation=%v products=%v/%v staged=%v required=%v code=%d/%x entries=%v/%v funcs=%+v types=%+v descs=%+v", validationErr, view.stagedGCStructProduct(), view.stagedGCArrayProduct(), view.stagedFeatures(), view.requiredFeatures, len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry, view.Funcs, view.Types, view.GCTypeDescs)
			}
			for _, profile := range []struct {
				name                      string
				config                    GCConfig
				requireCollectsOnAllocate bool
			}{
				{name: "default-gc"},
				{name: "stress-gc", config: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}, requireCollectsOnAllocate: true},
			} {
				t.Run(profile.name, func(t *testing.T) {
					rt := NewRuntime()
					defer rt.Close()
					mod, err := rt.Module(compiled)
					if err != nil {
						t.Fatal(err)
					}
					in, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports()), WithGC(profile.config))
					mod.Close()
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					if in.gc == nil {
						t.Fatal("allocating gc array uniform module has no collector")
					}
					if profile.requireCollectsOnAllocate && !in.gc.CollectsOnAllocation() {
						t.Fatal("stress collector does not collect on allocation")
					}
					if got, err := in.Invoke("get", 0xaaaa000092345678, 0xbbbb000000000003, 0xcccc000000000002); err != nil || !slices.Equal(got, []uint64{0x92345678}) {
						t.Fatalf("get()=%v err=%v want=[0x92345678]", got, err)
					}
					if got, err := in.Invoke("set_get", 0xaaaa000011111111, 0xbbbb000000000003, 0xcccc000000000001, 0xdddd000087654321); err != nil || !slices.Equal(got, []uint64{0x87654321}) {
						t.Fatalf("set_get()=%v err=%v want=[0x87654321]", got, err)
					}
					if got, err := in.Invoke("len", 0xaaaa000011111111, 0xbbbb000000000007); err != nil || !slices.Equal(got, []uint64{7}) {
						t.Fatalf("len()=%v err=%v want=[7]", got, err)
					}
					if _, err = in.Invoke("get", 1, 3, 3); err == nil {
						t.Fatal("out-of-bounds get succeeded")
					}
					if _, err = in.Invoke("set_get", 1, 3, 3, 9); err == nil {
						t.Fatal("out-of-bounds set succeeded")
					}
					if err = in.CollectGC(); err != nil {
						t.Fatal(err)
					}
				})
			}
		})
	}
}
