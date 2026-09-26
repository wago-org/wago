package shared

// AMD64Features is an immutable compile-time selection of optional ISA
// extensions. SSE2 is architectural and therefore needs no feature bit.
// AVX-family bits mean that the required OS XSAVE/XCR0 state is enabled too.
type AMD64Features uint32

const (
	AMD64SSSE3 AMD64Features = 1 << iota
	AMD64SSE41
	AMD64SSE42
	AMD64AVX
	AMD64AVX2
	AMD64BMI1
	AMD64BMI2
	AMD64LZCNT
	AMD64POPCNT
	AMD64FMA
	// AMD64AVX512 represents the plugin tier: AVX512F/DQ/BW/VL plus OS state.
	AMD64AVX512
)

// AMD64ModernBaseline names the historical optimization tier. It is no longer
// a native-admission requirement; AMD64 execution has an SSE2 baseline.
const AMD64ModernBaseline = AMD64SSSE3 | AMD64SSE41 | AMD64SSE42 | AMD64AVX

const AMD64KnownFeatures = AMD64ModernBaseline | AMD64AVX2 | AMD64BMI1 | AMD64BMI2 | AMD64LZCNT | AMD64POPCNT | AMD64FMA | AMD64AVX512

func (f AMD64Features) Has(required AMD64Features) bool { return f&required == required }

// AMD64BitCountCapabilities bridges the legacy three-bit selection vocabulary.
func (f AMD64Features) BitCountCapabilities() (bits uint8) {
	if f.Has(AMD64LZCNT) {
		bits |= BitCountLZCNT
	}
	if f.Has(AMD64BMI1) {
		bits |= BitCountTZCNT
	}
	if f.Has(AMD64POPCNT) {
		bits |= BitCountPOPCNT
	}
	return
}
func AMD64BitCountRequirements(bits uint8) (f AMD64Features) {
	if bits&BitCountLZCNT != 0 {
		f |= AMD64LZCNT
	}
	if bits&BitCountTZCNT != 0 {
		f |= AMD64BMI1
	}
	if bits&BitCountPOPCNT != 0 {
		f |= AMD64POPCNT
	}
	return
}
