//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"slices"
	"testing"
)

func rv64GCStructMutationCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()

	original, err := rv64I31Config().Compile(stagedGCStructMutationBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = original.Close() })

	return original

}

func TestRV64PublicGCStructMutationExecutionAndArtifact(t *testing.T) {
	data := stagedGCStructMutationBytes(t)
	wantSource := [sha256.Size]byte{0xe9, 0xf7, 0xa7, 0xec, 0x88, 0xc5, 0x66, 0x84, 0xad, 0x5b, 0x96, 0xe2, 0xa5, 0x47, 0x17, 0x65, 0xab, 0x28, 0x35, 0xdd, 0xea, 0x14, 0x06, 0x90, 0x06, 0xda, 0x51, 0xa9, 0x6e, 0xd5, 0xe8, 0x91}
	if len(data) != 106 || sha256.Sum256(data) != wantSource {
		t.Fatal("gc struct mutation source identity changed")
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64GCStructMutationCompile(t, artifact)
			view := compiled.executionView()
			exact, validation := rv64SourceContractValid(view), railshotValidateCompiled(view)
			if !exact || validation != nil {
				t.Fatalf("missing exact retained gc struct mutation witness: exact=%t validation=%v product=%v staged=%v required=%v code=%d/%x entries=%v/%v", exact, validation, view.stagedGCStructProduct(), view.stagedFeatures(), view.requiredFeatures, len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry)
			}
			for _, profile := range []struct {
				name                      string
				config                    GCConfig
				requireCollectsOnAllocate bool
			}{{name: "default-gc"}, {name: "stress-gc", config: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true, StressBarriers: true}, requireCollectsOnAllocate: true}} {
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
						t.Fatal("allocating gc struct mutation module has no collector")
					}
					if profile.requireCollectsOnAllocate && !in.gc.CollectsOnAllocation() {
						t.Fatal("stress collector does not collect on allocation")
					}
					if got, err := in.Invoke("get"); err != nil || !slices.Equal(got, []uint64{0}) {
						t.Fatalf("get()=%v,%v want [0]", got, err)
					}
					for i, raw := range []uint64{0, 1, 0xffffffff, 0xdeadbeef0000002a, 0xffffffffffffffff} {
						want := uint64(uint32(raw))
						got, err := in.Invoke("set", raw)
						if err != nil || !slices.Equal(got, []uint64{want}) {
							t.Fatalf("set case %d raw=%#x got=%v,%v want=[%#x]", i, raw, got, err, want)
						}
					}
					if err = in.CollectGC(); err != nil {
						t.Fatal(err)
					}
				})
			}
		})
	}
}
