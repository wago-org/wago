package wasm

import (
	"os"
	"path/filepath"
	"testing"
)

// checkInvariants asserts internal consistency of a decoded module.
func checkInvariants(t *testing.T, name string, m *Module) {
	t.Helper()
	if len(m.Code) != len(m.Functions) {
		t.Errorf("%s: code/function count mismatch: %d vs %d", name, len(m.Code), len(m.Functions))
	}
	for i, ti := range m.Functions {
		if int(ti) >= len(m.Types) {
			t.Errorf("%s: function %d type index %d out of range (%d types)", name, i, ti, len(m.Types))
		}
	}
	for i := range m.Imports {
		if m.Imports[i].Kind == ExternFunc && int(m.Imports[i].TypeIndex) >= len(m.Types) {
			t.Errorf("%s: import %d func type index out of range", name, i)
		}
	}
	totalFuncs := uint32(m.ImportedFuncCount() + len(m.Functions))
	if m.Start != nil && *m.Start >= totalFuncs {
		t.Errorf("%s: start func %d out of range (%d funcs)", name, *m.Start, totalFuncs)
	}
}

func TestDecodeRealModules(t *testing.T) {
	files, err := filepath.Glob("../../../../warp/wasm_examples/*.wasm")
	if err != nil {
		t.Fatal(err)
	}
	extra, _ := filepath.Glob("../../../../warp/scripts/*.wasm")
	files = append(files, extra...)
	if len(files) == 0 {
		t.Skip("no wasm_examples found")
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Base(f)
		m, err := Decode(data)
		if err != nil {
			t.Errorf("%s (%d bytes): decode failed: %v", name, len(data), err)
			continue
		}
		checkInvariants(t, name, m)
		t.Logf("%-32s %7dB  types=%d imports=%d funcs=%d globals=%d exports=%d elems=%d data=%d custom=%d",
			name, len(data), len(m.Types), len(m.Imports), len(m.Functions),
			len(m.Globals), len(m.Exports), len(m.Elements), len(m.Data), len(m.Customs))
	}
}

func TestDecodeMalformed(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"bad magic", []byte{0x00, 0x61, 0x73, 0x6E, 1, 0, 0, 0}},
		{"truncated version", []byte{0x00, 0x61, 0x73, 0x6D, 1}},
		{"bad version", []byte{0x00, 0x61, 0x73, 0x6D, 2, 0, 0, 0}},
		{"section size past end", []byte{0x00, 0x61, 0x73, 0x6D, 1, 0, 0, 0, 0x01, 0x7F}}, // type section, size 127, no body
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Decode(c.data); err == nil {
				t.Fatalf("expected error for %s", c.name)
			}
		})
	}
}

// A tiny hand-built module: (func (param i32) (result i32) local.get 0) exported as "id".
func TestDecodeTinyModule(t *testing.T) {
	mod := []byte{
		0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00, // header
		0x01, 0x06, 0x01, 0x60, 0x01, 0x7F, 0x01, 0x7F, // type: (i32)->(i32)
		0x03, 0x02, 0x01, 0x00, // function: 1 func, type 0
		0x07, 0x06, 0x01, 0x02, 0x69, 0x64, 0x00, 0x00, // export "id" func 0
		0x0A, 0x06, 0x01, 0x04, 0x00, 0x20, 0x00, 0x0B, // code: 1 body, locals=0, local.get 0, end
	}
	m, err := Decode(mod)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Types) != 1 || len(m.Functions) != 1 || len(m.Exports) != 1 {
		t.Fatalf("unexpected structure: %+v", m)
	}
	if m.Exports[0].Name != "id" || m.Exports[0].Kind != ExternFunc {
		t.Fatalf("bad export: %+v", m.Exports[0])
	}
	ft := m.Types[m.Functions[0]]
	if len(ft.Params) != 1 || ft.Params[0] != I32 || len(ft.Results) != 1 || ft.Results[0] != I32 {
		t.Fatalf("bad type: %+v", ft)
	}
	if len(m.Code[0].Body) == 0 || m.Code[0].Body[len(m.Code[0].Body)-1] != 0x0B {
		t.Fatalf("body should end with `end`: %x", m.Code[0].Body)
	}
}
