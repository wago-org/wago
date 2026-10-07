//go:build (linux || darwin) && (arm64 || amd64) && !tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"runtime"
	"testing"
	"unsafe"

	wruntime "github.com/wago-org/wago/src/core/runtime"
)

// One compiled import must support both Go and cross-instance Wasm bindings.
// Serialized artifacts deliberately lose the compile-only tagged-ABI marker.
func TestDirectGoImportBindingAndArtifactFallback(t *testing.T) {
	// Artifact round-trips require explicit bounds checks, including guard-page builds.
	cfg := goHostSegmentConfig().WithBoundsChecks(BoundsChecksExplicit)
	module := watToWasm(t, `(module
 (import "env" "step" (func $step (param i32) (result i32)))
 (func (export "run") (param i32) (result i32) local.get 0 call $step))`)
	producerCode, err := Compile(cfg, watToWasm(t, `(module
 (func (export "step") (param i32) (result i32)
  local.get 0 i32.const 7 i32.add))`))
	if err != nil {
		t.Fatal(err)
	}
	defer producerCode.Close()
	producer, err := Instantiate(producerCode, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	exported, err := producer.ExportedFunc("step")
	if err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{true, false} {
		name := "direct"
		if !enabled {
			name = "rollback"
		}
		t.Run(name, func(t *testing.T) {
			fresh, err := Compile(cfg.WithOptimization("direct-go-host-import", enabled), module)
			if err != nil {
				t.Fatal(err)
			}
			defer fresh.Close()
			wantCapability := runtime.GOARCH == "arm64" || enabled
			if fresh.supportsGoHostDispatchTag() != wantCapability {
				t.Fatalf("fresh dispatch capability=%t, want %t", fresh.supportsGoHostDispatchTag(), wantCapability)
			}
			var artifact bytes.Buffer
			if _, err := fresh.WriteTo(&artifact); err != nil {
				t.Fatal(err)
			}
			decoded := new(Compiled)
			if _, err := decoded.ReadFrom(bytes.NewReader(artifact.Bytes())); err != nil {
				t.Fatal(err)
			}
			defer decoded.Close()
			if decoded.supportsGoHostDispatchTag() {
				t.Fatal("artifact restored a compile-only dispatch capability")
			}
			for _, code := range []*Compiled{fresh, decoded} {
				for _, kind := range []string{"typed", "HostCall", "Caller", "Wasm"} {
					calls := 0
					var callback any = func(v int32) int32 { calls++; return v + 7 }
					switch kind {
					case "HostCall":
						callback = func(call HostCall) { calls++; call.SetI32(0, call.I32(0)+7) }
					case "Caller":
						callback = func(caller Caller, call HostCall) {
							if !caller.valid() {
								t.Fatal("inactive Caller")
							}
							calls++
							call.SetI32(0, call.I32(0)+7)
						}
					case "Wasm":
						callback = exported
					}
					in, err := Instantiate(code, InstantiateOptions{Imports: testImports("env.step", callback)})
					if err != nil {
						t.Fatal(err)
					}
					dispatch := in.jm.CaptureInstanceContext().ImportDispatch
					entry := unsafe.Slice((*byte)(offHeapPtr(dispatch)), wruntime.ImportDispatchEntryBytes)
					word := binary.LittleEndian.Uint64(entry[wruntime.ImportDispatchCallerContextOffset:])
					wantTag := code == fresh && wantCapability && kind != "Wasm"
					if tagged := word&wruntime.ImportDispatchCallerGoHostTag != 0; tagged != wantTag {
						t.Fatalf("%s fresh=%t: Go tag=%t, want %t", kind, code == fresh, tagged, wantTag)
					}
					if prepared := in.eng.PreparedScalarHost(); prepared != nil {
						wantPrivate := detachedNumericHostEnabled && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64" && armDetachedNumericEnabled) && wantTag
						if prepared.DetachedNumericContext() != wantPrivate {
							t.Fatalf("%s fresh=%t: detached=%t, want %t", kind, code == fresh, prepared.DetachedNumericContext(), wantPrivate)
						}
					}
					for _, input := range []uint64{0, 41, 0xfffffffe} {
						got, err := in.Invoke("run", input)
						want := uint64(uint32(input) + 7)
						if err != nil || len(got) != 1 || got[0] != want {
							t.Fatalf("%s fresh=%t input=%x: %v, %v; want %x", kind, code == fresh, input, got, err, want)
						}
					}
					if kind != "Wasm" && calls != 3 {
						t.Fatalf("%s callbacks=%d; want 3", kind, calls)
					}
					if err := in.Close(); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}
