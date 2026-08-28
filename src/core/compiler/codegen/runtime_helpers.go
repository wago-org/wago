package codegen

// Parked runtime-helper dispatch shares the synchronous host-transition ABI.
// The high bit distinguishes internal collector work from ordinary Wasm import
// indexes; the low byte is the stable helper operation ID.
const (
	GCHelperDispatchBit              uint32 = 1 << 30
	GCHelperStructAllocDefault       uint32 = 1
	GCHelperStructGet                uint32 = 2
	GCHelperStructSet                uint32 = 3
	GCHelperStructGetS               uint32 = 4
	GCHelperStructGetU               uint32 = 5
	GCHelperRefTest                  uint32 = 6
	GCHelperAnyConvertExtern         uint32 = 8
	GCHelperExternConvertAny         uint32 = 9
	GCHelperRefCast                  uint32 = 10
	GCHelperStructAlloc              uint32 = 11
	GCHelperStructReserveDead        uint32 = 15
	GCHelperArrayAllocDefault        uint32 = 16
	GCHelperArrayGet                 uint32 = 17
	GCHelperArrayGetS                uint32 = 18
	GCHelperArrayGetU                uint32 = 19
	GCHelperArraySet                 uint32 = 20
	GCHelperArrayLen                 uint32 = 21
	GCHelperArrayAllocFixed          uint32 = 22
	GCHelperArrayAllocUniform        uint32 = 23
	GCHelperArrayAllocData           uint32 = 24
	GCHelperArrayAllocElem           uint32 = 25
	GCHelperArrayDropElem            uint32 = 26
	GCHelperArrayFill                uint32 = 27
	GCHelperArrayCopy                uint32 = 28
	GCHelperArrayInitData            uint32 = 29
	GCHelperArrayInitElem            uint32 = 30
	GCHelperArrayAllocFixedV128Spill uint32 = 31
	GCHelperArrayCheckDefault        uint32 = 36
	GCHelperArrayCheckUniform        uint32 = 37
	GCHelperArrayCheckData           uint32 = 38
	GCHelperArrayCheckFixed          uint32 = 39
	GCHelperStructSetNoBarrier       uint32 = 40
	GCHelperArraySetNoBarrier        uint32 = 41
	GCHelperIDBits                          = 8
	GCHelperIDMask                   uint32 = 1<<GCHelperIDBits - 1
	GCSafepointIDShift                      = GCHelperIDBits
	GCSafepointIDMax                 uint32 = 1<<(30-GCSafepointIDShift) - 1
)

const gcRefTargetHeapMask uint64 = 1<<33 - 1

// EncodeGCRefTarget retains the signed binary heap-type immediate and the two
// dynamic-test qualifiers in one pointer-free machine immediate.
func EncodeGCRefTarget(heap int64, nullable, exact bool) (uint64, bool) {
	if heap < -(1<<32) || heap >= 1<<32 {
		return 0, false
	}
	value := uint64(heap) & gcRefTargetHeapMask
	if nullable {
		value |= 1 << 33
	}
	if exact {
		value |= 1 << 34
	}
	return value, true
}

func DecodeGCRefTarget(value uint64) (heap int64, nullable, exact bool) {
	heap = int64(value & gcRefTargetHeapMask)
	if heap&(1<<32) != 0 {
		heap |= ^int64(gcRefTargetHeapMask)
	}
	return heap, value&(1<<33) != 0, value&(1<<34) != 0
}

// EncodeGCHelperDispatch packs one stable helper operation and allocation
// safepoint into the low 30 bits of a synchronous host dispatch word.
func EncodeGCHelperDispatch(helper, safepoint uint32) (uint32, bool) {
	if helper > GCHelperIDMask || safepoint > GCSafepointIDMax {
		return 0, false
	}
	return helper | safepoint<<GCSafepointIDShift, true
}

func DecodeGCHelperDispatch(payload uint32) (helper, safepoint uint32) {
	return payload & GCHelperIDMask, payload >> GCSafepointIDShift
}
