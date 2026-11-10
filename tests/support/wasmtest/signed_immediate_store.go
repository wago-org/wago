package wasmtest

import "github.com/wago-org/wago/src/core/compiler/wasm"

// SignedImmediateStore builds a single access or a four-store guest loop.
// The loop rotates through 128 cache-resident groups; pressure keeps seven
// eager integer carriers alive across the stores and consumes them in a sum.
func SignedImmediateStore(value int64, size int, loop, pressure, addr64 bool, offset uint32) []byte {
	addressType := wasm.I32
	memory := []byte{1, 0, 1}
	if addr64 {
		addressType, memory = wasm.I64, []byte{1, 4, 1}
	}
	store := map[int]byte{1: 0x3c, 2: 0x3d, 4: 0x3e, 8: 0x37}[size]
	if store == 0 {
		panic("invalid store width")
	}
	params := []wasm.ValType{addressType}
	body := []byte{0}
	results := []wasm.ValType{}
	if loop {
		params = []wasm.ValType{wasm.I32, addressType, wasm.I64}
		results = []wasm.ValType{wasm.I64}
		body = []byte{2, 1, 0x7f, 1, 0x7e, 0x02, 0x40, 0x03, 0x40,
			0x20, 3, 0x20, 0, 0x4f, 0x0d, 1}
		if pressure {
			for j := 0; j < 7; j++ {
				body = append(body, 0x20, 2, 0xbf, 0xbd)
			}
		}
	}
	count := 1
	if loop {
		count = 4
	}
	for j := 0; j < count; j++ {
		if loop {
			body = append(body, 0x20, 1, 0x20, 3, 0x41)
			body = append(body, SLEB32(127)...)
			body = append(body, 0x71, 0x41, 5, 0x74)
			if addr64 {
				body = append(body, 0xad, 0x7c)
			} else {
				body = append(body, 0x6a)
			}
		} else {
			body = append(body, 0x20, 0)
		}
		body = append(body, 0x42)
		body = append(body, SLEB64(value)...)
		body = append(body, store, 0) // align=1 permits unaligned cases
		body = append(body, ULEB(offset+uint32(j*8))...)
	}
	if loop {
		if pressure {
			for j := 1; j < 7; j++ {
				body = append(body, 0x7c)
			}
			body = append(body, 0x20, 4, 0x7c, 0x21, 4)
		}
		body = append(body, 0x20, 3, 0x41, 1, 0x6a, 0x21, 3, 0x0c, 0, 0x0b, 0x0b, 0x20, 4)
	}
	body = append(body, 0x0b)
	return Module(
		Section(1, Vec(FuncType(params, results))), Section(3, Vec(ULEB(0))),
		Section(5, memory), Section(7, Vec(ExportEntry("run", 0, 0), ExportEntry("memory", 2, 0))),
		Section(10, Vec(append(ULEB(uint32(len(body))), body...))),
	)
}
