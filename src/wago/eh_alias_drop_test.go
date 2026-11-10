//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// A catch_all_ref result escapes through local.tee. Dropping the operand
// alias must leave the exception record (and its GC payload) alive for rethrow.
func TestExceptionAliasDrop(t *testing.T) {
	for _, gcPayload := range []bool{false, true} {
		name := "scalar"
		payloadType := wasm.I32
		throw := []byte{0x41, 42}
		types := [][]byte{wasmtest.FuncType([]wasm.ValType{payloadType}, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})}
		if gcPayload {
			name = "gc_payload"
			// struct $s (field i32), tag (param (ref $s)).
			types = [][]byte{{0x5f, 1, 0x7f, 0}, {0x60, 1, 0x64, 0, 0}, types[1], {0x60, 0, 1, 0x64, 0}}
			throw = append(throw, 0xfb, 0, 0) // struct.new $s
		}
		t.Run(name, func(t *testing.T) {
			body := []byte{
				0x02, 0x69, // block (result exnref)
				0x1f, 0x40, 1, byte(wasm.CatchAllRef), 0,
			}
			body = append(body, throw...)
			body = append(body, 0x08, 0, 0x0b, 0x00, 0x0b, // throw; end try; unreachable; end block
				0x22, 0, 0x1a) // local.tee $e; drop
			if gcPayload {
				// Force collections while the exception local is the only payload owner.
				for i := 0; i < 64; i++ {
					body = append(body, 0x41, 0, 0xfb, 0, 0, 0x1a)
				}
			}
			resultType := byte(0x7f)
			if gcPayload {
				resultType = 3
			} // type 3: (result (ref $s))
			body = append(body, 0x02, resultType)
			body = append(body,
				0x1f, 0x40, 1, 0, 0, 0,
				0x20, 0, 0x0a, // local.get $e; throw_ref
				0x0b, 0x00, 0x0b)
			if gcPayload {
				body = append(body, 0xfb, 2, 0, 0)
			} // struct.get $s 0
			body = append(body, 0x0b)
			fn := append([]byte{1, 1, 0x69}, body...) // one exnref local
			tagType, runType := byte(0), byte(1)
			if gcPayload {
				tagType, runType = 1, 2
			}
			data := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(types...)),
				wasmtest.Section(3, wasmtest.Vec([]byte{runType})),
				wasmtest.Section(13, wasmtest.Vec([]byte{0, tagType})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(fn))), fn...))),
			)
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			opts := InstantiateOptions{}
			if gcPayload {
				opts.GC = GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}
			}
			in, err := Instantiate(c, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for i := 0; i < 3; i++ {
				got, err := in.Invoke("run")
				if err != nil || len(got) != 1 || got[0] != 42 {
					t.Fatalf("run %d = %v, %v; want 42", i, got, err)
				}
			}
			if gcPayload && in.gc.Stats().FullCollections == 0 {
				t.Fatal("GC payload test ran without a collection")
			}
		})
	}
}
