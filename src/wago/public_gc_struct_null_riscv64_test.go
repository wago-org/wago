//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

const rv64PublicGCStructNullHex = "0061736d01000000018a80808000025f027f007f0160000003838080800002010107a580808000020f7374727563742e6765742d6e756c6c00000f7374727563742e7365742d6e756c6c00010aa480808000028c80808000010163002000fb0200011a0b8d808080000101630020004100fb0500010b"

func rv64PublicGCStructNullBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64PublicGCStructNullHex)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func rv64PublicGCStructNullCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()

	original, err := rv64I31Config().Compile(rv64PublicGCStructNullBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = original.Close() })

	return original

}

func rv64AssertGCStructNullTrap(t *testing.T, in *Instance, name string) {
	t.Helper()
	got, err := in.Invoke(name)
	if got != nil || err == nil {
		t.Fatalf("trapping %s=%v,%v", name, got, err)
	}
	var trap *coreruntime.TrapError
	if !errors.As(err, &trap) || trap.Code != coreruntime.TrapNullReference {
		t.Fatalf("trapping %s returned %v, want null-reference trap", name, err)
	}
}

func TestRV64PublicGCStructNullExecutionAndArtifact(t *testing.T) {
	data := rv64PublicGCStructNullBytes(t)
	wantSource := [sha256.Size]byte{0xb0, 0x19, 0x11, 0x99, 0x0b, 0xd0, 0x67, 0x8f, 0x48, 0x4a, 0xfa, 0xe1, 0xa2, 0x8d, 0xec, 0x4b, 0x44, 0xc3, 0x73, 0x08, 0xdb, 0x5f, 0x5f, 0xdc, 0xe7, 0xaa, 0x30, 0xb0, 0x02, 0x75, 0xfd, 0x31}
	if len(data) != 118 || sha256.Sum256(data) != wantSource {
		t.Fatal("gc/struct null source identity changed")
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64PublicGCStructNullCompile(t, artifact)
			view := compiled.executionView()
			if err := railshotValidateCompiled(view); !rv64SourceContractValid(view) || err != nil {
				t.Fatalf("missing exact gc/struct null witness: validation=%v product=%v staged=%v required=%v code=%d/%x entries=%v/%v funcs=%+v types=%+v descs=%+v", err, view.stagedGCStructProduct(), view.stagedFeatures(), view.requiredFeatures, len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry, view.Funcs, view.Types, view.GCTypeDescs)
			}
			if params, results, err := railshotImportedCallSlots(view, 0, 0); err != nil || params != 0 || results != 0 {
				t.Fatalf("gc/struct null call arena=%d/%d,%v, want 0/0", params, results, err)
			}
			for _, profile := range []struct {
				name   string
				config GCConfig
			}{{name: "default-gc"}, {name: "stress-gc", config: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true, StressBarriers: true}}} {
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
						t.Fatalf("gc/struct null runtime provisioning: collector=%v argument arena=%d", in.gc != nil, len(in.serArgs))
					}
					rv64AssertGCStructNullTrap(t, in, "struct.get-null")
					rv64AssertGCStructNullTrap(t, in, "struct.set-null")
				})
			}
		})
	}
}
