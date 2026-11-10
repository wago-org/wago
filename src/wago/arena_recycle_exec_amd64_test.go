//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"

	backend "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Prefix existing conformance fixtures with enough completed statements to
// cross multiple recycling checkpoints, preserving their original instructions.
func withArenaRecyclePrefix(t *testing.T, data []byte) []byte {
	t.Helper()
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	// Keep const and drop nonadjacent so lowering allocates an operand node.
	prefix := bytes.Repeat([]byte{0x41, 0, 0x01, 0x1a}, 600)
	out := append([]byte(nil), data[:8]...)
	for offset := 8; offset < len(data); {
		id := data[offset]
		offset++
		size, n := binary.Uvarint(data[offset:])
		if n <= 0 {
			t.Fatal("invalid section size")
		}
		offset += n
		section := data[offset : offset+int(size)]
		offset += int(size)
		if id == 10 {
			count, n := binary.Uvarint(section)
			if n <= 0 || int(count) != len(m.Code) {
				t.Fatal("invalid code count")
			}
			section = section[n:]
			bodies := make([][]byte, len(m.Code))
			for i := range m.Code {
				size, n := binary.Uvarint(section)
				if n <= 0 {
					t.Fatal("invalid body size")
				}
				section = section[n:]
				old := section[:int(size)]
				section = section[int(size):]
				locals := int(m.Code[i].LocalDeclBytes)
				body := append([]byte(nil), old[:locals]...)
				body = append(body, prefix...)
				body = append(body, old[locals:]...)
				bodies[i] = append(wasmtest.ULEB(uint32(len(body))), body...)
			}
			section = wasmtest.Vec(bodies...)
		}
		out = append(out, wasmtest.Section(id, section)...)
	}
	return out
}

func TestArenaRecycleBeforeGCAndExceptionCalls(t *testing.T) {
	eh, err := hex.DecodeString(exceptionRootSlotEntryWasm)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		data []byte
		want uint64
		gc   GCConfig
	}{
		{"gc", gcNativeMixedReservationCastModule(), 0, GCConfig{StressNurseryBytes: 8192, VerifyAfterCollect: true}},
		{"exception-gc", eh, 180, GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := withArenaRecyclePrefix(t, tc.data)
			assertGCModuleArenaRecycled(t, data)
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			in, err := Instantiate(compiled, InstantiateOptions{GC: tc.gc})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			got, err := in.Invoke("run")
			if err != nil || len(got) != 1 || got[0] != tc.want {
				t.Fatalf("got %v, %v; want %d", got, err, tc.want)
			}
		})
	}
}

func assertGCModuleArenaRecycled(t *testing.T, data []byte) {
	t.Helper()
	if !compilerTelemetryEnabled || codeProfileEnabled {
		return
	}
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	var stats backend.ModuleStats
	cm, err := backend.CompileModuleWith(m, backend.CompileOptions{
		Workers: 1, Stats: &stats, GCStructHelpers: true, GCArrayHelpers: true, GCTypeSubtypingRefTest: true,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	for i, s := range stats.Funcs {
		if s.Peephole["operand-arena-recycle"] == 0 {
			t.Fatalf("function %d did not recycle operand arena", i)
		}
	}
}
