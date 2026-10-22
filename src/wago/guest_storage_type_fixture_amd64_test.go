//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"testing"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Type 1 has an unrelated i64 field layout. A host result declared as type 0
// must reject its token before native code resumes.
func guestTypeIsolationModule() []byte {
	entry := append(wasmtest.Name("host"), wasmtest.Name("get")...)
	entry = append(entry, 0x00, 0x03)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},       // type 0: struct with i32 field
			[]byte{0x5f, 0x01, 0x7e, 0x00},       // type 1: struct with i64 field
			[]byte{0x60, 0x00, 0x01, 0x6e},       // () -> anyref
			[]byte{0x60, 0x00, 0x01, 0x64, 0x00}, // () -> (ref 0)
			[]byte{0x60, 0x00, 0x01, 0x7f},       // () -> i32
		)),
		wasmtest.Section(2, wasmtest.Vec(entry)),
		wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{2}, []byte{4})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("parent", 0, 1),
			wasmtest.ExportEntry("child", 0, 2),
			wasmtest.ExportEntry("host", 0, 3),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x0a, 0xfb, 0x00, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x42, 0x14, 0xfb, 0x00, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x10, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}),
		)),
	)
}

func guestTypeIsolationToken(t *testing.T, in *Instance, name string) uint64 {
	t.Helper()
	values, err := in.Invoke(name)
	if err != nil || len(values) != 1 || values[0] == 0 {
		t.Fatalf("%s token = %v, %v", name, values, err)
	}
	t.Cleanup(func() {
		if err := in.ReleaseGCRef(GCRef{token: values[0]}); err != nil {
			t.Errorf("release %s token: %v", name, err)
		}
	})
	return values[0]
}
