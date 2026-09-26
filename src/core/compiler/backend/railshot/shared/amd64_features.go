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
)

// AMD64ModernBaseline is the admission policy established by #693. Keep that
// policy until every core/SIMD path and artifact requirement has been migrated.
const AMD64ModernBaseline = AMD64SSSE3 | AMD64SSE41 | AMD64SSE42 | AMD64AVX

const AMD64KnownFeatures = AMD64ModernBaseline | AMD64AVX2 | AMD64BMI1 | AMD64BMI2 | AMD64LZCNT | AMD64POPCNT | AMD64FMA

func (f AMD64Features) Has(required AMD64Features) bool { return f&required == required }
