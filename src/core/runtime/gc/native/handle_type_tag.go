package gc

// ABI10 uses class bits5..11 and space bits3..7 for canonical TypeID+1.
// Zero is unknown. A tag is neither a root nor a liveness/extent proof.
func (e handleEntry) logicalSpace() spaceKind { return e.space & NativeHandleSpaceMask }

func (e handleEntry) cachedTypeTag() uint32 {
	if sp := e.logicalSpace(); sp == spaceFree || sp >= NativeSpaceCount {
		return 0
	}
	return uint32(e.class>>5)&127 | uint32(e.space&0xf8)<<4
}

// The payload copy preserves the immutable type. Copy both cache fragments at
// the final live-entry commit while retaining destination allocation metadata.
func (e *handleEntry) inheritTypeTag(src handleEntry) {
	e.class = e.class&^NativeHandleTypeTagClassMask | src.class&NativeHandleTypeTagClassMask
	e.space = e.space&NativeHandleSpaceMask | src.space&^NativeHandleSpaceMask
}
