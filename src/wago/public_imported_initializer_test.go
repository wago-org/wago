//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package wago

import (
	"math"
	"runtime"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Four immutable imports initialize four separate mutable local cells. Memory
// import positions exercise global-index space independently of import ordinals.
func publicImportedInitializerModule(memory int, payload []byte, extraSegments ...[]byte) []byte {
	types := []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}
	var imports, globals, exports [][]byte
	for i, typ := range types {
		imports = append(imports, wasmtest.GlobalImportEntry("env", "g"+string(rune('0'+i)), typ, false))
		globals = append(globals, wasmtest.GlobalEntry(typ, true, []byte{0x23, byte(i), 0x0b}))
		exports = append(exports, wasmtest.ExportEntry("local"+string(rune('0'+i)), 3, uint32(i+4)))
	}
	if memory >= 2 {
		entry := append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, 1, 1, 4)
		switch memory {
		case 3:
			imports = append([][]byte{entry}, imports...)
		case 4:
			imports = append(append(append([][]byte{}, imports[:2]...), entry), imports[2:]...)
		default:
			imports = append(imports, entry)
		}
	}
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, types), wasmtest.FuncType(types, types))),
		wasmtest.Section(2, wasmtest.Vec(imports...)),
		wasmtest.Section(3, []byte{2, 0, 1}),
	}
	if memory == 1 {
		sections = append(sections, wasmtest.Section(5, []byte{1, 1, 1, 4}))
	}
	sections = append(sections, wasmtest.Section(6, wasmtest.Vec(globals...)))
	exports = append(exports, wasmtest.ExportEntry("get", 0, 0), wasmtest.ExportEntry("set", 0, 1))
	if memory != 0 {
		exports = append(exports, wasmtest.ExportEntry("memory", 2, 0))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(exports...)))
	get := []byte{0x23, 4, 0x23, 5, 0x23, 6, 0x23, 7, 0x0b}
	set := append([]byte{0x20, 0, 0x24, 4, 0x20, 1, 0x24, 5, 0x20, 2, 0x24, 6, 0x20, 3, 0x24, 7}, get...)
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(get), wasmtest.Code(set))))
	if memory != 0 {
		segment := append([]byte{0, 0x23, 0, 0x0b}, wasmtest.ULEB(uint32(len(payload)))...)
		segment = append(segment, payload...)
		sections = append(sections, wasmtest.Section(11, wasmtest.Vec(append([][]byte{segment}, extraSegments...)...)))
	}
	return wasmtest.Module(sections...)
}

func initializerValueImports(bits []uint64) *Imports {
	im := NewImports()
	for i, typ := range []ValType{ValI32, ValI64, ValF32, ValF64} {
		im.Global("env", "g"+string(rune('0'+i)), GlobalImport{Type: typ, Bits: bits[i]})
	}
	return im
}

func TestPublicImportedInitializerRawBitsArtifactAndIndependentCells(t *testing.T) {
	bits := []uint64{0xdeadbeef00000007, 0x8000000000000001, 0xbeefcafe7f800001, 0x7ff0000000000001}
	c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), publicImportedInitializerModule(1, []byte{42}))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if len(c.GlobalImports) != 4 || len(c.Globals) != 8 || !c.Data[0].Offset.HasGlobal {
		t.Fatal("initializer metadata was replaced with placeholder constants")
	}
	for i := range 4 {
		if !c.Globals[i+4].HasInitGlobal || c.Globals[i+4].InitGlobal != i {
			t.Fatal("initializer metadata", c.Globals)
		}
	}

	objects := []*Global{NewGlobalI32(7, false), NewGlobalI64(int64(bits[1]), false), NewGlobalF32(math.Float32frombits(uint32(bits[2])), false), NewGlobalF64(math.Float64frombits(bits[3]), false)}
	im := NewImports()
	for i, g := range objects {
		im.Global("env", "g"+string(rune('0'+i)), g)
		defer g.Close()
	}
	a, err := Instantiate(c, im)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Instantiate(c, initializerValueImports(bits))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	want := []uint64{7, bits[1], uint64(uint32(bits[2])), bits[3]}
	importedGlobalInvoke(t, a, "get", want)
	importedGlobalInvoke(t, b, "get", want)
	if got := a.Memory().UnsafeBytes()[7]; got != 42 {
		t.Fatal("imported offset was replaced by placeholder zero", got)
	}
	changed := []uint64{0xffffffff, 3, 0x80000000, 0x8000000000000000}
	importedGlobalInvoke(t, a, "set", changed, changed...)
	importedGlobalInvoke(t, b, "get", want)
	for i, g := range objects {
		if got := readGlobalObject(g, []ValType{ValI32, ValI64, ValF32, ValF64}[i]); got != want[i] {
			t.Fatal("mutable derived global aliased immutable source", i, got)
		}
	}
	fn, err := a.WasmFunc("get")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fn.Invoke(); err != nil {
		t.Fatal(err)
	}
	if got := testing.AllocsPerRun(100, func() {
		r, e := fn.Invoke()
		if e != nil || !slices.Equal(r, changed) {
			panic("initializer invoke")
		}
	}); got != 0 {
		t.Fatal("warm initializer call allocation", got)
	}

}

func TestPublicImportedInitializerProducerCloseAndMemoryOrder(t *testing.T) {
	for _, order := range []int{2, 3, 4} {
		t.Run(string(rune('0'+order)), func(t *testing.T) {
			producerCode := importedGlobalCompile(t, false, 1, false)
			producer, err := Instantiate(producerCode, NewImports())
			if err != nil {
				t.Fatal(err)
			}
			defer producer.Close()
			im := NewImports()
			for i := range 4 {
				g, e := producer.ExportedGlobalObject("g" + string(rune('0'+i)))
				if e != nil {
					t.Fatal(e)
				}
				im.Global("env", "g"+string(rune('0'+i)), g)
			}
			memory, err := producer.ExportedMemory("memory")
			if err != nil {
				t.Fatal(err)
			}
			im.Memory("env", "memory", memory)
			c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), publicImportedInitializerModule(order, []byte{42}))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			a, err := Instantiate(c, im)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			b, err := Instantiate(c, im)
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close()
			if a.Memory().UnsafeBytes()[7] != 42 {
				t.Fatal("global-index/memory-import ordinal confusion")
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			want := []uint64{7, 9, 0x80000000, 0x8000000000000000}
			importedGlobalInvoke(t, a, "get", want)
			importedGlobalInvoke(t, b, "get", want)
			changed := []uint64{1, 2, 3, 4}
			importedGlobalInvoke(t, a, "set", changed, changed...)
			importedGlobalInvoke(t, b, "get", want)
			if err := a.Close(); err != nil {
				t.Fatal(err)
			}
			if !producer.hasPhysicalResources() {
				t.Fatal("remaining importer lost producer")
			}
			if err := b.Close(); err != nil {
				t.Fatal(err)
			}
			if producer.hasPhysicalResources() {
				t.Fatal("last importer retained producer")
			}
		})
	}
}

func TestPublicImportedInitializerUnsignedOffsetBounds(t *testing.T) {
	for _, tc := range []struct {
		name    string
		offset  uint64
		payload []byte
		ok      bool
	}{
		{"exact-end-empty", 65536, nil, true}, {"exact-end-byte", 65536, []byte{42}, false}, {"negative-empty", 0xffffffff, nil, false}, {"last-byte", 65535, []byte{42}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), publicImportedInitializerModule(1, tc.payload))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			g := NewGlobalI32(int32(uint32(tc.offset)), false)
			defer g.Close()
			im := NewImports().Global("env", "g0", g)
			for i, typ := range []ValType{ValI64, ValF32, ValF64} {
				im.Global("env", "g"+string(rune('1'+i)), GlobalImport{Type: typ, Bits: uint64(i + 1)})
			}
			in, err := Instantiate(c, im)
			if tc.ok {
				if err != nil || in == nil {
					t.Fatal(in, err)
				}
				if len(tc.payload) > 0 && in.Memory().UnsafeBytes()[tc.offset] != 42 {
					t.Fatal("wrong active data offset")
				}
				if err := in.Close(); err != nil {
					t.Fatal(err)
				}
			} else if err == nil || in != nil {
				t.Fatal("unsigned invalid offset instantiated", in, err)
			}
			if err := g.Close(); err != nil {
				t.Fatal("initializer failure leaked global attachment", err)
			}
		})
	}
}

func TestPublicImportedInitializerOrderedFailureReleasesOwners(t *testing.T) {
	producerCode := importedGlobalCompile(t, false, 1, false)
	producer, err := Instantiate(producerCode, NewImports())
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	im := NewImports()
	for i := range 4 {
		g, e := producer.ExportedGlobalObject("g" + string(rune('0'+i)))
		if e != nil {
			t.Fatal(e)
		}
		im.Global("env", "g"+string(rune('0'+i)), g)
	}
	memory, err := producer.ExportedMemory("memory")
	if err != nil {
		t.Fatal(err)
	}
	im.Memory("env", "memory", memory)
	bad := append([]byte{0, 0x41}, wasmtest.SLEB32(65536)...)
	bad = append(bad, 0x0b, 1, 99)
	c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), publicImportedInitializerModule(3, []byte{42}, bad))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if in, err := Instantiate(c, im); in != nil || err == nil {
		t.Fatal("later invalid initializer instantiated", in, err)
	}
	if memory.UnsafeBytes()[7] != 42 {
		t.Fatal("ordered initializers lost preceding imported-offset effect")
	}
	importedGlobalInvoke(t, producer, "get", []uint64{7, 9, 0x80000000, 0x8000000000000000, 33})
	if err := producer.Close(); err != nil {
		t.Fatal(err)
	}
	if producer.hasPhysicalResources() || memory.jobMemory() != nil {
		t.Fatal("failed construction retained memory/global owners")
	}
}
