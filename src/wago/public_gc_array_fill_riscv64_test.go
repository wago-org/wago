//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

const rv64PublicGCArrayFillHex = "0061736d01000000019580808000055e78005e780160017f017f60000060037f7f7f000384808080000302030406958080800002640000410a410cfb06000b640101410cfb07010b07b080808000030d61727261795f6765745f6e746800000f61727261795f66696c6c2d6e756c6c00010a61727261795f66696c6c00020ab3808080000389808080000023012000fb0d010b8d8080800000d001410041004100fb10010b8d80808000002301200020012002fb10010b"

func rv64PublicGCArrayFillBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64PublicGCArrayFillHex)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func rv64PublicGCArrayFillCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()

	original, err := rv64I31Config().Compile(rv64PublicGCArrayFillBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = original.Close() })

	return original

}

func rv64AssertGCArrayFillTrap(t *testing.T, in *Instance, name string, code coreruntime.TrapCode, args ...uint64) {
	t.Helper()
	got, err := in.Invoke(name, args...)
	if got != nil || err == nil {
		t.Fatalf("trapping %s%v=%v,%v", name, args, got, err)
	}
	var trap *coreruntime.TrapError
	if !errors.As(err, &trap) || trap.Code != code {
		t.Fatalf("trapping %s%v returned %v, want trap %v", name, args, err, code)
	}
}

func TestRV64PublicGCArrayFillExecutionAndArtifact(t *testing.T) {
	data := rv64PublicGCArrayFillBytes(t)
	wantSource := [sha256.Size]byte{0x08, 0x93, 0xca, 0xa7, 0xae, 0x7a, 0xb2, 0xd8, 0x70, 0x32, 0x9d, 0xa9, 0x69, 0x7d, 0x40, 0x5a, 0x51, 0x59, 0x2c, 0xb3, 0xec, 0xc1, 0xb4, 0xb8, 0x33, 0x78, 0x0e, 0xf9, 0xb2, 0x58, 0x01, 0x69}
	if len(data) != 183 || sha256.Sum256(data) != wantSource {
		t.Fatal("array.fill source identity changed")
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64PublicGCArrayFillCompile(t, artifact)
			view := compiled.executionView()
			if err := railshotValidateCompiled(view); !rv64SourceContractValid(view) || err != nil {
				t.Fatalf("missing exact array.fill witness: validation=%v product=%v code=%d/%x entries=%v/%v globals=%#v", err, view.stagedGCArrayProduct(), len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry, view.memoryDir.gcArrayGlobals)
			}
			if params, results, err := railshotImportedCallSlots(view, 3, 1); err != nil || params != 3 || results != 1 {
				t.Fatalf("array.fill call arena=%d/%d,%v, want 3/1", params, results, err)
			}
			for _, profile := range []struct {
				name   string
				config GCConfig
			}{{name: "default-gc"}, {name: "stress-gc", config: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 224, ForceMajorEveryMinor: true, VerifyAfterCollect: true, StressBarriers: true}}} {
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
					if len(in.serArgs) < 3*8 {
						t.Fatalf("array.fill argument arena=%d bytes, want at least 24", len(in.serArgs))
					}
					rv64AssertGCArrayFillTrap(t, in, "array_fill-null", coreruntime.TrapNullReference)
					rv64AssertGCArrayFillTrap(t, in, "array_fill", coreruntime.TrapBuiltin, 13, 0, 0)
					rv64AssertGCArrayFillTrap(t, in, "array_fill", coreruntime.TrapBuiltin, 0, 0, 13)
					if got, err := in.Invoke("array_fill", 0xffff00000000000c, 0, 0xffff000000000000); err != nil || len(got) != 0 {
						t.Fatalf("dirty-upper zero-length fill=%v,%v", got, err)
					}
					for _, index := range []uint64{0, 5, 11} {
						if got, err := in.Invoke("array_get_nth", index); err != nil || !slices.Equal(got, []uint64{0}) {
							t.Fatalf("array_get_nth(%d)=%v,%v want 0", index, got, err)
						}
					}
					rv64AssertGCArrayFillTrap(t, in, "array_get_nth", coreruntime.TrapBuiltin, 12)
					if got, err := in.Invoke("array_fill", 0xaaaa000000000002, 0xbbbb00000000010b, 0xcccc000000000002); err != nil || len(got) != 0 {
						t.Fatalf("dirty-upper array.fill=%v,%v", got, err)
					}
					for index, want := range map[uint64]uint64{1: 0, 2: 11, 3: 11, 4: 0} {
						if got, err := in.Invoke("array_get_nth", index); err != nil || !slices.Equal(got, []uint64{want}) {
							t.Fatalf("array_get_nth(%d)=%v,%v want %d", index, got, err, want)
						}
					}
					if err = in.gc.CollectFull(nil); err != nil {
						t.Fatal(err)
					}
					if live := in.gc.Stats().LiveObjects; live != 2 {
						t.Fatalf("live objects=%d, want two rooted arrays", live)
					}
				})
			}
		})
	}
}
