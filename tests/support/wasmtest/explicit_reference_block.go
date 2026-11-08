package wasmtest

// ExplicitReferenceBlockTypeModule builds the two reduced modules used by the
// exception-frame block-type regression. typeIndex identifies the last empty
// struct type and is also the explicit reference result of the only function.
func ExplicitReferenceBlockTypeModule(typeIndex uint32) []byte {
	types := make([][]byte, int(typeIndex)+2)
	for i := uint32(0); i <= typeIndex; i++ {
		types[i] = []byte{0x5f, 0x00} // empty struct type
	}
	referenceType := append([]byte{0x64}, ULEB(typeIndex)...)
	types[int(typeIndex)+1] = append([]byte{0x60, 0x00, 0x01}, referenceType...)

	body := []byte{0x41, 0x01, 0x04} // i32.const 1; if
	body = append(body, referenceType...)
	body = append(body, 0xfb, 0x00)
	body = append(body, ULEB(typeIndex)...)
	body = append(body, 0x05, 0xfb, 0x00)
	body = append(body, ULEB(typeIndex)...)
	body = append(body, 0x0b, 0x0b)

	return Module(
		Section(1, Vec(types...)),
		Section(3, Vec(ULEB(typeIndex+1))),
		Section(10, Vec(Code(body))),
	)
}
