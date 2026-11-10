//go:build linux && amd64 && wago_guardpage

package amd64

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
)

func callStoreValueGuarded(t *testing.T, m *wasm.Module, arg uint32) error {
	t.Helper()
	compiled, err := CompileModuleWith(m, CompileOptions{ElideBoundsChecks: true})
	if err != nil {
		t.Fatal(err)
	}
	if compiled.CodeImage != nil {
		defer compiled.CodeImage.Close()
	}
	engine, err := runtime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	if err := runtime.InstallGuardTrapHandler(); err != nil {
		t.Fatal(err)
	}
	memory, err := runtime.NewJobMemoryGuarded(1<<16, 1<<16)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	arena, err := runtime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer arena.Close()
	code, entry, err := runtime.MapCode(compiled.Code)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Unmap(code)
	args := arena.Alloc(128)
	binary.LittleEndian.PutUint32(args, arg)
	return engine.CallGuarded(entry+uintptr(compiled.Entry[0]), args, memory.LinMemBase(),
		arena.Alloc(runtime.TrapBufferBytes), arena.Alloc(128), memory)
}

func TestStoreValueLoadFoldPreservesTrapOrder(t *testing.T) {
	cases := []struct {
		name string
		body []byte
	}{
		{"address load before value division", []byte{0x00,
			0x20, 0x00, 0x28, 0x02, 0x00, // destination address load traps
			0x41, 0x01, 0x41, 0x00, 0x6e, // later division by zero
			0x41, 0x01, 0x6a, 0x36, 0x02, 0x00, 0x0b,
		}},
		{"value load before value division", []byte{0x00,
			0x41, 0x00, // destination address
			0x20, 0x00, 0x28, 0x02, 0x00, // value load traps
			0x41, 0x01, 0x41, 0x00, 0x6e, // later division by zero
			0x6a, 0x36, 0x02, 0x00, 0x0b,
		}},
		{"first of two value loads", []byte{0x00,
			0x41, 0x00, // destination address
			0x20, 0x00, 0x28, 0x02, 0x00, // first value load traps
			0x41, 0x00, 0x28, 0x02, 0x00, // second value load is valid
			0x6a, 0x36, 0x02, 0x00, 0x0b,
		}},
	}
	saved := storeValueLoadFoldEnabled
	defer func() { storeValueLoadFoldEnabled = saved }()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := modMem(t, 1, []wasm.ValType{wasm.I32}, nil, tc.body)
			for _, enabled := range []bool{false, true} {
				storeValueLoadFoldEnabled = enabled
				if err := callStoreValueGuarded(t, m, 65536); err == nil || !strings.Contains(err.Error(), "out of bounds") {
					t.Fatalf("enabled=%t: trap=%v, want memory out of bounds", enabled, err)
				}
			}
		})
	}
}
