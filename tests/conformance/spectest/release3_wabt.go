package spectest

// Release3WABTFlags enables Core 3 without experimental wire encodings.
// WABT 1.0.41's --enable-all automatically compacts ordinary imports using
// an incorrect outer group count. Core 3 does not include compact imports.
func Release3WABTFlags() []string {
	return []string{
		"--enable-exceptions", "--enable-function-references", "--enable-tail-call",
		"--enable-annotations", "--enable-gc", "--enable-memory64",
		"--enable-multi-memory", "--enable-extended-const", "--enable-relaxed-simd",
	}
}
