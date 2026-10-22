//go:build linux && !tinygo

package riscv64

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	native "github.com/wago-org/wago/src/core/runtime/riscv64"
)

func TestMemoryStateAdmissionMatrix(t *testing.T) {
	good := fixtureFunc{result: 0x7f, body: []byte{0x41, 0, 0x0b}}
	base := stateModule([]fixtureFunc{good}, false, 1, 2, true, nil, nil)
	manyG := make([]globalFixture, 257)
	manyData := make([]native.MemoryInitializer, 257)
	matrix := map[string][]byte{
		"shared":          modifySection(base, 5, 5, []byte{1, 3, 1, 2}, true),
		"memory64":        modifySection(base, 5, 5, []byte{1, 5, 1, 2}, true),
		"multiple":        modifySection(base, 5, 5, []byte{2, 1, 1, 2, 1, 1, 2}, true),
		"FP-load":         stateModule([]fixtureFunc{{params: []byte{0x7f}, result: 0x7f, body: []byte{0x20, 0, 0x2a, 2, 0, 0x1a, 0x41, 0, 0x0b}}}, false, 1, 2, true, nil, nil),
		"257-globals":     stateModule([]fixtureFunc{good}, false, 1, 2, true, manyG, nil),
		"257-data":        stateModule([]fixtureFunc{good}, false, 1, 2, true, nil, manyData),
		"extended-global": modifySection(base, 7, 6, []byte{1, 0x7f, 0, 0x41, 1, 0x41, 2, 0x6a, 0x0b}, false),
	}
	for name, data := range matrix {
		t.Run(name, func(t *testing.T) {
			m, err := wasm.DecodeModule(data)
			if err != nil {
				t.Fatal("fixture decode", err)
			}
			err = wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{MultiMemory: true}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256})
			if err != nil {
				t.Fatal("fixture validation", err)
			}
			c, err := Compile(data)
			if c != nil || err == nil {
				t.Fatal("excluded shape admitted", c, err)
			}
			if name != "multiple" && !errors.Is(err, ErrUnsupported) {
				t.Fatal("unexpected rejection", err)
			}
		})
	}
}
func TestMemoryNamedImportTypeBinding(t *testing.T) {
	fs := []fixtureFunc{sizeFixture()}
	c, err := Compile(stateModule(fs, true, 2, 2, true, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	m, err := native.NewLinearMemory(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	opts := InstantiateOptions{ImportedMemory: m, ImportModule: "m", ImportName: "x"}
	if _, l, _, err := c.state.bindMemory(opts); err == nil || l != nil {
		t.Fatal("initial minimum matched", l, err)
	}
	if _, ok := m.Grow(1); !ok {
		t.Fatal("host growth")
	}
	_, l, owns, err := c.state.bindMemory(opts)
	if err != nil || owns {
		t.Fatal(err, owns)
	}
	l.Close()
	for _, bad := range []InstantiateOptions{{}, {ImportedMemory: m, ImportModule: "wrong", ImportName: "x"}, {ImportedMemory: m, ImportModule: "m", ImportName: "wrong"}} {
		if _, l, _, err := c.state.bindMemory(bad); err == nil || l != nil {
			t.Fatal("unnamed import admitted", l, err)
		}
	}
	wide, err := native.NewLinearMemoryWithLimits(2, 65536, true)
	if err != nil {
		t.Fatal(err)
	}
	defer wide.Close()
	noMax, err := native.NewLinearMemoryWithLimits(2, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	defer noMax.Close()
	for _, provider := range []*native.LinearMemory{wide, noMax} {
		opts.ImportedMemory = provider
		if _, l, _, err := c.state.bindMemory(opts); err == nil || l != nil {
			t.Fatal("declared maximum ignored", l, err)
		}
	}
}
func TestNativeMemoryZeroLength(t *testing.T) {
	i := stateInstance(t, stateModule([]fixtureFunc{memoryFixture(0x2d, 0), sizeFixture(), growFixture()}, false, 0, 0, true, nil, nil), InstantiateOptions{})
	expectInteger(t, i, 0, TrapMemoryBounds, 0)
	if got, err := i.Invoke(context.Background(), "b"); err != nil || got != 0 {
		t.Fatal(got, err)
	}
	for _, delta := range []uint64{0, 1, 0xdeadbeef00000000, 0xffffffff} {
		want := uint64(0xffffffff)
		if uint32(delta) == 0 {
			want = 0
		}
		if got, err := i.Invoke(context.Background(), "c", delta); err != nil || got != want {
			t.Fatal(got, want, err)
		}
	}
	store := stateInstance(t, stateModule([]fixtureFunc{memoryFixture(0x37, 0)}, false, 0, 0, true, nil, nil), InstantiateOptions{})
	expectInteger(t, store, 0, TrapMemoryBounds, 0, ^uint64(0))
}
func TestNativeMemorySignedNarrowEdges(t *testing.T) {
	for _, op := range []byte{0x2c, 0x2d, 0x2e, 0x2f, 0x30, 0x31, 0x32, 0x33, 0x34, 0x35} {
		size, wide, signed, _, _ := memoryOpcode(op)
		i := stateInstance(t, stateModule([]fixtureFunc{memoryFixture(op, 0)}, false, 1, 1, true, nil, nil), InstantiateOptions{})
		for _, word := range []uint64{0x80007f01, 0x7fff80ff, 0xffffffff, 0, 1} {
			b := []byte{byte(word), byte(word >> 8), byte(word >> 16), byte(word >> 24)}
			if err := i.WriteMemory("mem", 9, b[:size]); err != nil {
				t.Fatal(err)
			}
			expectInteger(t, i, loadExpected(b[:size], wide, signed), 0, 0xdeadbeef00000009)
		}
	}
}
func TestNativeGlobalsOnlyI32WidthsAndImmutability(t *testing.T) {
	get := fixtureFunc{result: 0x7f, body: []byte{0x23, 0, 0x0b}}
	set := fixtureFunc{params: []byte{0x7f}, result: 0x7f, body: []byte{0x20, 0, 0x24, 0, 0x23, 0, 0x0b}}
	b := modifySection(binaryModule(get, set), 7, 6, []byte{1, 0x7f, 1, 0x41, 0x7f, 0x0b}, false)
	b = modifySection(b, 7, 7, []byte{3, 1, 'a', 0, 0, 1, 'b', 0, 1, 1, 'g', 3, 0}, true)
	i := stateInstance(t, b, InstantiateOptions{})
	if i.memory != nil || i.lease != nil {
		t.Fatal("globals-only module allocated memory")
	}
	expectInteger(t, i, 0xffffffff, 0)
	for _, raw := range []uint64{0xdeadbeef00000000, 0xdeadbeefffffffff, 0xdeadbeef80000000, 1} {
		want := uint64(uint32(raw))
		if got, err := i.Invoke(context.Background(), "b", raw); err != nil || got != want {
			t.Fatal(got, want, err)
		}
		expectInteger(t, i, want, 0)
		if got, err := i.Global("g"); err != nil || got != want {
			t.Fatal(got, want, err)
		}
	}
	immutable := modifySection(binaryModule(get), 7, 6, []byte{1, 0x7f, 0, 0x41, 0x7f, 0x0b}, false)
	j := stateInstance(t, immutable, InstantiateOptions{})
	expectInteger(t, j, 0xffffffff, 0)
	if runtime.GOARCH != "riscv64" {
		t.Fatal("unreachable host")
	}
}
