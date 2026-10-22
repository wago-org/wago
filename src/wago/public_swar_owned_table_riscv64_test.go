//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"runtime"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64PublicSWAROwnedTableModule() []byte {
	params := []wasm.ValType{wasm.I32, wasm.V128, wasm.F32, wasm.I64, wasm.V128, wasm.F64}
	results := []wasm.ValType{wasm.V128, wasm.I32, wasm.F64, wasm.V128, wasm.F32, wasm.I64, wasm.V128}
	callerParams := append(slices.Clone(params), wasm.I32)
	callerResults := append([]wasm.ValType{wasm.V128}, results...)
	wrongParams := slices.Clone(params)
	wrongParams[0] = wasm.I64
	vectorInts := func(n int) []wasm.ValType {
		p := []wasm.ValType{wasm.V128}
		for range n {
			p = append(p, wasm.I32)
		}
		return p
	}
	signatures := [][]byte{
		wasmtest.FuncType(callerParams, callerResults), wasmtest.FuncType(params, results), wasmtest.FuncType(wrongParams, results),
		wasmtest.FuncType(vectorInts(2), []wasm.ValType{wasm.V128, wasm.I32, wasm.I32}),
		wasmtest.FuncType(vectorInts(3), []wasm.ValType{wasm.V128, wasm.I32}),
		wasmtest.FuncType(vectorInts(2), []wasm.ValType{wasm.V128, wasm.I32, wasm.I32}),
		wasmtest.FuncType(vectorInts(0), []wasm.ValType{wasm.V128}),
		wasmtest.FuncType(vectorInts(0), []wasm.ValType{wasm.V128, wasm.I32}),
	}
	caller := []byte{0x20, 4}
	for i := range params {
		caller = append(caller, 0x20, byte(i))
	}
	caller = append(caller, 0x20, 6, 0x11, 1, 0, 0x0b)
	leaf := []byte{0x20, 1, 0x20, 4, 0xfd, 81, 0x20, 0, 0x20, 5, 0x20, 4, 0x20, 2, 0x20, 3, 0x23, 0, 0x0b}
	wrong := append([]byte{}, leaf[:8]...)
	wrong = append(wrong, 0xa7)
	wrong = append(wrong, leaf[8:]...)
	selectTarget := []byte{0xd2, 1, 0xd0, 0x70, 0x20, 2, 0x1c, 1, 0x70}
	set := []byte{0x20, 0, 0x20, 1, 0x25, 0, 0xd1, 0x20, 1}
	set = append(set, selectTarget...)
	set = append(set, 0x26, 0, 0x20, 1, 0x25, 0, 0xd1, 0x0b)
	fill := []byte{0x20, 0, 0x20, 1, 0xd2, 1, 0xd0, 0x70, 0x20, 3, 0x1c, 1, 0x70, 0x20, 2, 0xfc, 17, 0, 0xfc, 16, 0, 0x0b}
	copyBody := []byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0xfc, 14, 0, 0, 0xfc, 16, 0, 0x0b}
	grow := append([]byte{0x20, 0}, selectTarget...)
	grow = append(grow, 0x20, 1, 0xfc, 15, 0, 0xfc, 16, 0, 0x0b)
	init := []byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0xfc, 12, 1, 0, 0xfc, 16, 0, 0x0b}
	drop := []byte{0x20, 0, 0xfc, 13, 1, 0x0b}
	size := []byte{0x20, 0, 0xfc, 16, 0, 0x0b}
	bodies := [][]byte{caller, leaf, wrong, set, fill, copyBody, grow, init, drop, size}
	arities := []int{7, 6, 6, 3, 4, 4, 3, 4, 1, 1}
	code := make([][]byte, len(bodies))
	for i, body := range bodies {
		// Force framed pair homes beyond the signed12-bit displacement window.
		encoded := append([]byte{1, byte(128 - arities[i]), 0x7b}, body...)
		code[i] = append(wasmtest.ULEB(uint32(len(encoded))), encoded...)
	}
	raw := make([]byte, 16)
	binary.LittleEndian.PutUint64(raw, 0x7ff0000000000001)
	binary.LittleEndian.PutUint64(raw[8:], 0x80000000deadbeef)
	literal := append(append([]byte{0xfd, 12}, raw...), 0x0b)
	active := []byte{0, 0x41, 0, 0x0b, 3, 1, 2, 1}
	passive := []byte{1, 0, 2, 1, 2}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(signatures...)), wasmtest.Section(3, []byte{10, 0, 1, 2, 3, 4, 4, 5, 4, 6, 7}), wasmtest.Section(4, []byte{1, 0x70, 1, 4, 8}), wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.V128, true, literal))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 0), wasmtest.ExportEntry("set", 0, 3), wasmtest.ExportEntry("fill", 0, 4), wasmtest.ExportEntry("copy", 0, 5), wasmtest.ExportEntry("grow", 0, 6), wasmtest.ExportEntry("init", 0, 7), wasmtest.ExportEntry("drop", 0, 8), wasmtest.ExportEntry("size", 0, 9), wasmtest.ExportEntry("global", 3, 0))), wasmtest.Section(9, wasmtest.Vec(active, passive)), wasmtest.Section(10, wasmtest.Vec(code...)))
}

func rv64PublicSWAROwnedTableInstances(t *testing.T, artifact bool) (*Instance, *Instance) {
	t.Helper()
	c, err := Compile(nil, rv64PublicSWAROwnedTableModule())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if artifact {
		blob, err := c.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := LoadTrustedArtifact(blob)
		if loaded != nil {
			t.Cleanup(func() { _ = loaded.Close() })
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		c = loaded
	}
	instances := make([]*Instance, 2)
	for i := range instances {
		in, err := Instantiate(c)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = in.Close() })
		instances[i] = in
		if table, err := in.ExportedTable("private"); table != nil || err == nil {
			t.Fatal("private table escaped public policy")
		}
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	return instances[0], instances[1]
}

func rv64PublicSWAROwnedTableCall(t *testing.T, in *Instance, index uint64, global V128) {
	t.Helper()
	args := []uint64{0xdeadbeefffffffff, 0xfff0000000000001, 0x7fa1234500000001, 0xaabbccddff800001, 0x8000000000000001, 0x8899aabbccddeeff, 0x0807060504030201, 0x7ff0000000000001, index | 0xdeadbeef00000000}
	want := []uint64{args[5], args[6], args[1] ^ args[5], args[2] ^ args[6], 0xffffffff, args[7], args[5], args[6], 0xff800001, args[4], binary.LittleEndian.Uint64(global[:]), binary.LittleEndian.Uint64(global[8:])}
	importedGlobalInvoke(t, in, "call", want, args...)
}

func TestRV64PublicSWAROwnedTableTypedPairsAndArtifactIsolation(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			a, b := rv64PublicSWAROwnedTableInstances(t, artifact)
			initial := rv64PublicVector(0x7ff0000000000001, 0x80000000deadbeef)
			rv64PublicSWAROwnedTableCall(t, a, 0, initial)
			rv64PublicSWAROwnedTableCall(t, b, 2, initial)
			replacement := rv64PublicVector(0xff8000017f800001, 0x8000000000000000)
			if err := a.SetGlobalV128("global", replacement); err != nil {
				t.Fatal(err)
			}
			rv64PublicSWAROwnedTableCall(t, a, 2, replacement)
			rv64PublicSWAROwnedTableCall(t, b, 0, initial)
			for _, test := range []struct {
				index uint64
				code  coreruntime.TrapCode
			}{{1, coreruntime.TrapIndirectWrongSig}, {3, coreruntime.TrapIndirectOutOfBounds}, {4, coreruntime.TrapIndirectOutOfBounds}, {0xffffffff, coreruntime.TrapIndirectOutOfBounds}} {
				before := slices.Clone(privateTableBytes(a))
				output, err := a.Invoke("call", 0xdeadbeefffffffff, 1, 2, 0xaabbccddff800001, 3, 4, 5, 6, test.index|0xdeadbeef00000000)
				ownedTableTrap(t, err, test.code)
				if output != nil || !slices.Equal(before, privateTableBytes(a)) {
					t.Fatal("typed indirect failure changed table or produced results")
				}
				rv64PublicSWAROwnedTableCall(t, a, 0, replacement)
			}
			if err := a.Close(); err != nil {
				t.Fatal(err)
			}
			if a.hasPhysicalResources() {
				t.Fatal("closed SIMD table owner retained resources")
			}
			rv64PublicSWAROwnedTableCall(t, b, 0, initial)
		})
	}
}

func TestRV64PublicSWAROwnedTableMutationElementsAndGrowth(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			a, b := rv64PublicSWAROwnedTableInstances(t, artifact)
			raw := []uint64{0x7ff0000000000042, 0x80000000deadbeef}
			initial := rv64PublicVector(0x7ff0000000000001, 0x80000000deadbeef)
			// Snapshot distinct leaf/wrong-signature/leaf/null rows, then model
			// copies in both directions from the immutable original source rows.
			for _, indices := range [][2]int{{1, 0}, {0, 1}} {
				before := slices.Clone(privateTableBytes(a))
				want := slices.Clone(before)
				for i := range 3 {
					dst := 8 + (indices[0]+i)*coreruntime.TableEntryBytes
					src := 8 + (indices[1]+i)*coreruntime.TableEntryBytes
					copy(want[dst:dst+coreruntime.TableEntryBytes], before[src:src+coreruntime.TableEntryBytes])
				}
				importedGlobalInvoke(t, a, "copy", append(slices.Clone(raw), 4), raw[0], raw[1], uint64(indices[0])|0xdeadbeef00000000, uint64(indices[1])|0xaabbccdd00000000, 3)
				if !slices.Equal(want, privateTableBytes(a)) {
					t.Fatal("overlapping table copy differs from whole-row snapshot oracle", indices)
				}
			}
			importedGlobalInvoke(t, a, "set", append(slices.Clone(raw), 0, 1), raw[0], raw[1], 3, 0)
			importedGlobalInvoke(t, a, "set", append(slices.Clone(raw), 0, 0), raw[0], raw[1], 0xdeadbeef00000001, 0xdeadbeef00000001)
			rv64PublicSWAROwnedTableCall(t, a, 1, initial)
			importedGlobalInvoke(t, a, "set", append(slices.Clone(raw), 0, 1), raw[0], raw[1], 2, 0)
			importedGlobalInvoke(t, a, "fill", append(slices.Clone(raw), 4), raw[0], raw[1], 0, 3, 1)
			rv64PublicSWAROwnedTableCall(t, a, 2, initial)
			// Invalid bulk destination/source ranges leave every row unchanged.
			for _, test := range []struct {
				name     string
				operands []uint64
			}{{"copy", []uint64{0, 3, 2}}, {"copy", []uint64{3, 0, 2}}, {"fill", []uint64{3, 2, 1}}, {"init", []uint64{3, 0, 2}}, {"init", []uint64{0, 1, 2}}, {"set", []uint64{4, 1}}} {
				before := slices.Clone(privateTableBytes(a))
				output, err := a.Invoke(test.name, append(slices.Clone(raw), test.operands...)...)
				ownedTableTrap(t, err, coreruntime.TrapTableOutOfBounds)
				if output != nil || !slices.Equal(before, privateTableBytes(a)) {
					t.Fatal("SIMD table mutation wrote before full preflight", test.name)
				}
			}
			importedGlobalInvoke(t, a, "init", append(slices.Clone(raw), 4), raw[0], raw[1], 1, 0, 2)
			rv64PublicSWAROwnedTableCall(t, a, 1, initial)
			// Passive segment index1 initializes the different same-slot-width signature.
			_, err := a.Invoke("call", 1, 2, 3, 4, 5, 6, 7, 8, 2)
			ownedTableTrap(t, err, coreruntime.TrapIndirectWrongSig)
			importedGlobalInvoke(t, a, "drop", raw, raw...)
			before := slices.Clone(privateTableBytes(a))
			output, err := a.Invoke("init", raw[0], raw[1], 0, 0, 1)
			ownedTableTrap(t, err, coreruntime.TrapTableOutOfBounds)
			if output != nil || !slices.Equal(before, privateTableBytes(a)) {
				t.Fatal("dropped SIMD element segment wrote table")
			}
			importedGlobalInvoke(t, b, "init", append(slices.Clone(raw), 4), raw[0], raw[1], 1, 0, 2)
			rv64PublicSWAROwnedTableCall(t, b, 1, initial)
			importedGlobalInvoke(t, a, "grow", append(slices.Clone(raw), 4, 6), raw[0], raw[1], 0xdeadbeef00000002, 1)
			rv64PublicSWAROwnedTableCall(t, a, 5, initial)
			importedGlobalInvoke(t, a, "grow", append(slices.Clone(raw), 6, 8), raw[0], raw[1], 2, 0)
			importedGlobalInvoke(t, a, "grow", append(slices.Clone(raw), 0xffffffff, 8), raw[0], raw[1], 1, 1)
			importedGlobalInvoke(t, a, "grow", append(slices.Clone(raw), 8, 8), raw[0], raw[1], 0xdeadbeef00000000, 1)
			importedGlobalInvoke(t, b, "size", append(slices.Clone(raw), 4), raw...)
			if err := a.Close(); err != nil {
				t.Fatal(err)
			}
			if a.hasPhysicalResources() {
				t.Fatal("retired SIMD bulk-table owner retained resources")
			}
			rv64PublicSWAROwnedTableCall(t, b, 0, initial)
		})
	}
}
