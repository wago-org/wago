package wasmtest

// IndexMixStoreLoopBody is a scalar, high-word-first pair loop. Local indices
// are destination=0, seed=1, temporary=2, bound=3. It begins with blocktype void.
func IndexMixStoreLoopBody(add, mul, mask, offset uint32, first, last int32, boundFirst bool, boundConstant *uint32) []byte {
	constant := func(v uint32) []byte { return append([]byte{0x41}, SLEB64(int64(int32(v)))...) }
	expression := func(bias uint32) []byte {
		b := []byte{0x20, 1}
		b = append(b, constant(bias)...)
		b = append(b, 0x6a, 0x22, 2)
		b = append(b, constant(uint32(first))...)
		b = append(b, 0x76, 0x20, 2, 0x73)
		b = append(b, constant(mul)...)
		b = append(b, 0x6c, 0x22, 2)
		b = append(b, constant(uint32(last))...)
		b = append(b, 0x76, 0x20, 2, 0x73)
		b = append(b, constant(mask)...)
		return append(b, 0x71)
	}
	store := append([]byte{0x36, 2}, ULEB(offset)...)
	b := []byte{0x40, 0x20, 0, 0x41, 4, 0x6a}
	b = append(b, expression(add+1)...)
	b = append(b, store...)
	b = append(b, 0x20, 0)
	b = append(b, expression(add)...)
	b = append(b, store...)
	b = append(b, 0x20, 0, 0x41, 8, 0x6a, 0x21, 0)
	bound := []byte{0x20, 3}
	if boundConstant != nil {
		bound = constant(*boundConstant)
	}
	step := []byte{0x20, 1, 0x41, 2, 0x6a, 0x22, 1}
	if boundFirst {
		b = append(b, bound...)
		b = append(b, step...)
	} else {
		b = append(b, step...)
		b = append(b, bound...)
	}
	return append(b, 0x47, 0x0d, 0, 0x0b)
}
