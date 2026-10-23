package wago

import (
	"fmt"
)

func (c *Compiled) executionView() *Compiled {
	if snapshot := c.loadValidateMemo().executionView(); snapshot != nil {
		return snapshot
	}
	return c
}

func cloneCompiledMetadata(c *Compiled) *Compiled {
	return cloneCompiledMetadataWithDataPolicy(c, false)
}

type packedCloneStorage[T any] struct {
	values []T
	next   int
}

// Empty internal entries retain the legacy wrapper-only representation. Every
// present directory must cover all local functions. Only fresh compiler output
// may carry the direct-prepared marker; wire offsets are always non-negative.
func (c *Compiled) validateInternalEntries(artifact bool) error {
	if len(c.InternalEntry) != 0 && len(c.InternalEntry) != len(c.Entry) {
		return fmt.Errorf("compiled metadata invalid: InternalEntry length %d != Entry length %d", len(c.InternalEntry), len(c.Entry))
	}
	for i, entry := range c.InternalEntry {
		compileMarkers := directPreparedEntryMask | directPreparedLightMask | directPreparedBoundedMask | directHostSegmentsMask | nativeScalarLeafMask | nativeScalarBoundedMask | goHostDispatchTagMask | integerHostContextMask
		if artifact && uint(entry)&compileMarkers != 0 {
			return fmt.Errorf("compiled metadata invalid: InternalEntry[%d] has compile-only marker", i)
		}
		off := internalEntryOffset(entry)
		if off < 0 || off >= len(c.code) {
			return fmt.Errorf("compiled metadata invalid: InternalEntry[%d] offset %d out of code range %d", i, off, len(c.code))
		}
	}
	return nil
}
