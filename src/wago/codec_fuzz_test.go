package wago

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

func FuzzCompiledCodecGeneratedValidModules(f *testing.F) {
	for _, seed := range [][]byte{
		nil,
		{0},
		{1, 2, 3, 4, 5, 6, 7, 8},
		{0xff, 0x80, 0x40, 0x20, 0x10, 0x08},
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		input := generatedValidCompiled(t, data)
		if err := input.validate(); err != nil {
			t.Fatalf("generated invalid Compiled: %v", err)
		}

		encoded, err := input.MarshalBinary()
		if err != nil {
			t.Fatalf("MarshalBinary generated Compiled: %v", err)
		}
		var decoded Compiled
		if err := decoded.UnmarshalBinary(encoded); err != nil {
			t.Fatalf("UnmarshalBinary generated Compiled: %v", err)
		}
		if err := decoded.validate(); err != nil {
			t.Fatalf("decoded generated Compiled failed validation: %v", err)
		}
		reencoded, err := decoded.MarshalBinary()
		if err != nil {
			t.Fatalf("MarshalBinary decoded Compiled: %v", err)
		}
		if !bytes.Equal(encoded, reencoded) {
			t.Fatalf("codec is not deterministic after round trip")
		}
	})
}

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

type compiledFuzzBytes struct {
	data []byte
	pos  int
}

func (r *compiledFuzzBytes) next() byte {
	if len(r.data) == 0 {
		return 0
	}
	b := r.data[r.pos%len(r.data)]
	r.pos++
	return b
}

func (r *compiledFuzzBytes) n(limit int) int {
	if limit <= 0 {
		return 0
	}
	return int(r.next()) % limit
}

func (r *compiledFuzzBytes) smallString(prefix string) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz"
	n := r.n(4)
	buf := make([]byte, 0, len(prefix)+n)
	buf = append(buf, prefix...)
	for i := 0; i < n; i++ {
		buf = append(buf, alphabet[r.n(len(alphabet))])
	}
	return string(buf)
}

func (r *compiledFuzzBytes) valType() ValType {
	switch r.n(4) {
	case 0:
		return ValI32
	case 1:
		return ValI64
	case 2:
		return ValF32
	default:
		return ValF64
	}
}

func generatedValidCompiled(t testing.TB, data []byte) *Compiled {
	t.Helper()
	r := compiledFuzzBytes{data: data}

	importCount := r.n(3)
	localFuncCount := r.n(4)
	totalFuncs := importCount + localFuncCount

	c := &Compiled{
		NumImports:    importCount,
		Imports:       make([]string, importCount),
		importFuncSigs: make([]FuncSig, importCount),
		Funcs:          make([]FuncSig, localFuncCount),
		Entry:          make([]int, localFuncCount),
		FuncTypeID:    make([]uint32, totalFuncs),
		Exports:       map[string]int{},
		GlobalExports: map[string]int{},
	}
	for i := range c.Imports {
		c.Imports[i] = r.smallString("imp")
	}
	for i := range c.FuncTypeID {
		c.FuncTypeID[i] = uint32(r.n(4))
	}
	if localFuncCount > 0 {
		c.Code = make([]byte, localFuncCount+r.n(4))
		for i := range c.Code {
			c.Code[i] = r.next()
		}
		for i := range c.Entry {
			c.Entry[i] = r.n(len(c.Code))
		}
	}
	for i := range c.importFuncSigs {
		params := r.n(4)
		results := r.n(2)
		c.importFuncSigs[i].Params = make([]ValType, params)
		for j := range c.importFuncSigs[i].Params {
			c.importFuncSigs[i].Params[j] = r.valType()
		}
		c.importFuncSigs[i].Results = make([]ValType, results)
		for j := range c.importFuncSigs[i].Results {
			c.importFuncSigs[i].Results[j] = r.valType()
		}
	}
	for i := range c.Funcs {
		params := r.n(4)
		results := r.n(2)
		c.Funcs[i].Params = make([]ValType, params)
		for j := range c.Funcs[i].Params {
			c.Funcs[i].Params[j] = r.valType()
		}
		c.Funcs[i].Results = make([]ValType, results)
		for j := range c.Funcs[i].Results {
			c.Funcs[i].Results[j] = r.valType()
		}
	}
	if totalFuncs > 0 && r.n(2) == 1 {
		c.Exports[r.smallString("fn")] = r.n(totalFuncs)
	}

	if r.n(2) == 1 {
		imp := GlobalImportDef{Module: "env", Name: r.smallString("g"), Type: ValI32}
		c.GlobalImports = []GlobalImportDef{imp}
		c.Globals = []GlobalDef{{Type: imp.Type}}
		if r.n(2) == 1 {
			c.Globals = append(c.Globals, GlobalDef{Type: ValI32, HasInitGlobal: true, InitGlobal: 0})
		}
	} else if r.n(2) == 1 {
		c.Globals = []GlobalDef{{Type: r.valType(), Bits: uint64(r.next())}}
	}
	if len(c.Globals) > 0 && r.n(2) == 1 {
		c.GlobalExports[r.smallString("glob")] = r.n(len(c.Globals))
	}

	if totalFuncs > 0 && r.n(2) == 1 {
		c.HasTable = true
		c.TableSize = 1 + r.n(4)
		nSeg := 1 + r.n(3)
		c.Elems = make([]ElemInit, nSeg)
		for si := range c.Elems {
			funcs := make([]uint32, r.n(4))
			for i := range funcs {
				funcs[i] = uint32(r.n(totalFuncs))
			}
			c.Elems[si] = ElemInit{Offset: OffsetInit{Base: uint32(r.n(c.TableSize))}, Funcs: funcs}
		}
	}
	if r.n(2) == 1 {
		nSeg := 1 + r.n(3)
		c.Data = make([]DataInit, nSeg)
		for si := range c.Data {
			bytes := make([]byte, r.n(8))
			for i := range bytes {
				bytes[i] = r.next()
			}
			c.Data[si] = DataInit{Offset: OffsetInit{Base: uint32(r.n(8))}, Bytes: bytes}
		}
	}
	if r.n(2) == 1 {
		c.memoryImport = "env.memory"
	}
	if r.n(2) == 1 {
		moduleName := r.smallString("mod")
		c.Names = &wasm.NameSec{
			ModuleName:    &moduleName,
			FunctionNames: wasm.NameMap{{Index: 0, Name: r.smallString("f")}},
		}
		if r.n(3) == 0 {
			c.Names.LocalNames = wasm.IndirectNameMap{{Index: 0, Names: wasm.NameMap{{Index: 0, Name: r.smallString("l")}}}}
			c.Names.LabelNames = wasm.IndirectNameMap{{Index: 0, Names: wasm.NameMap{{Index: 0, Name: r.smallString("label")}}}}
			c.Names.TypeNames = wasm.NameMap{{Index: 0, Name: r.smallString("t")}}
			c.Names.TableNames = wasm.NameMap{{Index: 0, Name: r.smallString("tab")}}
			c.Names.MemoryNames = wasm.NameMap{{Index: 0, Name: r.smallString("mem")}}
			c.Names.GlobalNames = wasm.NameMap{{Index: 0, Name: r.smallString("g")}}
			c.Names.ElementNames = wasm.NameMap{{Index: 0, Name: r.smallString("e")}}
			c.Names.DataNames = wasm.NameMap{{Index: 0, Name: r.smallString("d")}}
			c.Names.FieldNames = wasm.IndirectNameMap{{Index: 0, Names: wasm.NameMap{{Index: 0, Name: r.smallString("field")}}}}
			c.Names.TagNames = wasm.NameMap{{Index: 0, Name: r.smallString("tag")}}
		}
	}
	if r.n(2) == 1 {
		c.GCTypeDescs = generatedValidGCTypeDescs(t, &r, r.n(5))
	}
	return c
}

func generatedValidGCTypeDescs(t testing.TB, r *compiledFuzzBytes, count int) []gc.TypeDesc {
	t.Helper()
	if count == 0 {
		return nil
	}
	descs := make([]gc.TypeDesc, 0, count)
	for len(descs) < count {
		id := gc.TypeID(len(descs))
		switch r.n(4) {
		case 0:
			descs = append(descs, gc.TypeDesc{ID: id, Kind: gc.KindFunc, Final: true})
		case 1:
			var fields []gc.StorageKind
			if r.n(2) == 1 {
				fields = []gc.StorageKind{}
			}
			if r.n(2) == 1 {
				fields = append(fields, []gc.StorageKind{gc.StorageI32, gc.StorageRefNull, gc.StorageI64}[r.n(3)])
			}
			d, err := gc.NewStructDesc(id, fields)
			if err != nil {
				t.Fatalf("struct desc: %v", err)
			}
			descs = append(descs, d)
		case 2:
			d, err := gc.NewArrayDesc(id, []gc.StorageKind{gc.StorageI8, gc.StorageI32, gc.StorageRefNull}[r.n(3)])
			if err != nil {
				t.Fatalf("array desc: %v", err)
			}
			descs = append(descs, d)
		default:
			d, err := gc.NewStructDesc(id, []gc.StorageKind{gc.StorageRefNull, gc.StorageI32})
			if err != nil {
				t.Fatalf("struct desc: %v", err)
			}
			descs = append(descs, d)
		}
	}
	if err := gc.ValidateTypeDescs(descs); err != nil {
		t.Fatalf("generated GC descriptors invalid: %v", err)
	}
	return descs
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
			Code:           []byte{0xc3},
			Entry:          []int{0},
			NumImports:     1,
			Imports:        []string{"env.host"},
			importFuncSigs: []FuncSig{{Params: []ValType{ValI32}}},
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
		compiledBlobFromPayload(compiledCodecMaliciousGCFieldCountSeed(t)),
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

func compiledCodecMaliciousGCFieldCountSeed(t testing.TB) []byte {
	t.Helper()
	var w compiledWriter
	writeCompiledCodecPrefixAfterMemoryImport(t, &w)
	w.uvar(1)
	w.u32(0)     // ID.
	w.u8(0)      // Kind.
	w.bool(true) // Fields are present.
	w.uvar(uint64(maxInt()))
	for i := 0; i < minGCDescTailBytes; i++ {
		w.u8(0)
	}
	return w.buf
}
