//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"slices"
	"testing"
)

func rv64GCStructGetCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()

	original, err := rv64I31Config().Compile(stagedGCStructGetOnlyBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = original.Close() })
	c := original

	return c
}

func TestRV64PublicGCStructGetExecutionAndArtifact(t *testing.T) {
	data := stagedGCStructGetOnlyBytes(t)
	wantSource := [sha256.Size]byte{0xf5, 0xfc, 0x57, 0xa9, 0xa6, 0xb9, 0x59, 0xa1, 0xa6, 0x89, 0x38, 0x5c, 0xb7, 0x90, 0x50, 0xb6, 0x99, 0x8c, 0x86, 0x7c, 0x61, 0xea, 0xfd, 0x65, 0xff, 0x03, 0xb2, 0xd5, 0x7d, 0x12, 0x8f, 0xcf}
	if len(data) != 65 || sha256.Sum256(data) != wantSource {
		t.Fatal("gc struct.get source identity changed")
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64GCStructGetCompile(t, artifact)
			view := compiled.executionView()
			if !rv64SourceContractValid(view) || railshotValidateCompiled(view) != nil {
				t.Fatalf("missing exact retained gc struct.get witness: product=%v staged=%v required=%v code=%d/%x entries=%v/%v funcs=%+v types=%+v descs=%+v flags=%t/%t/%t/%t/%t", view.stagedGCStructProduct(), view.stagedFeatures(), view.requiredFeatures, len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry, view.Funcs, view.Types, view.GCTypeDescs, view.nativeContextTrapABI, view.nativeTableContextABI, view.nativeTableReferenceABI, view.registerABIDisabled, view.needsFuncRefContextHeader)
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
						t.Fatal("allocating gc struct.get module has no collector")
					}
					if profile.requireCollectsOnAllocate && !in.gc.CollectsOnAllocation() {
						t.Fatal("stress collector does not collect on allocation")
					}
					got, err := in.Invoke("get")
					if err != nil || !slices.Equal(got, []uint64{0}) {
						t.Fatalf("get()=%v err=%v want=[0]", got, err)
					}
					if err = in.CollectGC(); err != nil {
						t.Fatal(err)
					}
				})
			}
		})
	}
}
