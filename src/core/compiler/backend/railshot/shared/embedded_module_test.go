package shared

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func embeddedTestModule(t *testing.T, types, funcs, code [][]byte, extra ...[]byte) *wasm.Module {
	t.Helper()
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
	}
	sections = append(sections, extra...)
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(code...)))
	m, err := wasm.DecodeModule(wasmtest.Module(sections...))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileEmbeddedI32ModuleLayoutAndPreflight(t *testing.T) {
	m := embeddedTestModule(t,
		[][]byte{wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})},
		[][]byte{{0}, {1}},
		[][]byte{wasmtest.Code([]byte{0x41, 0x2a, 0x0b}), wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b})},
	)
	var bodies [][]byte
	fake := func(params int, body []byte) ([]byte, error) {
		bodies = append(bodies, append([]byte(nil), body...))
		return bytes.Repeat([]byte{0xaa}, 4+params*4), nil
	}
	cm, err := CompileEmbeddedI32Module(m, EmbeddedModuleOptions{}, "test32", 4, 8, []byte{0, 0, 0, 0}, fake)
	if err != nil {
		t.Fatal(err)
	}
	if len(cm.Entry) != 2 || cm.Entry[0] != 0 || cm.Entry[1] != 16 {
		t.Fatalf("entry=%v", cm.Entry)
	}
	if len(cm.Functions) != 2 || cm.Functions[1].Offset != 16 || cm.Functions[1].Size != 8 {
		t.Fatalf("metadata=%+v", cm.Functions)
	}
	if len(bodies) != 2 || !bytes.Equal(bodies[0], []byte{0, 0x41, 0x2a, 0x0b}) {
		t.Fatalf("reconstructed bodies=%x", bodies)
	}
	if cm.RequiredCodeBytes <= uint32(len(cm.Code)) {
		t.Fatalf("required=%d code=%d", cm.RequiredCodeBytes, len(cm.Code))
	}
	_, err = CompileEmbeddedI32Module(m, EmbeddedModuleOptions{CodeCapacity: cm.RequiredCodeBytes - 1}, "test32", 4, 8, []byte{0, 0, 0, 0}, fake)
	if err == nil || !strings.Contains(err.Error(), "preflight requirement") {
		t.Fatalf("capacity error=%v", err)
	}
}

func TestPublishEmbeddedModuleIsTransactional(t *testing.T) {
	module := &EmbeddedModule{
		Code:      []byte{1, 2, 3, 4},
		Entry:     []int{0},
		Functions: []EmbeddedFunctionMetadata{{FuncIndex: 0, Size: 4}},
	}
	arena := embedded32.NewCodeArena(make([]byte, 64))
	publishErr := errors.New("cache sync")
	if _, err := PublishEmbeddedModule(arena, module, func(uint32, []byte) error { return publishErr }); !errors.Is(err, embedded32.ErrPublish) {
		t.Fatalf("publish error=%v", err)
	}
	if arena.Used() != 0 || arena.Published() != 0 {
		t.Fatalf("failed publish retained used=%d published=%d", arena.Used(), arena.Published())
	}
	published, err := PublishEmbeddedModule(arena, module, func(offset uint32, code []byte) error {
		if offset != 0 || !bytes.Equal(code, module.Code) {
			t.Fatalf("publisher offset=%d code=%x", offset, code)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if published.Block.Offset != 0 || len(published.Entry) != 1 || published.Entry[0] != 0 || published.Functions[0].Offset != 0 {
		t.Fatalf("published=%+v", published)
	}
	if arena.Used() != 4 || arena.Published() != 4 {
		t.Fatalf("successful publish used=%d published=%d", arena.Used(), arena.Published())
	}
}

func TestCompileEmbeddedI32ModuleReconstructsLocals(t *testing.T) {
	localBody := []byte{1, 1, 0x7f, 0x41, 7, 0x21, 0, 0x20, 0, 0x0b}
	code := append(wasmtest.ULEB(uint32(len(localBody))), localBody...)
	m := embeddedTestModule(t,
		[][]byte{wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})},
		[][]byte{{0}}, [][]byte{code},
	)
	var got []byte
	_, err := CompileEmbeddedI32Module(m, EmbeddedModuleOptions{}, "test32", 4, 8, []byte{0, 0, 0, 0}, func(_ int, body []byte) ([]byte, error) {
		got = append([]byte(nil), body...)
		return []byte{0, 0, 0, 0}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, localBody) {
		t.Fatalf("body=%x want=%x", got, localBody)
	}
}

func TestCompileEmbeddedI32ModuleRejectsIncompatibleModules(t *testing.T) {
	validCode := [][]byte{wasmtest.Code([]byte{0x41, 0, 0x0b})}
	tests := []struct {
		name string
		m    func(*testing.T) *wasm.Module
		want string
	}{
		{"nil", func(*testing.T) *wasm.Module { return nil }, "nil module"},
		{"i64 signature", func(t *testing.T) *wasm.Module {
			return embeddedTestModule(t, [][]byte{wasmtest.FuncType(nil, []wasm.ValType{wasm.I64})}, [][]byte{{0}}, [][]byte{wasmtest.Code([]byte{0x42, 0, 0x0b})})
		}, "result signature"},
		{"memory", func(t *testing.T) *wasm.Module {
			return embeddedTestModule(t, [][]byte{wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})}, [][]byte{{0}}, validCode, wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})))
		}, "runtime state"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CompileEmbeddedI32Module(tc.m(t), EmbeddedModuleOptions{}, "test32", 4, 8, []byte{0, 0, 0, 0}, func(int, []byte) ([]byte, error) { return []byte{0, 0, 0, 0}, nil })
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v want %q", err, tc.want)
			}
		})
	}
}
