package wago

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestCompiledCodecPreservesStartFunctionNamespace(t *testing.T) {
	base := Compiled{Entry: []int{0, 16}, InternalEntry: []int{0, 16}, NumImports: 1, Imports: []string{"env.start"}, importFuncSigs: []FuncSig{{}}, Funcs: []FuncSig{{Results: []ValType{ValI32}}, {}}, dynamicImports: true, needsFuncRefContextHeader: true}
	plain, err := marshalCompiledMetadata(&base)
	if err != nil {
		t.Fatal(err)
	}
	for _, imported := range []bool{false, true} {
		input := base
		input.HasStart, input.StartIsImport, input.StartLocalFunc = true, imported, 1
		encoded, err := marshalCompiledMetadata(&input)
		if err != nil {
			t.Fatal(err)
		}
		// This fixture has no GC descriptors or frame map: the extension follows
		// the final requirement word and precedes the one-byte empty GC count.
		if len(encoded) != len(plain)+4 || binary.LittleEndian.Uint64(encoded[len(encoded)-13:])&compiledStartFunction == 0 {
			t.Fatal("missing gated start payload")
		}
		measured, err := measureCompiledMetadata(&input)
		_, written, writeErr := marshalCompiledMetadataMeasured(&input)
		_, plainSizes, plainErr := marshalCompiledMetadataMeasured(&base)
		if err != nil || writeErr != nil || plainErr != nil || measured != written || measured.Features != plainSizes.Features+4 {
			t.Fatal("start payload measurement", measured, written, err, writeErr, plainErr)
		}
		output := Compiled{code: make([]byte, 32)}
		if err := unmarshalCompiledMetadata(&output, encoded); err != nil {
			t.Fatal(err)
		}
		if !output.HasStart || output.StartIsImport != imported || imported && output.StartImportIdx != 0 || !imported && output.StartLocalFunc != 1 {
			t.Fatal("start namespace was not preserved", output.StartLocalFunc, output.StartImportIdx, output.StartIsImport)
		}
		if output.requiredFeatures&CoreFeatures(compiledStartFunction) != 0 {
			t.Fatal("internal start requirement escaped into Wasm features")
		}
		for _, corrupt := range []func([]byte) []byte{
			func(b []byte) []byte { binary.LittleEndian.PutUint32(b[len(b)-5:len(b)-1], 127); return b },
			func(b []byte) []byte { binary.LittleEndian.PutUint32(b[len(b)-5:len(b)-1], 1); return b }, // defined nonvoid function0
			func(b []byte) []byte { return b[:len(b)-3] },
		} {
			bad := Compiled{code: make([]byte, 32)}
			if err := unmarshalCompiledMetadata(&bad, corrupt(bytes.Clone(encoded))); err == nil {
				t.Fatal("malformed start metadata accepted")
			}
		}
		if err := unmarshalCompiledMetadata(&output, plain); err != nil || output.HasStart || output.StartIsImport || output.StartLocalFunc != 0 || output.StartImportIdx != 0 {
			t.Fatal("reused metadata retained inactive start", err)
		}
	}
	for _, mutate := range []func(*Compiled){
		func(c *Compiled) { c.StartLocalFunc = -1 },
		func(c *Compiled) { c.StartLocalFunc = 2 },
		func(c *Compiled) { c.StartLocalFunc = 0 },
		func(c *Compiled) { c.Entry = nil },
		func(c *Compiled) { c.StartIsImport = true; c.StartImportIdx = 1 },
	} {
		bad := base
		bad.HasStart, bad.StartLocalFunc = true, 1
		mutate(&bad)
		if _, err := marshalCompiledMetadata(&bad); err == nil {
			t.Fatal("invalid start serialized")
		}
	}
	// No-start metadata remains unchanged, even if inactive start fields were
	// left over in the producer's scratch state.
	base.StartLocalFunc, base.StartIsImport, base.StartImportIdx = 1, true, 0
	unchanged, err := marshalCompiledMetadata(&base)
	if err != nil || !bytes.Equal(plain, unchanged) {
		t.Fatal("inactive start changed the existing artifact format", err)
	}
}
