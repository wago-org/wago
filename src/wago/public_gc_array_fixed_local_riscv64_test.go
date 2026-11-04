//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"slices"
	"testing"
)

func rv64GCArrayFixedLocalCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()

	original, err := rv64I31Config().Compile(stagedGCArrayNumericFixedLocalBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = original.Close() })
	c := original

	return c
}

func TestRV64PublicGCArrayFixedLocalExecutionAndArtifact(t *testing.T) {
	data := stagedGCArrayNumericFixedLocalBytes(t)
	wantSource := [sha256.Size]byte{0x41, 0x26, 0x8d, 0xe7, 0xa6, 0xcb, 0x79, 0xd2, 0x2f, 0xc7, 0x5f, 0x69, 0x07, 0xc8, 0xe8, 0xae, 0xe8, 0xf8, 0x70, 0x25, 0x22, 0x20, 0x91, 0xb7, 0x62, 0x1c, 0x4f, 0xa5, 0x93, 0x2c, 0x46, 0x45}
	if len(data) != 139 || sha256.Sum256(data) != wantSource {
		t.Fatal("gc array fixed local source identity changed")
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64GCArrayFixedLocalCompile(t, artifact)
			view := compiled.executionView()
			if err := railshotValidateCompiled(view); !rv64SourceContractValid(view) || err != nil {
				t.Fatalf("missing exact retained gc array fixed local witness: validation=%v products=%v/%v code=%d/%x entries=%v/%v", err, view.stagedGCStructProduct(), view.stagedGCArrayProduct(), len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry)
			}
			for _, profile := range []struct {
				name   string
				config GCConfig
			}{
				{name: "default-gc"},
				{name: "stress-gc", config: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}},
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
					if in.gc == nil || profile.name == "stress-gc" && !in.gc.CollectsOnAllocation() {
						t.Fatal("fixed local collector profile unavailable")
					}
					for _, tc := range []struct {
						index uint64
						want  uint64
					}{{0xdddd000000000000, 0x80000001}, {0xdddd000000000001, 0x92345678}, {0xdddd000000000002, 0xf0000003}} {
						if got, err := in.Invoke("get", 0xaaaa000080000001, 0xbbbb000092345678, 0xcccc0000f0000003, tc.index); err != nil || !slices.Equal(got, []uint64{tc.want}) {
							t.Fatalf("get(%#x)=%v err=%v", tc.index, got, err)
						}
					}
					if got, err := in.Invoke("set_get", 0xaaaa000000000001, 0xbbbb000000000002, 0xcccc000000000003, 0xdddd000000000002, 0xeeee000087654321); err != nil || !slices.Equal(got, []uint64{0x87654321}) {
						t.Fatalf("set_get()=%v err=%v", got, err)
					}
					if got, err := in.Invoke("len", 0xaaaa000000000001, 0xbbbb000000000002, 0xcccc000000000003); err != nil || !slices.Equal(got, []uint64{3}) {
						t.Fatalf("len()=%v err=%v", got, err)
					}
					if _, err = in.Invoke("get", 1, 2, 3, 3); err == nil {
						t.Fatal("out-of-bounds get succeeded")
					}
					if _, err = in.Invoke("set_get", 1, 2, 3, 3, 9); err == nil {
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
