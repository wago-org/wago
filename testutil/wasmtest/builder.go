package wasmtest

import wasm "github.com/wago-org/wago/src/core/compiler/wasm3"

func ULEB(v uint32) []byte {
	var out []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if v == 0 {
			return out
		}
	}
}

func SLEB32(v int32) []byte {
	var out []byte
	more := true
	for more {
		b := byte(v & 0x7f)
		v >>= 7
		sign := b&0x40 != 0
		more = !((v == 0 && !sign) || (v == -1 && sign))
		if more {
			b |= 0x80
		}
		out = append(out, b)
	}
	return out
}

func SLEB64(v int64) []byte {
	var out []byte
	more := true
	for more {
		b := byte(v & 0x7f)
		v >>= 7
		sign := b&0x40 != 0
		more = !((v == 0 && !sign) || (v == -1 && sign))
		if more {
			b |= 0x80
		}
		out = append(out, b)
	}
	return out
}

func Section(id byte, payload []byte) []byte {
	out := []byte{id}
	out = append(out, ULEB(uint32(len(payload)))...)
	return append(out, payload...)
}

func Module(sections ...[]byte) []byte {
	out := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	for _, s := range sections {
		out = append(out, s...)
	}
	return out
}

func Vec(items ...[]byte) []byte {
	out := ULEB(uint32(len(items)))
	for _, it := range items {
		out = append(out, it...)
	}
	return out
}

func Name(s string) []byte { return append(ULEB(uint32(len(s))), []byte(s)...) }

func FuncType(params, results []wasm.ValType) []byte {
	out := []byte{0x60}
	out = append(out, ULEB(uint32(len(params)))...)
	for _, p := range params {
		out = append(out, wasm.MustEncodeValType(p))
	}
	out = append(out, ULEB(uint32(len(results)))...)
	for _, r := range results {
		out = append(out, wasm.MustEncodeValType(r))
	}
	return out
}

func GlobalEntry(t wasm.ValType, mutable bool, init []byte) []byte {
	mut := byte(0)
	if mutable {
		mut = 1
	}
	out := []byte{wasm.MustEncodeValType(t), mut}
	return append(out, init...)
}

func ExportEntry(name string, kind byte, idx uint32) []byte {
	out := Name(name)
	out = append(out, kind)
	return append(out, ULEB(idx)...)
}

func GlobalImportEntry(module, name string, t wasm.ValType, mutable bool) []byte {
	mut := byte(0)
	if mutable {
		mut = 1
	}
	out := append(Name(module), Name(name)...)
	return append(out, 3, wasm.MustEncodeValType(t), mut)
}

func Code(body []byte) []byte {
	fn := append([]byte{0x00}, body...) // zero local decls
	return append(ULEB(uint32(len(fn))), fn...)
}
