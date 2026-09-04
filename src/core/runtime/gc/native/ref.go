package gc

// Ref is the compact guest reference representation used by WasmGC code.
//
// Encoding invariants:
//   - 0 is null.
//   - low bit 1 is an i31 immediate; bits 1..31 store the low 31 bits.
//   - low bit 0 and non-zero is a heap-object handle (handle index << 1).
//
// Refs are integer guest values, never Go pointers. Native code may keep them in
// registers/spill slots and exact safepoint maps will identify which slots hold
// Refs for collection.
type Ref uint32

const (
	nullRef Ref = 0
	i31Tag  Ref = 1
)

// Null returns the canonical null reference.
func Null() Ref { return nullRef }

func (r Ref) IsNull() bool { return r == nullRef }
func (r Ref) IsI31() bool  { return r&i31Tag != 0 }
func (r Ref) IsObj() bool  { return r != 0 && r&i31Tag == 0 }

// I31New packs the low 31 bits of v into an i31 immediate.
func I31New(v int32) Ref { return Ref(uint32(v)<<1) | i31Tag }

// I31GetS returns the sign-extended 31-bit immediate value.
func (r Ref) I31GetS() int32 {
	u := uint32(r) >> 1
	if u&(1<<30) != 0 {
		u |= ^uint32(0x7fffffff)
	}
	return int32(u)
}

// I31GetU returns the zero-extended 31-bit immediate value.
func (r Ref) I31GetU() uint32 { return (uint32(r) >> 1) & 0x7fffffff }

func RefEq(a, b Ref) bool { return a == b }

func makeObjRef(handle uint32) Ref { return Ref(handle << 1) }
func handleOf(r Ref) uint32        { return uint32(r) >> 1 }
