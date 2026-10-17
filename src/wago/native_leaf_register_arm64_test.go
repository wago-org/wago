//go:build (linux || darwin || windows) && arm64 && !tinygo

package wago

import (
	"encoding/binary"
	"testing"
	"unsafe"

	wruntime "github.com/wago-org/wago/src/core/runtime"
)

//go:norace
//go:noinline
func nativeImportIdentity(v int32) int32 { return v }

func TestNativeLeafRegisterImportPreservesContextAndCanonicalResult(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains Go callback execution")
	}
	modules := []struct{ name, wat string }{
		{"aliases", `(module
   (import "env" "a" (func $a (param i32) (result i32)))
   (import "env" "b" (func $b (param i32) (result i32)))
   (func (export "run") (param i32) (result i64 i32)
    (i64.const 0x123456789abcdef0)
    (call $b (call $a (local.get 0)))))`},
		{"memory", `(module
   (import "env" "a" (func $a (param i32) (result i32)))
   (memory 1 1)
   (global $g (mut i32) (i32.const 17))
   (func (export "run") (param i32) (result i64 i32)
    (i64.const 0x123456789abcdef0)
    (i32.store (i32.const 0) (local.get 0))
    (call $a (i32.load (i32.const 0)))))`},
	}
	for _, m := range modules {
		t.Run(m.name, func(t *testing.T) {
			for _, enabled := range []bool{false, true} {
				c, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithOptimization("native-leaf-host", enabled).Compile(watToWasm(t, m.wat))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				for artifact, code := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
					in, err := Instantiate(code, InstantiateOptions{Imports: testImports("env.a", nativeImportIdentity, "env.b", nativeImportIdentity)})
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					ptr := in.jm.CaptureInstanceContext().ImportDispatch
					cells := unsafe.Slice((*byte)(offHeapPtr(ptr)), len(in.syncHosts)*wruntime.ImportDispatchEntryBytes)
					for offset := 0; offset < len(cells); offset += wruntime.ImportDispatchEntryBytes {
						word := binary.LittleEndian.Uint64(cells[offset+wruntime.ImportDispatchCallerContextOffset:])
						if tagged := word&wruntime.ImportDispatchCallerNativeLeafTag != 0; tagged != (enabled && artifact == 0) {
							t.Fatalf("identity=%v enabled=%v artifact=%d", tagged, enabled, artifact)
						}
						if word&^wruntime.ImportDispatchCallerTagMask != uint64(in.nativeContext) {
							t.Fatal("caller context changed")
						}
					}
					for _, v := range []uint64{0, 1, 0x7fffffff, 0x80000000, 0xffffffff, 0xffffffff00000007} {
						got, err := in.Invoke("run", v)
						if err != nil || len(got) != 2 || got[0] != 0x123456789abcdef0 || got[1] != uint64(uint32(v)) {
							t.Fatalf("run(%x)=%x err=%v", v, got, err)
						}
					}
				}
			}
		})
	}
}
