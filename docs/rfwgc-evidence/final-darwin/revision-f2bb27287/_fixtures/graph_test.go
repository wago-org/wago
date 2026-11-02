//go:build darwin && arm64 && !tinygo && !wago_guardpage

package wago

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// The graph is a genuine guest-created 32-node chain rooted in a mutable global.
// Repeated calls replace the previous graph; scalar is a separate compiler control.
func rfw3Module(name string) []byte {
	types := [][]byte{{0x5f, 1, 0x7f, 0}, {0x5f, 2, 0x7f, 0, 0x63, 1, 0}}
	body := []byte{0, 0x41, 7, 0x0b}
	var globals [][]byte
	if name == "scalar" {
		body = []byte{0, 0x41, 7, 0xfb, 0, 0, 0xfb, 2, 0, 0, 0x0b}
	}
	if name == "graph" {
		globals = [][]byte{{0x63, 1, 1, 0xd0, 1, 0x0b}}
		body = []byte{1, 1, 0x63, 1}
		for i := 0; i < 32; i++ {
			body = append(body, 0x41, 7, 0x20, 0, 0xfb, 0, 1, 0x21, 0)
		}
		body = append(body, 0x20, 0, 0x24, 0, 0x23, 0, 0xfb, 2, 1, 0, 0x0b)
	}
	types = append(types, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))
	parts := [][]byte{wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, wasmtest.Vec([]byte{2}))}
	if len(globals) > 0 {
		parts = append(parts, wasmtest.Section(6, wasmtest.Vec(globals...)))
	}
	parts = append(parts, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
	return wasmtest.Module(parts...)
}
func rfw3Config() *RuntimeConfig {
	return NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("inline", false).WithOptimization("gc-lifetime-analyze", true)
}
func rfw3Instance(t testing.TB, name string, on bool) *Instance {
	c, err := Compile(rfw3Config(), rfw3Module(name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	cfg := GCConfig{NurseryBytes: 4096, SurvivorBytes: 4096}
	// No-GC modules have no collecting-frame support: use explicit Off/Off control.
	if on && name != "noGC" {
		f := reflect.ValueOf(&cfg).Elem().FieldByName("NurseryRegionShift")
		if !f.IsValid() {
			t.Fatal("unsupported On")
		}
		f.SetUint(8)
	}
	in, err := Instantiate(c, InstantiateOptions{GC: cfg})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { in.Close() })
	return in
}
func rfw3Invoke(t testing.TB, in *Instance) {
	got, err := in.Invoke("run")
	if err != nil || len(got) != 1 || got[0] != 7 {
		t.Fatalf("oracle: %v %v", got, err)
	}
}
func TestRFWStage3MeasurementGuest(t *testing.T) {
	modes := []bool{false}
	if _, ok := reflect.TypeOf(GCConfig{}).FieldByName("NurseryRegionShift"); ok {
		modes = append(modes, true)
	}
	for _, name := range []string{"noGC", "scalar", "graph"} {
		data := rfw3Module(name)
		t.Logf("fixture %s sha256=%x bytes=%d", name, sha256.Sum256(data), len(data))
		if dir := os.Getenv("RFW_EXPORT"); dir != "" {
			if err := os.WriteFile(filepath.Join(dir, name+".wasm"), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		for _, on := range modes {
			t.Run(fmt.Sprintf("%s/%v", name, on), func(t *testing.T) {
				in := rfw3Instance(t, name, on)
				for i := 0; i < 64; i++ {
					rfw3Invoke(t, in)
				}
				if err := in.CollectGC(); err != nil {
					t.Fatal(err)
				}
				s := in.gc.Stats()
				if name == "graph" && (s.Allocations < 2048 || s.LiveObjects != 32) {
					t.Fatalf("graph coverage: %+v", s)
				}
				if name == "noGC" && s.Allocations != 0 {
					t.Fatal(s)
				}
				raw, _ := json.Marshal(s)
				t.Log(string(raw))
			})
		}
	}
}
