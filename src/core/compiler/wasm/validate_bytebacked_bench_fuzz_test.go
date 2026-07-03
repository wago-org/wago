package wasm

import (
	"fmt"
	"testing"
)

func BenchmarkDecodeValidate(b *testing.B) {
	data := benchmarkDecodeValidateModule(256)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		m, err := DecodeModule(data)
		if err != nil {
			b.Fatal(err)
		}
		if err := ValidateModule(m); err != nil {
			b.Fatal(err)
		}
	}
}

func FuzzDecodeValidateByteBackedDifferentialGenerated(f *testing.F) {
	for _, seed := range []struct {
		kind     uint8
		funcs    uint8
		mutation uint8
		arg      uint32
	}{
		{0, 1, 0, 0},
		{0, 8, 0, 0},
		{1, 3, 0, 0},
		{2, 1, 0, 0},
		{3, 1, 0, 0},
		{4, 1, 0, 0},
		{5, 1, 0, 0},
		{6, 1, 0, 0},
		{0, 8, 1, 17},
		{0, 8, 2, 23},
		{0, 8, 3, 5},
		{0, 8, 4, 0},
	} {
		f.Add(seed.kind, seed.funcs, seed.mutation, seed.arg)
	}
	f.Fuzz(func(t *testing.T, kind, funcs, mutation uint8, arg uint32) {
		data := generatedDifferentialModule(kind, funcs)
		data = mutateDifferentialModule(data, mutation, arg)
		want := decodeThenValidate(data)
		got := byteBackedDecodeThenValidate(data)
		if (want == nil) != (got == nil) {
			t.Fatalf("AST decode+ValidateModule=%v byte-backed decode+validate=%v", want, got)
		}
		if want != nil && errorPhase(want) != errorPhase(got) {
			t.Fatalf("AST decode+ValidateModule=%v (%s) byte-backed decode+validate=%v (%s)", want, errorPhase(want), got, errorPhase(got))
		}
	})
}

func generatedDifferentialModule(kind, funcs uint8) []byte {
	switch kind % 7 {
	case 0:
		n := 1 + int(funcs%16)
		return benchmarkDecodeValidateModule(n)
	case 1:
		return module(section(secCustom, 0x01, 0xff)) // malformed custom-section name UTF-8
	case 2:
		payload := []byte{0x00, 0x04, 0x03, 'm', 'o', 'd'}
		return module(custom("name", payload...), custom("name", payload...))
	case 3:
		return module(section(secGlobal, 0x01, 0x7f, 0x00, 0x42, 0x00, 0x0b))
	case 4:
		return module(
			section(secMemory, 0x01, 0x00, 0x01),
			section(secData, 0x01, 0x00, 0x42, 0x00, 0x0b, 0x00),
		)
	case 5:
		return module(
			section(secType, 0x01, 0x60, 0x00, 0x00),
			section(secFunction, 0x01, 0x00),
			section(secTable, 0x01, 0x70, 0x00, 0x01),
			section(secElement, 0x01, 0x06, 0x00, 0x41, 0x00, 0x0b, 0x70, 0x01, 0xd0, 0x70, 0x0b),
			section(secCode, 0x01, 0x02, 0x00, 0x0b),
		)
	default:
		body := []byte{0xfd, 0x0c}
		body = append(body, make([]byte, 16)...)
		body = append(body, 0x1a, 0x0b) // v128.const; drop; end
		return module(
			section(secType, 0x01, 0x60, 0x00, 0x00),
			section(secFunction, 0x01, 0x00),
			section(secCode, append([]byte{0x01}, append(u32(uint32(1+len(body))), append([]byte{0x00}, body...)...)...)...),
		)
	}
}

func mutateDifferentialModule(data []byte, mutation uint8, arg uint32) []byte {
	out := append([]byte(nil), data...)
	switch mutation % 6 {
	case 0:
		return out
	case 1:
		if len(out) == 0 {
			return out
		}
		return out[:int(arg%uint32(len(out)))]
	case 2:
		if len(out) > 8 {
			out[8+int(arg%uint32(len(out)-8))] ^= 0x40
		} else if len(out) > 0 {
			out[int(arg%uint32(len(out)))] ^= 0x40
		}
		return out
	case 3:
		out = append(out, 0xff, 0x00)
		return out
	case 4:
		return insertAfterHeader(out, custom("name", 0x00, 0x04, 0x03, 'm', 'o', 'd'))
	default:
		return insertAfterHeader(out, section(secCustom, 0x01, 0xff))
	}
}

func benchmarkDecodeValidateModule(funcs int) []byte {
	if funcs < 1 {
		funcs = 1
	}
	var sections [][]byte
	sections = append(sections, benchmarkNameSection(funcs))
	sections = append(sections, section(secType, benchmarkVec(
		benchmarkFuncType([]byte{0x7f}, []byte{0x7f}),          // type 0: (i32)->i32
		benchmarkFuncType([]byte{0x7f, 0x7f, 0x7f}, nil),       // type 1: (i32,i32,i32)->()
		benchmarkFuncType(nil, nil),                            // type 2: ()->()
		benchmarkFuncType([]byte{0x7f, 0x7f}, []byte{0x7f}),    // type 3: (i32,i32)->i32
		benchmarkFuncType([]byte{0x7e, 0x7e}, []byte{0x7e}),    // type 4: (i64,i64)->i64
		benchmarkFuncType([]byte{0x7d, 0x7d}, []byte{0x7d}),    // type 5: (f32,f32)->f32
		benchmarkFuncType([]byte{0x7c, 0x7c}, []byte{0x7c}),    // type 6: (f64,f64)->f64
		benchmarkFuncType([]byte{0x7f, 0x7e, 0x7d, 0x7c}, nil), // type 7: mixed params
		benchmarkFuncType(nil, []byte{0x7f, 0x7e}),             // type 8: multivalue result
		benchmarkFuncType([]byte{0x7f}, []byte{0x7f, 0x7e}),    // type 9: multivalue block type target
	)...))
	sections = append(sections, section(secFunction, benchmarkFunctionSection(funcs)...))
	sections = append(sections, section(secTable, append([]byte{0x01, 0x70, 0x00}, u32(uint32(funcs))...)...))
	sections = append(sections, section(secMemory, 0x01, 0x00, 0x01))
	sections = append(sections, section(secGlobal, 0x01, 0x7f, 0x01, 0x41, 0x00, 0x0b))
	sections = append(sections, section(secElement, benchmarkElementSection(funcs)...))
	sections = append(sections, section(secCode, benchmarkCodeSection(funcs)...))
	sections = append(sections, section(secData, 0x01, 0x00, 0x41, 0x00, 0x0b, 0x20,
		'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h',
		'i', 'j', 'k', 'l', 'm', 'n', 'o', 'p',
		'q', 'r', 's', 't', 'u', 'v', 'w', 'x',
		'y', 'z', '0', '1', '2', '3', '4', '5'))
	return module(sections...)
}

func benchmarkNameSection(funcs int) []byte {
	modulePayload := benchmarkName("bench")
	payload := []byte{0x00}
	payload = append(payload, u32(uint32(len(modulePayload)))...)
	payload = append(payload, modulePayload...)

	funcNames := u32(uint32(funcs))
	for i := 0; i < funcs; i++ {
		funcNames = append(funcNames, u32(uint32(i))...)
		funcNames = append(funcNames, benchmarkName(fmt.Sprintf("f%d", i))...)
	}
	payload = append(payload, 0x01)
	payload = append(payload, u32(uint32(len(funcNames)))...)
	payload = append(payload, funcNames...)
	return custom("name", payload...)
}

func benchmarkFunctionSection(funcs int) []byte {
	payload := u32(uint32(funcs))
	for i := 0; i < funcs; i++ {
		payload = append(payload, u32(uint32(benchmarkFuncTypeIndex(i)))...)
	}
	return payload
}

func benchmarkCodeSection(funcs int) []byte {
	payload := u32(uint32(funcs))
	for i := 0; i < funcs; i++ {
		payload = append(payload, benchmarkCode(benchmarkFuncBody(i))...)
	}
	return payload
}

func benchmarkElementSection(funcs int) []byte {
	payload := []byte{0x01, 0x00, 0x41, 0x00, 0x0b}
	payload = append(payload, u32(uint32(funcs))...)
	for i := 0; i < funcs; i++ {
		payload = append(payload, u32(uint32(i))...)
	}
	return payload
}

func benchmarkFuncTypeIndex(i int) int {
	switch i % 10 {
	case 5:
		return 1
	case 8:
		return 3
	case 9:
		return 4
	default:
		return 0
	}
}

func benchmarkFuncBody(i int) []byte {
	switch i % 10 {
	case 0:
		return []byte{0x20, 0x00, 0x41, byte(i & 0x3f), 0x6a, 0x0b} // local.get; i32.const; i32.add
	case 1:
		return []byte{0x02, 0x7f, 0x41, 0x07, 0x20, 0x00, 0x0d, 0x00, 0x0b, 0x0b} // block/br_if
	case 2:
		return []byte{0x02, 0x7f, 0x02, 0x7f, 0x41, 0x09, 0x41, 0x00, 0x0e, 0x01, 0x00, 0x01, 0x0b, 0x0b, 0x0b} // br_table
	case 3:
		return []byte{0x20, 0x00, 0x10, 0x00, 0x0b} // direct call
	case 4:
		return []byte{0x20, 0x00, 0x24, 0x00, 0x23, 0x00, 0x0b} // global.set/get
	case 5:
		return []byte{ // memory.copy; memory.fill
			0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0a, 0x00, 0x00,
			0x20, 0x00, 0x41, 0x00, 0x20, 0x02, 0xfc, 0x0b, 0x00,
			0x0b,
		}
	case 6:
		return []byte{0x3f, 0x00, 0x20, 0x00, 0x40, 0x00, 0x6a, 0x0b} // memory.size/grow
	case 7:
		return []byte{0x20, 0x00, 0x45, 0x04, 0x7f, 0x41, 0x01, 0x05, 0x41, 0x02, 0x0b, 0x0b} // if/else
	case 8:
		return []byte{0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b} // (i32,i32)->i32
	default:
		return []byte{0x20, 0x00, 0x20, 0x01, 0x7c, 0x0b} // (i64,i64)->i64
	}
}

func benchmarkCode(body []byte) []byte {
	fn := append([]byte{0x00}, body...)
	return append(u32(uint32(len(fn))), fn...)
}

func benchmarkFuncType(params, results []byte) []byte {
	out := []byte{0x60}
	out = append(out, u32(uint32(len(params)))...)
	out = append(out, params...)
	out = append(out, u32(uint32(len(results)))...)
	out = append(out, results...)
	return out
}

func benchmarkVec(items ...[]byte) []byte {
	out := u32(uint32(len(items)))
	for _, item := range items {
		out = append(out, item...)
	}
	return out
}

func benchmarkName(s string) []byte {
	out := u32(uint32(len(s)))
	return append(out, s...)
}

func insertAfterHeader(data []byte, payload []byte) []byte {
	if len(data) < 8 {
		return append(append([]byte(nil), payload...), data...)
	}
	out := append([]byte(nil), data[:8]...)
	out = append(out, payload...)
	out = append(out, data[8:]...)
	return out
}
