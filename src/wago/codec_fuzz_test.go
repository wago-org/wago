package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/runtime/gc"
)

func FuzzCompiledCodecMutations(f *testing.F) {
	for _, seed := range compiledCodecFuzzSeeds(f) {
		addCompiledCodecMutationSeeds(f, seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		var c Compiled
		if err := c.UnmarshalBinary(data); err != nil {
			return
		}

		encoded, err := c.MarshalBinary()
		if err != nil {
			t.Fatalf("MarshalBinary after successful decode: %v", err)
		}
		var roundtrip Compiled
		if err := roundtrip.UnmarshalBinary(encoded); err != nil {
			t.Fatalf("roundtrip UnmarshalBinary: %v", err)
		}
	})
}

func compiledCodecFuzzSeeds(t testing.TB) [][]byte {
	t.Helper()
	structDesc, err := gc.NewStructDesc(0, []gc.StorageKind{gc.StorageRefNull, gc.StorageI32})
	if err != nil {
		t.Fatalf("struct desc seed: %v", err)
	}
	arrayDesc, err := gc.NewArrayDesc(1, gc.StorageI8)
	if err != nil {
		t.Fatalf("array desc seed: %v", err)
	}

	valid := []*Compiled{
		{},
		{
			Code:       []byte{0xc3},
			Entry:      []int{0},
			NumImports: 1,
			Imports:    []string{"env.host"},
			Funcs: []FuncSig{{
				Params:  []ValType{ValI32, ValI64},
				Results: []ValType{ValF64},
			}},
			Exports:    map[string]int{"run": 1},
			FuncTypeID: []uint32{0, 1},

			GlobalImports: []GlobalImportDef{{Module: "env", Name: "base", Type: ValI32}},
			Globals: []GlobalDef{
				{Type: ValI32},
				{Type: ValI32, HasInitGlobal: true, InitGlobal: 0},
			},
			GlobalExports: map[string]int{"g": 1},

			HasTable:  true,
			TableSize: 2,
			Elems:     []ElemInit{{Offset: OffsetInit{Base: 0}, Funcs: []uint32{0, 1}}},
			Data:      []DataInit{{Offset: OffsetInit{Base: 1}, Bytes: []byte{1, 2, 3}}},

			memoryImport: "env.memory",
			GCTypeDescs:  []gc.TypeDesc{structDesc, arrayDesc},
		},
	}

	seeds := make([][]byte, 0, len(valid)+2)
	for _, c := range valid {
		b, err := c.MarshalBinary()
		if err != nil {
			t.Fatalf("marshal seed: %v", err)
		}
		seeds = append(seeds, b)
	}
	seeds = append(seeds,
		compiledBlobFromPayload(compiledCodecMaliciousEntryCountSeed()),
		compiledBlobFromPayload(compiledCodecMaliciousGCFieldCountSeed()),
	)
	return seeds
}

func addCompiledCodecMutationSeeds(f *testing.F, seed []byte) {
	f.Helper()
	f.Add(append([]byte(nil), seed...))
	if len(seed) == 0 {
		return
	}

	truncated := append([]byte(nil), seed[:len(seed)/2]...)
	f.Add(truncated)

	flipped := append([]byte(nil), seed...)
	flipped[len(flipped)/2] ^= 0xff
	f.Add(flipped)

	pos := len(seed) / 2
	inserted := make([]byte, 0, len(seed)+1)
	inserted = append(inserted, seed[:pos]...)
	inserted = append(inserted, 0xff)
	inserted = append(inserted, seed[pos:]...)
	f.Add(inserted)
}

func compiledBlobFromPayload(payload []byte) []byte {
	blob := make([]byte, 0, len(wagoMagic)+1+len(payload))
	blob = append(blob, wagoMagic...)
	blob = append(blob, wagoVersion)
	blob = append(blob, payload...)
	return blob
}

func compiledCodecMaliciousEntryCountSeed() []byte {
	var w compiledWriter
	w.bytes(nil)
	w.uvar(uint64(maxInt()))
	return w.buf
}

func compiledCodecMaliciousGCFieldCountSeed() []byte {
	var w compiledWriter
	w.bytes(nil)
	w.intSlice(nil)
	w.uvar(0) // NumImports.
	w.stringSlice(nil)
	if err := w.funcSigs(nil); err != nil {
		panic(err)
	}
	w.stringIntMap(nil)
	if err := w.globalImports(nil); err != nil {
		panic(err)
	}
	if err := w.globals(nil); err != nil {
		panic(err)
	}
	w.stringIntMap(nil)
	w.bool(false)
	w.uvar(0) // TableSize.
	w.u32Slice(nil)
	w.elems(nil)
	w.data(nil)
	w.str("")
	w.uvar(1)
	w.u32(0)     // ID.
	w.u8(0)      // Kind.
	w.bool(true) // Fields are present.
	w.uvar(uint64(maxInt()))
	for i := 0; i < 20; i++ {
		w.u8(0)
	}
	return w.buf
}
