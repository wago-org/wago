package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	wago "github.com/wago-org/wago"
)

type vectorContract struct {
	InputOffset     uint64 `json:"input_offset"`
	OutputOffset    uint64 `json:"output_offset"`
	InputPtrExport  string `json:"input_ptr_export"`
	OutputPtrExport string `json:"output_ptr_export"`
	OutputLen       uint64 `json:"output_len"`
	Mod             uint64 `json:"mod"`
	Cases           []struct {
		Len uint64 `json:"len"`
		Out string `json:"out"`
	} `json:"cases"`
}

// Use only a declared exact-vector contract; recreate its input in every fresh
// sample and validate bytes outside the timer, after warmup and measurement.
func setupVector(in *wago.Instance, v *vectorContract, length uint64) ([]uint64, func()) {
	var encoded string
	found := false
	for _, c := range v.Cases {
		if c.Len == length {
			encoded = c.Out
			found = true
			break
		}
	}
	if !found || v.Mod == 0 {
		panic("unsupported vector length or pattern")
	}
	expected, err := hex.DecodeString(encoded)
	must(err)
	if uint64(len(expected)) != v.OutputLen {
		panic("vector oracle length")
	}
	pointer := func(name string, fallback uint64) uint64 {
		if name == "" {
			return fallback
		}
		fn, err := in.WasmFunc(name)
		must(err)
		out, err := fn.Invoke()
		must(err)
		if len(out) != 1 {
			panic("vector pointer arity")
		}
		return out[0]
	}
	input := pointer(v.InputPtrExport, v.InputOffset)
	output := pointer(v.OutputPtrExport, v.OutputOffset)
	if in.Memory() == nil {
		panic("vector requires memory")
	}
	span := func(offset, size uint64) []byte {
		mem := in.Memory().UnsafeBytes()
		if offset > uint64(len(mem)) || size > uint64(len(mem))-offset {
			panic("vector memory range")
		}
		return mem[offset : offset+size]
	}
	data := span(input, length)
	for i := range data {
		data[i] = byte(uint64(i) % v.Mod)
	}
	check := func() {
		if !bytes.Equal(span(output, v.OutputLen), expected) {
			panic(fmt.Sprintf("vector oracle mismatch, length %d", length))
		}
	}
	return []uint64{input, length, output}, check
}
